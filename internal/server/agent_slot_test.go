package server

import (
	"context"
	"encoding/json"
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

// twoSlotAgents lists an author and its reviewer on one task.
type twoSlotAgents struct{ promptAgentService }

func (twoSlotAgents) ListAgents(context.Context) ([]service.AgentSession, error) {
	return []service.AgentSession{
		{TaskID: "T-1", CardTaskID: "T-1", AttributionTaskID: "T-1", Slot: "main", Title: "Fix it", Status: "running", Activity: "working", AgentKind: "claude-code"},
		{TaskID: "T-1_reviewer", CardTaskID: "T-1", AttributionTaskID: "T-1", Slot: "reviewer", Title: "Fix it · reviewer", Status: "running", Activity: "working", AgentKind: "codex"},
	}, nil
}

func TestABoardCardShowsItsMainAgentAndAChipForItsReviewer(t *testing.T) {
	board := &mockBoardService{columns: []service.Column{{Name: "Doing"}}, cards: map[string][]service.Card{"Doing": {{ID: "T-1", Title: "Fix it"}}}}
	srv := New(board, &twoSlotAgents{}, nil, "")
	rec := httptest.NewRecorder()
	srv.boardHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/board", nil))

	var resp BoardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	cards := resp.Columns[0].Cards
	if len(cards) != 1 || cards[0].AgentState != "working" || cards[0].AgentKind != "claude-code" {
		t.Fatalf("cards = %+v; want one card with the author's state and tool", cards)
	}
	if len(cards[0].SlotAgents) != 1 || cards[0].SlotAgents[0].Slot != "reviewer" || cards[0].SlotAgents[0].State != "working" {
		t.Fatalf("slot chips = %+v", cards[0].SlotAgents)
	}

	rec = httptest.NewRecorder()
	srv.agentsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var agents []AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[1].TaskID != "T-1_reviewer" || agents[1].CardTaskID != "T-1" || agents[1].Slot != "reviewer" {
		t.Fatalf("agents = %+v", agents)
	}
}
