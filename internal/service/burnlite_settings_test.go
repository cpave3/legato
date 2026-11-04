package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func newBurnliteSettingsTest(t *testing.T, f *fakeOAuth, source string, opts BurnliteSettingsOptions) (*BurnliteSettings, *store.Store, *memorySecrets) {
	t.Helper()
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	opts.SecretSource = func(context.Context) (string, error) { return source, nil }
	return NewBurnliteSettings(db, NewIntegrationConnector(db, secrets, f.srv.Client()), opts), db, secrets
}

// consent opens the authorize address as a browser would, and gives the
// address Burnlite sent it back to.
func consent(t *testing.T, authorize string) string {
	t.Helper()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(authorize)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Header.Get("Location")
}

func TestConnectingBurnliteFromTheWebUI(t *testing.T) {
	f := newFakeOAuth(t)
	settings, db, secrets := newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{ConfiguredURL: "https://configured.example.com"})
	ctx := context.Background()

	before, err := settings.Overview(ctx)
	if err != nil || before.Connection.State != "none" || before.ConfiguredURL != "https://configured.example.com" || before.Projects == nil {
		t.Fatalf("before = %+v, %v", before, err)
	}

	authorize, err := settings.Begin(ctx, f.srv.URL, "https://legato.local:3080/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if q := mustQuery(t, authorize); q.Get("scope") != BurnliteScope || q.Get("redirect_uri") != "https://legato.local:3080/oauth/callback" {
		t.Errorf("authorize query = %v", q)
	}
	if err := settings.Callback(ctx, consent(t, authorize)); err != nil {
		t.Fatal(err)
	}
	after, _ := settings.Overview(ctx)
	if after.Connection.State != store.IntegrationConnected || after.Connection.URL != f.srv.URL || after.Connection.ConnectedAt == "" {
		t.Fatalf("after = %+v", after)
	}
	if v, _ := secrets.Read(ctx, IntegrationRefreshSecret("burnlite")); v == "" {
		t.Error("no refresh token kept")
	}

	if err := settings.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetIntegration(ctx, "burnlite"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("still connected: %v", err)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestACallbackItCannotPlaceIsRefused(t *testing.T) {
	f := newFakeOAuth(t)
	settings, _, _ := newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{})
	ctx := context.Background()
	if err := settings.Callback(ctx, "https://legato.local/oauth/callback?code=c&state=nobody"); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("err = %v", err)
	}
	// A refusal in Burnlite comes back as an error, and ends the attempt.
	authorize, _ := settings.Begin(ctx, f.srv.URL, "https://legato.local/oauth/callback")
	state := mustQuery(t, authorize).Get("state")
	err := settings.Callback(ctx, "https://legato.local/oauth/callback?error=access_denied&state="+state)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("err = %v", err)
	}
	if err := settings.Callback(ctx, consent(t, authorize)); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("a finished attempt was taken again: %v", err)
	}
}

func TestAnAttemptLeftTooLongIsForgotten(t *testing.T) {
	f := newFakeOAuth(t)
	settings, _, _ := newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{})
	now := time.Now()
	settings.now = func() time.Time { return now }
	ctx := context.Background()
	authorize, _ := settings.Begin(ctx, f.srv.URL, "https://legato.local/oauth/callback")
	back := consent(t, authorize)
	now = now.Add(11 * time.Minute)
	if err := settings.Callback(ctx, back); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("err = %v", err)
	}
}

func TestConnectingRefusesWhatCannotWork(t *testing.T) {
	f := newFakeOAuth(t)
	ctx := context.Background()
	settings, _, _ := newBurnliteSettingsTest(t, f, "passphrase", BurnliteSettingsOptions{})
	if _, err := settings.Begin(ctx, f.srv.URL, "https://legato.local/oauth/callback"); !errors.Is(err, ErrSecretsNeedPassphrase) {
		t.Errorf("passphrase store: err = %v", err)
	}
	settings, _, _ = newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{})
	for _, bad := range []string{"", "burnlite.example.com", "ftp://burnlite.example.com"} {
		if _, err := settings.Begin(ctx, bad, "https://legato.local/oauth/callback"); !errors.Is(err, ErrBurnliteInput) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestProjectsAreSavedUnlessTheConfigurationSetsThem(t *testing.T) {
	f := newFakeOAuth(t)
	ctx := context.Background()
	settings, _, _ := newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{})
	got, err := settings.SaveProjects(ctx, []string{"est", "OPS"})
	if err != nil || strings.Join(got.Projects, ",") != "EST,OPS" || got.ProjectsFromConfig {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := settings.SaveProjects(ctx, []string{"not a key"}); !errors.Is(err, ErrBurnliteInput) {
		t.Errorf("err = %v", err)
	}

	settings, _, _ = newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{ConfiguredProjects: []string{"CFG"}})
	if _, err := settings.SaveProjects(ctx, []string{"EST"}); !errors.Is(err, ErrProjectsFromConfig) {
		t.Errorf("err = %v", err)
	}
	if o, _ := settings.Overview(ctx); !o.ProjectsFromConfig || strings.Join(o.Projects, ",") != "CFG" {
		t.Errorf("overview = %+v", o)
	}
}

func TestTheIntegrationsOverviewShowsBurnlite(t *testing.T) {
	f := newFakeOAuth(t)
	settings, db, _ := newBurnliteSettingsTest(t, f, "auto", BurnliteSettingsOptions{ConfiguredURL: "https://b.example.com"})
	overview := NewIntegrationOverview(db, nil)
	got, err := overview.Overview(context.Background())
	if err != nil || got.Burnlite != nil {
		t.Fatalf("without Burnlite settings: %+v, %v", got.Burnlite, err)
	}
	overview.SetBurnlite(settings)
	got, err = overview.Overview(context.Background())
	if err != nil || got.Burnlite == nil || got.Burnlite.ConfiguredURL != "https://b.example.com" || got.Burnlite.Connection.State != "none" {
		t.Fatalf("with Burnlite settings: %+v, %v", got.Burnlite, err)
	}
}
