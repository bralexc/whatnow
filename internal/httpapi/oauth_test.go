package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jameynakama/whatnow/internal/integrations"
	"github.com/jameynakama/whatnow/internal/integrations/gcal"
	"github.com/jameynakama/whatnow/internal/store"
	"github.com/zalando/go-keyring"
)

func oauthServer(t *testing.T) *Server {
	t.Helper()
	keyring.MockInit()
	db, err := store.Open(filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	runner := integrations.NewRunner(db, nil, nil, time.UTC, time.Now, gcal.New(nil))
	_, err = runner.Save(context.Background(), "gcal", integrations.Update{Values: map[string]string{"method": "oauth", "client_id": "client", "client_secret": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Deps: Deps{Integrations: runner}}
}

func startRequest(s *Server, redirect string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"redirect_uri": redirect})
	req := httptest.NewRequest("POST", "http://localhost:7777/api/v1/integrations/gcal/oauth/start", strings.NewReader(string(body)))
	req.SetPathValue("id", "gcal")
	rec := httptest.NewRecorder()
	s.startOAuth(rec, req)
	return rec
}

func TestOAuthStartBindsStateAndRejectsForeignRedirect(t *testing.T) {
	s := oauthServer(t)
	for _, redirect := range []string{"https://evil.example/api/v1/integrations/gcal/oauth/callback", "http://localhost:7777/wrong", "http://user@localhost:7777/api/v1/integrations/gcal/oauth/callback", "http://localhost:7777/api/v1/integrations/gcal/oauth/callback?next=evil"} {
		if rec := startRequest(s, redirect); rec.Code != 400 {
			t.Fatalf("accepted redirect %s", redirect)
		}
	}
	rec := startRequest(s, "http://localhost:7777/api/v1/integrations/gcal/oauth/callback")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var body struct {
		URL string `json:"url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	target, _ := url.Parse(body.URL)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != target.Query().Get("state") || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("invalid state cookie: %v", cookies)
	}
	if _, ok := s.oauthFlows[cookies[0].Value]; !ok {
		t.Fatal("state wasn't stored")
	}
}

func TestOAuthCallbackRequiresCookieExpiresAndConsumesState(t *testing.T) {
	for _, scenario := range []string{"success", "denied", "missing-cookie", "wrong-cookie", "expired", "wrong-id", "exchange-failed"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			expires := time.Now().Add(time.Minute)
			if scenario == "expired" {
				expires = time.Now().Add(-time.Minute)
			}
			s := &Server{oauthFlows: map[string]oauthFlow{"state": {id: "gcal", expires: expires, finish: func(context.Context, string) error {
				calls++
				if scenario == "exchange-failed" {
					return context.Canceled
				}
				return nil
			}}}}
			req := httptest.NewRequest("GET", "http://localhost/api/v1/integrations/gcal/oauth/callback?state=state&code=code", nil)
			req.SetPathValue("id", "gcal")
			if scenario == "wrong-id" {
				req.SetPathValue("id", "jira")
			}
			if scenario == "denied" {
				req.URL.RawQuery = "state=state&error=access_denied"
			}
			if scenario != "missing-cookie" {
				value := "state"
				if scenario == "wrong-cookie" {
					value = "wrong"
				}
				req.AddCookie(&http.Cookie{Name: "whatnow_oauth", Value: value})
			}
			rec := httptest.NewRecorder()
			s.finishOAuth(rec, req)
			valid := scenario == "success" || scenario == "exchange-failed" || scenario == "denied"
			if valid && rec.Code != 303 || !valid && rec.Code != 400 {
				t.Fatalf("status %d", rec.Code)
			}
			expectedCalls := 0
			if scenario == "success" || scenario == "exchange-failed" {
				expectedCalls = 1
			}
			if calls != expectedCalls {
				t.Fatalf("exchange calls = %d", calls)
			}
			if valid {
				result := "connected"
				if scenario == "denied" {
					result = "denied"
				}
				if scenario == "exchange-failed" {
					result = "failed"
				}
				if rec.Header().Get("Location") != "/settings/integrations?google="+result {
					t.Fatal("wrong redirect")
				}
				again := httptest.NewRecorder()
				s.finishOAuth(again, req)
				if again.Code != 400 {
					t.Fatal("callback replay accepted")
				}
			}
		})
	}
}
