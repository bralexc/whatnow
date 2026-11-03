package integrations_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jameynakama/whatnow/internal/integrations"
	"github.com/jameynakama/whatnow/internal/integrations/gcal"
	"github.com/jameynakama/whatnow/internal/store"
	"github.com/zalando/go-keyring"
)

type testOAuth struct{ *gcal.Calendar }

func (*testOAuth) Exchange(context.Context, integrations.Config, string, string, string) (string, error) {
	return "private-refresh-token", nil
}

func TestGoogleConnectionMethodsAndOAuthLifecycle(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runner := integrations.NewRunner(db, nil, nil, time.UTC, time.Now, &testOAuth{gcal.New(nil)})
	view, err := runner.View(ctx, "gcal")
	if err != nil || view.Ready {
		t.Fatalf("fresh ready=%v err=%v", view.Ready, err)
	}
	update := integrations.Update{Values: map[string]string{"method": "api_key", "api_key": "private-api-key"}, Lists: map[string][]integrations.RowUpdate{"api_calendars": {{Values: map[string]string{"label": "Public", "calendar_id": "public@example.com"}}}}}
	view, err = runner.Save(ctx, "gcal", update)
	if err != nil || !view.Ready || !view.Secrets["api_key"] || view.Values["api_key"] != "" {
		t.Fatalf("API key setup failed: %v", err)
	}
	update.Lists["api_calendars"][0].ID = view.Lists["api_calendars"][0].ID
	// An unfinished, now hidden iCal row must not block OAuth setup.
	update.Lists["calendars"] = []integrations.RowUpdate{{Values: map[string]string{"label": "Unfinished"}}}
	update.Values = map[string]string{"method": "oauth", "client_id": "client", "client_secret": "private-client-secret"}
	view, err = runner.Save(ctx, "gcal", update)
	if err != nil || view.Ready {
		t.Fatalf("OAuth ready before connection: %v", err)
	}
	_, finish, err := runner.BeginOAuth(ctx, "gcal", "http://localhost/callback", "state", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(ctx, "code"); err != nil {
		t.Fatal(err)
	}
	view, err = runner.View(ctx, "gcal")
	if err != nil || !view.Ready || !view.Secrets["refresh_token"] {
		t.Fatal("OAuth not ready after connection")
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM integrations WHERE id='gcal'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "private-") {
		t.Fatal("secret stored in database")
	}
	update.Values["refresh_token"] = "forged"
	if _, err := runner.Save(ctx, "gcal", update); err == nil {
		t.Fatal("accepted a client-supplied refresh token")
	}
	delete(update.Values, "refresh_token")
	update.Values["client_secret"] = ""
	view, err = runner.Save(ctx, "gcal", update)
	if err != nil || !view.Ready {
		t.Fatal("blank secret did not preserve authorization")
	}
	update.Values["client_id"] = "different-client"
	view, err = runner.Save(ctx, "gcal", update)
	if err != nil || view.Ready || view.Secrets["refresh_token"] {
		t.Fatal("changing client did not clear authorization")
	}
	if err := finish(ctx, "stale-code"); err == nil {
		t.Fatal("completed sign-in with changed client")
	}
	_, finish, err = runner.BeginOAuth(ctx, "gcal", "http://localhost/callback", "state", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(ctx, "code"); err != nil {
		t.Fatal(err)
	}
	view, err = runner.DisconnectOAuth(ctx, "gcal")
	if err != nil || view.Ready || view.Secrets["refresh_token"] {
		t.Fatal("disconnect retained authorization")
	}
	update.Values["method"] = "unsupported"
	if _, err := runner.Save(ctx, "gcal", update); err == nil {
		t.Fatal("accepted unsupported method")
	}
	update.Values["method"] = "api_key"
	view, err = runner.Save(ctx, "gcal", update)
	if err != nil || !view.Ready {
		t.Fatal("switching back lost API key settings")
	}
}
