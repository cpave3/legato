package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type stubEngrams struct {
	status     service.EngramStatus
	picker     service.EngramPicker
	asked      service.EngramPickerRequest
	remembered []string
	rememberAt service.EngramPickerRequest
}

func (s *stubEngrams) Status(context.Context) service.EngramStatus { return s.status }
func (s *stubEngrams) Picker(_ context.Context, req service.EngramPickerRequest) (service.EngramPicker, error) {
	s.asked = req
	if s.picker.Engrams == nil {
		engrams := []service.EngramChoice{}
		for _, e := range s.status.Engrams {
			engrams = append(engrams, service.EngramChoice{Name: e.Name, Summary: e.Summary})
		}
		return service.EngramPicker{EngramStatus: s.status, Engrams: engrams}, nil
	}
	return s.picker, nil
}
func (s *stubEngrams) Remember(_ context.Context, req service.EngramPickerRequest, name string) error {
	s.rememberAt = req
	s.remembered = append(s.remembered, name)
	return nil
}

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

func TestEngramsAnswersThePickerForAPlace(t *testing.T) {
	stub := &stubEngrams{picker: service.EngramPicker{
		EngramStatus: service.EngramStatus{Enabled: true},
		Place:        &service.EngramPlace{Key: "repo:github.com/cpave3/legato", Label: "github.com/cpave3/legato", Kind: "repo", Default: "argus"},
		Engrams:      []service.EngramChoice{{Name: "mneme", Mark: "last"}, {Name: "argus", Mark: "default", Bound: true}},
		Preselect:    "mneme",
	}}
	srv := New(nil, nil, nil, ":0")
	srv.SetEngramService(stub)
	code, body := getEngrams(t, srv, "?repo=cpave3/legato&task_id=T-1")
	if code != http.StatusOK || body["preselect"] != "mneme" {
		t.Fatalf("GET = %d %v", code, body)
	}
	if stub.asked.Repo != "cpave3/legato" || stub.asked.TaskID != "T-1" {
		t.Fatalf("asked %+v", stub.asked)
	}
	place := body["place"].(map[string]any)
	if place["kind"] != "repo" || place["default"] != "argus" {
		t.Fatalf("place = %v", place)
	}
	first := body["engrams"].([]any)[0].(map[string]any)
	if first["name"] != "mneme" || first["mark"] != "last" {
		t.Fatalf("engrams = %v", body["engrams"])
	}
}

func TestTheSpawnRemembersTheEngramWhenAsked(t *testing.T) {
	agents := &taskAgentFake{}
	stub := &stubEngrams{}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	server.SetEngramService(stub)
	server.workDir = "/default"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"working_dir":"/repo","engram":"mneme","remember_engram":true}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.remembered) != 1 || stub.remembered[0] != "mneme" || stub.rememberAt.Dir != "/repo" {
		t.Fatalf("remembered %v at %+v", stub.remembered, stub.rememberAt)
	}
	// Not ticked: nothing is stored.
	stub.remembered = nil
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"working_dir":"/repo","engram":"mneme"}`)))
	if len(stub.remembered) != 0 {
		t.Fatalf("remembered without being asked: %v", stub.remembered)
	}
}
