package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeBurnliteSettings struct {
	beginErr    error
	callbackErr error
	saveErr     error
	redirect    string
	address     string
	callback    string
	saved       []string
	disconnects int
}

func (f *fakeBurnliteSettings) Overview(context.Context) (service.BurnliteOverview, error) {
	return service.BurnliteOverview{Projects: []string{}, Connection: service.BurnliteConnection{State: "none"}}, nil
}
func (f *fakeBurnliteSettings) Begin(_ context.Context, address, redirect string) (string, error) {
	f.address, f.redirect = address, redirect
	return "https://burnlite.example.com/api/oauth/authorize?x=1", f.beginErr
}
func (f *fakeBurnliteSettings) Callback(_ context.Context, redirected string) error {
	f.callback = redirected
	return f.callbackErr
}
func (f *fakeBurnliteSettings) SaveProjects(_ context.Context, projects []string) (service.BurnliteOverview, error) {
	f.saved = projects
	return service.BurnliteOverview{Projects: projects}, f.saveErr
}
func (f *fakeBurnliteSettings) Disconnect(context.Context) error { f.disconnects++; return nil }

func burnliteServer(f *fakeBurnliteSettings) http.Handler {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetBurnliteSettings(f)
	return srv.Handler()
}

func TestConnectingBurnliteSendsTheBrowserBackToThisServer(t *testing.T) {
	f := &fakeBurnliteSettings{}
	h := burnliteServer(f)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://legato.lan:3080/api/settings/integrations/burnlite/connect", strings.NewReader(`{"url":"https://burnlite.example.com"}`))
	h.ServeHTTP(w, req)
	var body struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.AuthorizeURL == "" {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if f.redirect != "http://legato.lan:3080/oauth/callback" || f.address != "https://burnlite.example.com" {
		t.Errorf("began %q back to %q", f.address, f.redirect)
	}

	for _, tt := range []struct {
		err  error
		want int
	}{{service.ErrBurnliteInput, http.StatusBadRequest}, {service.ErrSecretsNeedPassphrase, http.StatusConflict}} {
		f.beginErr = tt.err
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/settings/integrations/burnlite/connect", strings.NewReader(`{"url":"x"}`)))
		if w.Code != tt.want {
			t.Errorf("%v: status %d", tt.err, w.Code)
		}
	}
}

func TestTheCallbackFinishesAndReturnsToSettings(t *testing.T) {
	f := &fakeBurnliteSettings{}
	h := burnliteServer(f)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://legato.lan:3080/oauth/callback?code=c&state=s", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/settings?integration=burnlite&result=connected#integrations" {
		t.Fatalf("status %d to %q", w.Code, w.Header().Get("Location"))
	}
	if f.callback != "http://legato.lan:3080/oauth/callback?code=c&state=s" {
		t.Errorf("callback got %q", f.callback)
	}

	f.callbackErr = errors.New("Burnlite did not let Legato in: access_denied")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/oauth/callback?error=access_denied&state=s", nil))
	loc, _ := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusFound || loc.Path != "/settings" || loc.Query().Get("result") != "failed" ||
		!strings.Contains(loc.Query().Get("error"), "access_denied") || loc.Fragment != "integrations" {
		t.Fatalf("status %d to %q", w.Code, w.Header().Get("Location"))
	}
}

func TestBurnliteProjectsAndDisconnect(t *testing.T) {
	f := &fakeBurnliteSettings{}
	h := burnliteServer(f)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/api/settings/integrations/burnlite", strings.NewReader(`{"projects":["EST"]}`)))
	if w.Code != http.StatusOK || len(f.saved) != 1 || !strings.Contains(w.Body.String(), `"projects":["EST"]`) {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	for _, tt := range []struct {
		err  error
		want int
	}{{service.ErrBurnliteInput, http.StatusBadRequest}, {service.ErrProjectsFromConfig, http.StatusConflict}} {
		f.saveErr = tt.err
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("PUT", "/api/settings/integrations/burnlite", strings.NewReader(`{"projects":["x"]}`)))
		if w.Code != tt.want {
			t.Errorf("%v: status %d", tt.err, w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/settings/integrations/burnlite", nil))
	if w.Code != http.StatusOK || f.disconnects != 1 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestBurnliteSettingsAreUnavailableWithoutTheService(t *testing.T) {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/settings/integrations/burnlite/connect", strings.NewReader(`{}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}

type listingSync struct{ mockSyncService }

func (listingSync) TicketProviders(context.Context) []service.TicketProviderInfo {
	return []service.TicketProviderInfo{{Name: "burnlite", Label: "Burnlite", WebOrigin: "https://burnlite.example.com", KeyPattern: service.BurnliteKeyPattern}}
}

func TestTicketProvidersAreListedForTheExtension(t *testing.T) {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/ticket-providers", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"providers":[]}` {
		t.Fatalf("without sync: %d %s", w.Code, w.Body.String())
	}
	srv.SetSyncService(&listingSync{})
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/ticket-providers", nil))
	var body struct {
		Providers []service.TicketProviderInfo `json:"providers"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Providers) != 1 || body.Providers[0].WebOrigin != "https://burnlite.example.com" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
