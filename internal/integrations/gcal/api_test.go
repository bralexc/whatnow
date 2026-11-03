package gcal

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jameynakama/whatnow/internal/integrations"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestAPIPaginatesFiltersAndKeepsICalIdentity(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "www.googleapis.com" || r.URL.EscapedPath() != "/calendar/v3/calendars/team%2Fwork@example.com/events" {
			t.Errorf("URL = %s", r.URL)
		}
		if r.Header.Get("X-Goog-Api-Key") != "private-key" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("key") != "" {
			t.Error("wrong API key transport")
		}
		q := r.URL.Query()
		if q.Get("singleEvents") != "true" || q.Get("timeMin") != "2026-10-05T00:00:00-05:00" || q.Get("timeMax") != "2026-10-06T00:00:00-05:00" {
			t.Errorf("query = %v", q)
		}
		if calls == 1 {
			return response(200, `{"nextPageToken":"page-two","items":[
    {"id":"one","iCalUID":"one@google.com","summary":" Meeting ","start":{"dateTime":"2026-10-05T09:00:00-05:00"},"end":{"dateTime":"2026-10-05T10:00:00-05:00"}},
    {"id":"all-day","start":{"date":"2026-10-05"}},
    {"id":"cancelled","status":"cancelled","start":{"dateTime":"2026-10-05T09:00:00-05:00"}},
    {"id":"declined","start":{"dateTime":"2026-10-05T09:00:00-05:00"},"attendees":[{"self":true,"responseStatus":"declined"}]},
    {"id":"overlap","start":{"dateTime":"2026-10-04T23:00:00-05:00"}},
    {"id":"next-day","start":{"dateTime":"2026-10-06T00:00:00-05:00"}}
   ]}`), nil
		}
		if q.Get("pageToken") != "page-two" {
			t.Error("missing next page")
		}
		return response(200, `{"items":[{"id":"instance","iCalUID":"series@google.com","recurringEventId":"series","originalStartTime":{"dateTime":"2026-10-04T10:00:00-05:00"},"start":{"dateTime":"2026-10-05T08:00:00-05:00"}}]}`), nil
	})}
	from, to := day("2026-10-05")
	events, err := New(client).fetchEvents(context.Background(), "private-key", "", "team/work@example.com", from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"(no title) series@google.com|20261004T150000Z series@google.com 08:00", "Meeting one@google.com  09:00-10:00"}
	if diff := cmp.Diff(want, summary(events)); diff != "" {
		t.Error(diff)
	}
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
}

func TestAPIFailedPageDiscardsPartialEventsAndHidesSecrets(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return response(200, `{"nextPageToken":"next","items":[{"id":"1","start":{"dateTime":"2026-10-05T09:00:00-05:00"}}]}`), nil
				}
				return response(status, `{"error":"private-key private-token"}`), nil
			})}
			from, to := day("2026-10-05")
			events, err := New(client).fetchEvents(context.Background(), "private-key", "", "calendar", from, to)
			if len(events) != 0 || err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("events=%v err=%v", events, err)
			}
		})
	}
}

func TestOAuthAuthorizationAndRefresh(t *testing.T) {
	cfg := integrations.Config{Values: map[string]string{"method": "oauth", "client_id": "client", "client_secret": "secret", "refresh_token": "refresh"}, Lists: map[string][]integrations.Row{"api_calendars": {{Values: map[string]string{"label": "Work", "calendar_id": "primary"}}}}}
	var grants []string
	c := New(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == tokenEndpoint {
			r.ParseForm()
			grants = append(grants, r.Form.Get("grant_type"))
			if r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
				t.Error("missing client credentials")
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("code") != "code" || r.Form.Get("redirect_uri") != "http://localhost/callback" {
					t.Error("missing exchange parameters")
				}
			} else if r.Form.Get("refresh_token") != "refresh" {
				t.Error("missing refresh token")
			}
			return response(200, `{"access_token":"access","refresh_token":"refresh","scope":"`+calendarScope+`"}`), nil
		}
		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("X-Goog-Api-Key") != "" {
			t.Error("wrong OAuth authorization")
		}
		return response(200, `{"items":[]}`), nil
	})})
	target, err := c.Authorize(cfg, "http://localhost/callback", "state", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(target)
	q := u.Query()
	challenge := sha256.Sum256([]byte("verifier"))
	if u.Host != "accounts.google.com" || q.Get("state") != "state" || q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || q.Get("code_challenge_method") != "S256" || q.Get("scope") != calendarScope || q.Get("access_type") != "offline" {
		t.Errorf("authorization URL = %s", target)
	}
	if strings.Contains(target, "secret") {
		t.Error("client secret exposed")
	}
	token, err := c.Exchange(context.Background(), cfg, "http://localhost/callback", "code", "verifier")
	if token != "refresh" || err != nil {
		t.Fatalf("token=%q err=%v", token, err)
	}
	from, to := day("2026-10-05")
	sources := c.Pull(context.Background(), cfg, from, to)
	if len(sources) != 1 || sources[0].Err != nil {
		t.Fatalf("sources=%v", sources)
	}
	if diff := cmp.Diff([]string{"authorization_code", "refresh_token"}, grants); diff != "" {
		t.Error(diff)
	}
}

func TestOAuthRejectsMissingTokensAndScope(t *testing.T) {
	for _, body := range []string{`{}`, `{"access_token":"access"}`, `{"access_token":"access","refresh_token":"refresh","scope":"openid"}`} {
		c := New(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })})
		if _, err := c.Exchange(context.Background(), integrations.Config{Values: map[string]string{}}, "redirect", "code", "verifier"); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
