package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// AgentCompact is an agent in GET /api/agents?view=compact: what a status
// check needs, for the Reeve and scripts.
type AgentCompact struct {
	TaskID     string    `json:"task_id"`
	CardTaskID string    `json:"card_task_id,omitempty"`
	Slot       string    `json:"slot,omitempty"`
	Title      string    `json:"title"`
	Group      string    `json:"group,omitempty"`
	Status     string    `json:"status"`
	Activity   string    `json:"activity"`
	Sandboxed  bool      `json:"sandboxed"`
	StartedAt  time.Time `json:"started_at"`
	Reeve      bool      `json:"reeve,omitempty"`
}

func (s *Server) compactAgents(ctx context.Context, agents []service.AgentSession) []AgentCompact {
	reeveTask := s.reeveTaskID(ctx)
	out := make([]AgentCompact, 0, len(agents))
	for _, a := range agents {
		resp := agentResponse(a)
		out = append(out, AgentCompact{
			TaskID: a.TaskID, CardTaskID: resp.CardTaskID, Slot: resp.Slot, Title: a.Title, Group: a.Group,
			Status: a.Status, Activity: a.Activity, Sandboxed: a.Sandboxed, StartedAt: a.StartedAt,
			Reeve: reeveTask != "" && a.TaskID == reeveTask,
		})
	}
	return out
}

// filterAgents keeps the agents a request's query asks for: task (task IDs,
// comma separated: an agent's own, its card's or the task it works for),
// status (running, dead, …), and workspace (its ID or name).
func (s *Server) filterAgents(r *http.Request, agents []service.AgentSession) ([]service.AgentSession, error) {
	q := r.URL.Query()
	tasks := map[string]bool{}
	for _, id := range strings.Split(q.Get("task"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			tasks[id] = true
		}
	}
	status := strings.TrimSpace(q.Get("status"))
	workspace := -1
	if ws := strings.TrimSpace(q.Get("workspace")); ws != "" {
		id, err := s.workspaceID(r.Context(), ws)
		if err != nil {
			return nil, err
		}
		workspace = id
	}
	if len(tasks) == 0 && status == "" && workspace < 0 {
		return agents, nil
	}
	out := make([]service.AgentSession, 0, len(agents))
	for _, a := range agents {
		card := a.CardTaskID
		if card == "" {
			card = a.TaskID
		}
		if len(tasks) > 0 && !tasks[a.TaskID] && !tasks[card] && !tasks[a.AttributionTaskID] {
			continue
		}
		if status != "" && !strings.EqualFold(a.Status, status) {
			continue
		}
		if workspace >= 0 && !s.inWorkspace(r.Context(), card, workspace) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// workspaceID is a workspace named by its ID or, ignoring case, its name.
func (s *Server) workspaceID(ctx context.Context, ws string) (int, error) {
	if id, err := strconv.Atoi(ws); err == nil {
		return id, nil
	}
	if s.board != nil {
		workspaces, err := s.board.ListWorkspaces(ctx)
		if err != nil {
			return 0, err
		}
		for _, w := range workspaces {
			if strings.EqualFold(w.Name, ws) {
				return w.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("no workspace %q", ws)
}

func (s *Server) inWorkspace(ctx context.Context, taskID string, workspace int) bool {
	if s.board == nil {
		return false
	}
	card, err := s.board.GetCard(ctx, taskID)
	return err == nil && card != nil && card.WorkspaceID != nil && *card.WorkspaceID == workspace
}
