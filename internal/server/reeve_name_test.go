package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// The web UI reads the Reeve's name with its settings, each time, so a
// change shows without a restart; an unnamed Reeve has none.
func TestTheSettingsGiveTheReevesNameAsItIsNow(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	get := func() string {
		var settings struct {
			ReeveName *string `json:"reeve_name"`
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		req.Header.Set("Authorization", "Bearer secret")
		srv.Handler().ServeHTTP(rec, req)
		decode(t, rec.Body.Bytes(), &settings)
		if settings.ReeveName == nil {
			t.Fatal("no reeve_name in /api/settings")
		}
		return *settings.ReeveName
	}
	if got := get(); got != "" {
		t.Fatalf("with no name source: %q", got)
	}
	name := service.ReeveName("Tessitura")
	srv.SetReeveName(func(context.Context) service.ReeveName { return name })
	if got := get(); got != "Tessitura" {
		t.Fatalf("named: %q", got)
	}
	name = "Tess"
	if got := get(); got != "Tess" {
		t.Fatalf("renamed: %q", got)
	}
}
