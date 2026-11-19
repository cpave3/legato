package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeMacroStarter struct {
	fakeMacroSource
	got    service.MacroStart
	result service.MacroStartResult
	err    error
}

func (f *fakeMacroStarter) Start(_ context.Context, req service.MacroStart) (service.MacroStartResult, error) {
	f.got = req
	return f.result, f.err
}

func postMacro(t *testing.T, s *Server, task, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/"+task+"/macro", strings.NewReader(body)))
	return rec
}

func TestTheMacroEndpointStartsAMacroOnATaskAndPR(t *testing.T) {
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	s := New(board, &taskAgentFake{}, nil, ":0")
	starter := &fakeMacroStarter{result: service.MacroStartResult{TaskID: "T-1", Started: true, AgentKind: "codex"}}
	s.SetMacroService(starter)
	rec := postMacro(t, s, "T-1", `{"name":"Review","pr":{"repo":"acme/web","pr_number":42,"url":"https://github.com/acme/web/pull/42"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if starter.got.Name != "Review" || starter.got.TaskID != "T-1" || starter.got.PR == nil || starter.got.PR.Number != 42 || starter.got.PR.Repo != "acme/web" {
		t.Fatalf("start = %+v", starter.got)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["started"] != true || body["agent_kind"] != "codex" {
		t.Fatalf("body = %v", body)
	}
	starter.result = service.MacroStartResult{TaskID: "T-1", Queued: true}
	if rec := postMacro(t, s, "T-1", `{"name":"Plan"}`); rec.Code != http.StatusOK || starter.got.PR != nil {
		t.Fatalf("queued: status = %d, pr = %v", rec.Code, starter.got.PR)
	}
}

func TestTheMacroEndpointRefusesWhatItCannotStart(t *testing.T) {
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	s := New(board, &taskAgentFake{}, nil, ":0")
	starter := &fakeMacroStarter{}
	s.SetMacroService(starter)
	if rec := postMacro(t, s, "T-1", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no name: %d", rec.Code)
	}
	starter.err = fmt.Errorf("%q: %w", "x", service.ErrMacroNotFound)
	if rec := postMacro(t, s, "T-1", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown macro: %d", rec.Code)
	}
	board.cardErr = store.ErrNotFound
	if rec := postMacro(t, s, "T-9", `{"name":"Plan"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task: %d", rec.Code)
	}
}

func TestTheTaskAgentEndpointTakesATier(t *testing.T) {
	agents := &taskAgentFake{}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	s := New(board, agents, nil, ":0")
	s.workDir = "/default"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"codex","tier":"smart"}`)))
	if rec.Code != http.StatusCreated || agents.opts.Tier != "smart" {
		t.Fatalf("status = %d, tier = %q", rec.Code, agents.opts.Tier)
	}
}
