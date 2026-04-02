package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// AgentResponse is the JSON representation of an agent session.
type AgentResponse struct {
	ID          int        `json:"id"`
	TaskID      string     `json:"task_id"`
	Title       string     `json:"task_title"`
	TmuxSession string     `json:"tmux_session"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	Activity    string     `json:"activity"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

func (s *Server) agentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if s.agents == nil {
			json.NewEncoder(w).Encode([]AgentResponse{})
			return
		}

		agents, err := s.agents.ListAgents(context.Background())
		if err != nil {
			http.Error(w, "failed to list agents", http.StatusInternalServerError)
			return
		}

		resp := make([]AgentResponse, len(agents))
		for i, a := range agents {
			resp[i] = AgentResponse{
				ID:          a.ID,
				TaskID:      a.TaskID,
				Title:       a.Title,
				TmuxSession: a.TmuxSession,
				Command:     a.Command,
				Status:      a.Status,
				Activity:    a.Activity,
				StartedAt:   a.StartedAt,
				EndedAt:     a.EndedAt,
			}
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// TasksResponse groups tasks by column.
type TasksResponse map[string][]CardResponse

func (s *Server) tasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx := context.Background()
		columns, err := s.board.ListColumns(ctx)
		if err != nil {
			http.Error(w, "failed to list columns", http.StatusInternalServerError)
			return
		}

		resp := make(TasksResponse)
		for _, col := range columns {
			cards, err := s.board.ListCards(ctx, col.Name)
			if err != nil {
				http.Error(w, "failed to list cards", http.StatusInternalServerError)
				return
			}
			cardResp := make([]CardResponse, len(cards))
			for i, c := range cards {
				cardResp[i] = CardResponse{
					Key:    c.ID,
					Title:  c.Title,
					Status: c.Status,
				}
			}
			resp[col.Name] = cardResp
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
