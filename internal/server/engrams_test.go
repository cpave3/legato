package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type stubEngrams struct{ status service.EngramStatus }

func (s *stubEngrams) Status(context.Context) service.EngramStatus { return s.status }

func getEngrams(t *testing.T, srv *Server, query string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/engrams"+query, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestEngramsListsTheEngramsWhenEnabled(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetEngramService(&stubEngrams{status: service.EngramStatus{Enabled: true, Mode: "auto", Found: true, Engrams: []service.EngramIdentity{{Name: "mneme", Summary: "Keeps notes"}}}})
	code, body := getEngrams(t, srv, "")
	if code != http.StatusOK || body["enabled"] != true {
		t.Fatalf("GET /api/engrams = %d %v", code, body)
	}
	list := body["engrams"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "mneme" || list[0].(map[string]any)["summary"] != "Keeps notes" {
		t.Fatalf("engrams = %v", list)
	}
}

func TestEngramsSaysNotFound(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetEngramService(&stubEngrams{status: service.EngramStatus{Mode: "auto", Error: "engram was not found", Engrams: []service.EngramIdentity{}}})
	code, body := getEngrams(t, srv, "")
	if code != http.StatusOK || body["enabled"] != false || body["error"] == nil {
		t.Fatalf("GET /api/engrams = %d %v", code, body)
	}
}

func TestEngramsWithoutAServiceIsDisabled(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	code, body := getEngrams(t, srv, "")
	if code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("GET /api/engrams = %d %v", code, body)
	}
}
