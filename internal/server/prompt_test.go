package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestAnEphemeralSpawnCarriesItsPrompt(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents, "work")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","prompt":"Fix the sync test."}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || agents.spawned[0].Prompt != "Fix the sync test." {
		t.Fatalf("spawned = %+v", agents.spawned)
	}
}

type promptTaskAgents struct {
	taskAgentFake
	queued []string
}

func (p *promptTaskAgents) QueueMessage(_ context.Context, taskID, message string) error {
	p.queued = append(p.queued, taskID+": "+message)
	return nil
}

func postTaskAgent(t *testing.T, agents service.AgentService, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, agents, nil, ":0")
	srv.workDir = "/default"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(body)))
	return rec
}

func TestATaskAgentSpawnCarriesItsPrompt(t *testing.T) {
	agents := &promptTaskAgents{}
	if rec := postTaskAgent(t, agents, `{"agent_kind":"chimera","prompt":"Review the PR."}`); rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if agents.opts.Prompt != "Review the PR." || len(agents.queued) != 0 {
		t.Fatalf("opts %+v queued %v", agents.opts, agents.queued)
	}
}

func TestAPromptForARunningTaskAgentIsAMessage(t *testing.T) {
	agents := &promptTaskAgents{taskAgentFake: taskAgentFake{live: true}}
	if rec := postTaskAgent(t, agents, `{"prompt":"Also update the docs."}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if agents.spawned != "" || len(agents.queued) != 1 || agents.queued[0] != "T-1: Also update the docs." {
		t.Fatalf("spawned %q queued %v", agents.spawned, agents.queued)
	}
}
