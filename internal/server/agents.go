package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// AgentResponse is the JSON representation of an agent session.
type AgentResponse struct {
	ID              int        `json:"id"`
	TaskID          string     `json:"task_id"`
	Title           string     `json:"task_title"`
	TmuxSession     string     `json:"tmux_session"`
	Command         string     `json:"command"`
	Status          string     `json:"status"`
	Activity        string     `json:"activity"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	WorkingSeconds  float64    `json:"working_seconds"`
	WaitingSeconds  float64    `json:"waiting_seconds"`
}

func (s *Server) spawnAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}

		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Title == "" {
			req.Title = "Ephemeral session"
		}

		if err := s.agents.SpawnEphemeralAgent(context.Background(), req.Title, 0, 0); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Notify all clients so they refresh the agent list.
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (s *Server) killAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}

		var req struct {
			TaskID string `json:"task_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}

		if err := s.agents.KillAgent(context.Background(), req.TaskID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
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

		// Fetch durations for all agent tasks.
		taskIDs := make([]string, len(agents))
		for i, a := range agents {
			taskIDs[i] = a.TaskID
		}
		durations, _ := s.agents.GetTaskDurations(context.Background(), taskIDs)

		resp := make([]AgentResponse, len(agents))
		for i, a := range agents {
			r := AgentResponse{
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
			if d, ok := durations[a.TaskID]; ok {
				r.WorkingSeconds = d.Working.Seconds()
				r.WaitingSeconds = d.Waiting.Seconds()
			}
			resp[i] = r
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
