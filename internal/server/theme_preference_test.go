package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestServerThemeSettingsCommitReachesMetadataAndLiveClients(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "themes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := events.New()
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetThemePreferenceService(service.NewThemePreferenceService(db, bus))
	stop := srv.StartServerThemeEvents(bus)
	defer stop()
	messages := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { messages <- msg }})
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := call("PUT", "/api/settings/theme", `{"id":"ocular-light-vivid","expected_revision":0}`, ""); rec.Code != 401 {
		t.Fatalf("unauthorized %d %s", rec.Code, rec.Body)
	}
	rec := call("PUT", "/api/settings/theme", `{"id":"ocular-light-vivid","expected_revision":0}`, "secret")
	if rec.Code != 200 {
		t.Fatalf("save %d %s", rec.Code, rec.Body)
	}
	var saved service.ThemePreference
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-messages:
		if msg.Type != MsgServerThemeChanged {
			t.Fatalf("event %s", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("no live invalidation")
	}
	meta := call(http.MethodGet, "/api/meta", "", "")
	var value struct {
		ThemePreference *service.ThemePreference `json:"theme_preference"`
	}
	if err := json.Unmarshal(meta.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if meta.Code != 200 || value.ThemePreference == nil || *value.ThemePreference != saved || saved.ID != "ocular-light-vivid" || saved.Revision != 1 {
		t.Fatalf("public metadata: %d %s", meta.Code, meta.Body)
	}
	if rec := call("PUT", "/api/settings/theme", `{"id":"dracula","expected_revision":0}`, "secret"); rec.Code != 409 {
		t.Fatalf("stale %d %s", rec.Code, rec.Body)
	}
	authority, _ := service.NewThemePreferenceService(db, nil).Current(context.Background())
	if authority != saved {
		t.Fatal("stale write mutated authority")
	}
}
