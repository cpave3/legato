package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/macros"
)

func TestMacrosHandlerEmpty(t *testing.T) {
	s := &Server{macros: nil}
	h := s.macrosHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/macros", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body macros.ListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Macros) != 0 {
		t.Errorf("len(macros) = %d, want 0", len(body.Macros))
	}
}

func TestMacrosHandlerUsesLowercaseJSONFields(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	s.SetMacros([]macros.Macro{{Name: "run tests", Keys: "task test"}})
	rec := httptest.NewRecorder()
	s.macrosHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/macros", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body["macros"][0]
	if item["name"] != "run tests" || item["keys"] != "task test" {
		t.Fatalf("macro JSON = %s", rec.Body.String())
	}
	if _, exists := item["Name"]; exists {
		t.Fatalf("Go-style field leaked: %s", rec.Body.String())
	}
}

func TestMacrosHandlerPopulated(t *testing.T) {
	s := &Server{macros: []macros.Macro{
		{Name: "run tests", Keys: "task test\n"},
		{Name: "git diff", Keys: "! git diff\n"},
	}}
	h := s.macrosHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/macros", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body macros.ListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Macros) != 2 {
		t.Fatalf("len(macros) = %d, want 2", len(body.Macros))
	}
	if body.Macros[0].Name != "run tests" {
		t.Errorf("macros[0].Name = %q, want %q", body.Macros[0].Name, "run tests")
	}
	if body.Macros[1].Keys != "! git diff\n" {
		t.Errorf("macros[1].Keys = %q, want %q", body.Macros[1].Keys, "! git diff\n")
	}
}

func TestMacrosHandlerAuthProtected(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetAuthToken("secret123")
	h := s.Handler()

	// No auth header
	req := httptest.NewRequest(http.MethodGet, "/api/macros", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}

	// With valid token
	req2 := httptest.NewRequest(http.MethodGet, "/api/macros", nil)
	req2.Header.Set("Authorization", "Bearer secret123")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 with auth, got %d", rec2.Code)
	}
}

func TestMacrosHandlerMethodNotAllowed(t *testing.T) {
	s := &Server{macros: nil}
	h := s.macrosHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/macros", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
