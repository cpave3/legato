package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// slotAgentService records kills of one slot.
type slotAgentService struct {
	promptAgentService
	killedSlot string
}

func (s *slotAgentService) KillAgentInSlot(_ context.Context, taskID, slot string) error {
	s.killedTaskID, s.killedSlot = taskID, slot
	return nil
}

func (s *slotAgentService) GetAgentSessionInSlot(context.Context, string, string) (*store.AgentSession, error) {
	return nil, store.ErrNotFound
}

func (s *slotAgentService) GetTaskResumeSessionInSlot(context.Context, string, string, string) (*service.TaskResumeSession, error) {
	return nil, nil
}

func TestSpawnRoutesRefuseANamedSlotFromThePerson(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/api/agents/spawn", `{"title":"x","slot":"reviewer"}`},
		{"/api/tasks/TASK-1/agent", `{"slot":"reviewer"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "TASK-1"}}, &promptAgentService{}, nil, "")
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.SetPathValue("id", "TASK-1")
			rec := httptest.NewRecorder()

			if strings.HasPrefix(tc.path, "/api/agents") {
				srv.spawnAgentHandler().ServeHTTP(rec, req)
			} else {
				srv.taskAgentHandler().ServeHTTP(rec, req)
			}

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s; want 403", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestKillAgentHandlerKillsTheNamedSlot(t *testing.T) {
	agents := &slotAgentService{promptAgentService: promptAgentService{agent: service.AgentSession{TaskID: "AGENT-1"}}}
	srv := New(nil, agents, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/agents/kill", strings.NewReader(`{"task_id":"AGENT-1","slot":"reviewer"}`))
	rec := httptest.NewRecorder()

	srv.killAgentHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || agents.killedTaskID != "AGENT-1" || agents.killedSlot != "reviewer" {
		t.Fatalf("status %d, killed %q in slot %q", rec.Code, agents.killedTaskID, agents.killedSlot)
	}
}
