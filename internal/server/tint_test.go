package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func tintServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "tint.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := New(nil, nil, nil, ":0")
	srv.SetTintService(service.NewTintService(db))
	return srv
}

// A Legato's tint is set in its settings, and every web UI that uses it
// (and the switcher of another, without a token) reads it.
func TestALegatosTintIsSetAndShown(t *testing.T) {
	srv := tintServer(t)
	srv.SetAuthToken("secret")
	ch := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { ch <- msg }})

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/settings/tint", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := put(`{"tint":"purple"}`); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	select {
	case msg := <-ch:
		if msg.Type != MsgSettingsChanged {
			t.Fatalf("broadcast %q", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("the other web UIs were not told")
	}
	if code := put(`{"tint":"chartreuse"}`); code != http.StatusBadRequest {
		t.Fatalf("an unknown colour: %d", code)
	}

	var settings struct {
		Tint string `json:"tint"`
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(rec, req)
	decode(t, rec.Body.Bytes(), &settings)
	if settings.Tint != "purple" {
		t.Fatalf("settings tint = %q", settings.Tint)
	}

	var meta struct {
		Tint string `json:"tint"`
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta", nil))
	decode(t, rec.Body.Bytes(), &meta)
	if rec.Code != http.StatusOK || meta.Tint != "purple" {
		t.Fatalf("meta without a token: %d %+v", rec.Code, meta)
	}
}

func TestATintCannotBeSetWithoutTheService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodPut, "/api/settings/tint", `{"tint":"purple"}`, nil); status != http.StatusServiceUnavailable {
		t.Fatalf("status %d", status)
	}
}
