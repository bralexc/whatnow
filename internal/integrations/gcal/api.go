package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jameynakama/whatnow/internal/integrations"
)

const eventsEndpoint = "https://www.googleapis.com/calendar/v3/calendars/"

func (c *Calendar) pullAPI(ctx context.Context, cfg integrations.Config, from, to time.Time) []integrations.Source {
	var token string
	var authErr error
	if cfg.Values["method"] == "oauth" {
		var tok tokenResponse
		tok, authErr = c.token(ctx, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {cfg.Values["refresh_token"]},
			"client_id": {cfg.Values["client_id"]}, "client_secret": {cfg.Values["client_secret"]},
		})
		token = tok.AccessToken
	}
	var sources []integrations.Source
	for _, row := range cfg.Lists["api_calendars"] {
		src := integrations.Source{Label: row.Values["label"], Err: authErr}
		if src.Err == nil {
			src.Events, src.Err = c.fetchEvents(ctx, cfg.Values["api_key"], token, row.Values["calendar_id"], from, to)
		}
		sources = append(sources, src)
	}
	return sources
}

type apiTime struct {
	DateTime string `json:"dateTime"`
}
type apiEvent struct {
	ID            string  `json:"id"`
	UID           string  `json:"iCalUID"`
	Summary       string  `json:"summary"`
	Status        string  `json:"status"`
	RecurringID   string  `json:"recurringEventId"`
	OriginalStart apiTime `json:"originalStartTime"`
	Start         apiTime `json:"start"`
	End           apiTime `json:"end"`
	Attendees     []struct {
		Self     bool   `json:"self"`
		Email    string `json:"email"`
		Response string `json:"responseStatus"`
	} `json:"attendees"`
}

func (c *Calendar) fetchEvents(ctx context.Context, key, token, calendarID string, from, to time.Time) ([]integrations.Event, error) {
	query := url.Values{
		"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)},
		"singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {"2500"},
	}
	var events []integrations.Event
	seenPages := map[string]bool{}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsEndpoint+url.PathEscape(calendarID)+"/events?"+query.Encode(), nil)
		if err != nil {
			return nil, errUnreadable
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("X-Goog-Api-Key", key)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, errors.New("couldn't reach Google Calendar; try again")
		}
		var page struct {
			Items []apiEvent `json:"items"`
			Next  string     `json:"nextPageToken"`
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			switch resp.StatusCode {
			case 401:
				return nil, errors.New("Google authorization expired; reconnect in settings")
			case 403:
				return nil, errors.New("Google denied access; check API permissions and that API-key calendars are public")
			case 404:
				return nil, errors.New("calendar not found; check the Calendar ID and access permissions")
			default:
				return nil, fmt.Errorf("Google Calendar returned %d; try again", resp.StatusCode)
			}
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, errUnreadable
		}
		for _, item := range page.Items {
			if event, ok := item.event(calendarID, from, to); ok {
				events = append(events, event)
			}
		}
		if page.Next == "" {
			break
		}
		if seenPages[page.Next] {
			return nil, errUnreadable
		}
		seenPages[page.Next] = true
		query.Set("pageToken", page.Next)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].StartsAt.Before(events[j].StartsAt) })
	return events, nil
}

func (item apiEvent) event(calendarID string, from, to time.Time) (integrations.Event, bool) {
	start, err := time.Parse(time.RFC3339, item.Start.DateTime)
	if err != nil || item.Status == "cancelled" || !inWindow(start, from, to) {
		return integrations.Event{}, false
	}
	for _, attendee := range item.Attendees {
		if (attendee.Self || strings.EqualFold(attendee.Email, calendarID)) && attendee.Response == "declined" {
			return integrations.Event{}, false
		}
	}
	key := item.UID
	if key == "" {
		key = calendarID + "|" + item.ID
	}
	if item.ID == "" {
		return integrations.Event{}, false
	}
	event := integrations.Event{Key: key, Title: strings.TrimSpace(item.Summary), StartsAt: start}
	if event.Title == "" {
		event.Title = "(no title)"
	}
	if end, err := time.Parse(time.RFC3339, item.End.DateTime); err == nil && end.After(start) {
		event.EndsAt = &end
	}
	if item.RecurringID != "" {
		original, err := time.Parse(time.RFC3339, item.OriginalStart.DateTime)
		if err != nil {
			return integrations.Event{}, false
		}
		event.Series = item.UID
		if event.Series == "" {
			event.Series = calendarID + "|" + item.RecurringID
		}
		event.Key = occurrenceKey(event.Series, original)
	}
	return event, true
}
