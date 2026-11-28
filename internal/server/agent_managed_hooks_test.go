package server

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestBothSpawnRoutesPassHooksOffToTheSpawn(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents, "work")
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","hooks_off":["codeeval"]}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.spawned) != 1 || !slices.Equal(agents.spawned[0].HooksOff, []string{"codeeval"}) {
		t.Fatalf("spawned = %+v", agents.spawned)
	}

	task := &taskAgentFake{}
	if rec := postTaskAgent(t, task, `{"hooks_off":["codeeval","lint"]}`); rec.Code != http.StatusCreated || !slices.Equal(task.opts.HooksOff, []string{"codeeval", "lint"}) {
		t.Fatalf("task agent: %d %s, opts %+v", rec.Code, rec.Body, task.opts)
	}
}

// managedHooksAgents answers what an agent's spawn recorded.
type managedHooksAgents struct {
	promptAgentService
	hooks service.AgentManagedHooks
	err   error
	asked string
}

func (m *managedHooksAgents) AgentManagedHooks(_ context.Context, taskID string) (service.AgentManagedHooks, error) {
	m.asked = taskID
	return m.hooks, m.err
}

func TestTheAgentsManagedHooksRouteSaysWhatItHasAndWhyTheRestAreOff(t *testing.T) {
	agents := &managedHooksAgents{hooks: service.AgentManagedHooks{ExecutionID: "ex-1", Recorded: true, Hooks: []service.AgentManagedHook{
		{Name: "codeeval", Events: []string{"stop"}, On: true, Off: []string{}, ChosenOff: []string{}},
		{Name: "lint", Events: []string{"stop"}, Off: []string{"off:global", "off:spawn"}, ChosenOff: []string{"spawn"}},
	}}}
	srv := New(nil, agents, nil, "")
	var answer struct {
		TaskID      string                     `json:"task_id"`
		ExecutionID string                     `json:"execution_id"`
		Recorded    bool                       `json:"recorded"`
		Hooks       []service.AgentManagedHook `json:"hooks"`
		Runs        []any                      `json:"runs"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/T-1/managed-hooks", "", &answer); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if agents.asked != "T-1" || answer.TaskID != "T-1" || answer.ExecutionID != "ex-1" || !answer.Recorded || len(answer.Hooks) != 2 ||
		!slices.Equal(answer.Hooks[1].Off, []string{"off:global", "off:spawn"}) || answer.Runs == nil || len(answer.Runs) != 0 {
		t.Fatalf("answer = %+v", answer)
	}

	agents.err = store.ErrNotFound
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/T-2/managed-hooks", "", nil); status != http.StatusNotFound {
		t.Fatalf("no agent: status = %d, want 404", status)
	}
	if status := serveJSON(t, New(nil, &promptAgentService{}, nil, ""), http.MethodGet, "/api/agents/T-1/managed-hooks", "", nil); status != http.StatusNotImplemented {
		t.Fatalf("no reader: status = %d, want 501", status)
	}
}
