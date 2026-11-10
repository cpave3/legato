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

// reportingAgents reports an engram warning, as a spawn whose engram did
// not boot does.
type reportingAgents struct {
	taskAgentFake
	warning string
}

func (f *reportingAgents) SpawnAgent(ctx context.Context, taskID string, w, h int, options ...service.AgentSpawnOptions) error {
	if f.warning != "" {
		*reportFrom(ctx) = service.SpawnReport{EngramWarning: f.warning}
	} else if len(options) > 0 && options[0].Engram != "" {
		*reportFrom(ctx) = service.SpawnReport{Engram: options[0].Engram}
	}
	return f.taskAgentFake.SpawnAgent(ctx, taskID, w, h, options...)
}

func reportFrom(ctx context.Context) *service.SpawnReport {
	// The handler passes a report through the context; reach it the way
	// SpawnAgent does.
	report := service.SpawnReportFromContext(ctx)
	if report == nil {
		panic("the handler passed no spawn report")
	}
	return report
}

func TestTaskAgentEndpointStartsTheAgentAsAnEngram(t *testing.T) {
	agents := &reportingAgents{}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	server.workDir = "/default"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"codex","engram":"mneme"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if agents.opts.Engram != "mneme" {
		t.Fatalf("opts = %+v", agents.opts)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["engram"] != "mneme" {
		t.Fatalf("body = %v", body)
	}
}

func TestTaskAgentEndpointSaysWhenTheEngramDidNotBoot(t *testing.T) {
	agents := &reportingAgents{warning: "mneme did not boot"}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	server.workDir = "/default"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"engram":"mneme"}`)))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusCreated || body["engram_warning"] != "mneme did not boot" {
		t.Fatalf("status=%d body=%v", rec.Code, body)
	}
}

func TestSpawnEndpointStartsTheAgentAsAnEngram(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents)
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","engram":" mneme "}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || agents.spawned[0].Engram != "mneme" {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
}
