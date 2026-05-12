package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/service"
)

const (
	defaultSparklineWindow  = 10 * time.Minute
	defaultSparklineBuckets = 10
)

// SetSparklineWindow configures the window and bucket count used when
// populating AgentResponse.StateTimeline. Zero or negative values fall back to
// the defaults (10 minutes, 10 buckets).
func (s *Server) SetSparklineWindow(window time.Duration, buckets int) {
	if window > 0 {
		s.sparklineWindow = window
	}
	if buckets > 0 {
		s.sparklineBuckets = buckets
	}
}

func (s *Server) sparklineParams() (time.Duration, int) {
	window := s.sparklineWindow
	if window <= 0 {
		window = defaultSparklineWindow
	}
	buckets := s.sparklineBuckets
	if buckets <= 0 {
		buckets = defaultSparklineBuckets
	}
	return window, buckets
}

// AgentResponse is the JSON representation of an agent session.
type AgentResponse struct {
	ID             int        `json:"id"`
	TaskID         string     `json:"task_id"`
	Title          string     `json:"task_title"`
	TmuxSession    string     `json:"tmux_session"`
	Command        string     `json:"command"`
	Status         string     `json:"status"`
	Activity       string     `json:"activity"`
	Role           string     `json:"role,omitempty"`
	ParentTaskID   string     `json:"parent_task_id,omitempty"`
	SubtaskID      string     `json:"subtask_id,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	WorkingSeconds float64    `json:"working_seconds"`
	WaitingSeconds float64    `json:"waiting_seconds"`
	StateTimeline  []string   `json:"state_timeline,omitempty"`
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
			Title      string `json:"title"`
			AgentKind  string `json:"agent_kind"`
			WorkingDir string `json:"working_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Title == "" {
			req.Title = "Ephemeral session"
		}
		if req.WorkingDir == "" {
			req.WorkingDir = s.workDir
		}

		opts := service.AgentSpawnOptions{
			AgentKind:  req.AgentKind,
			WorkingDir: req.WorkingDir,
		}
		if err := s.agents.SpawnEphemeralAgent(context.Background(), req.Title, 0, 0, opts); err != nil {
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
				ID:           a.ID,
				TaskID:       a.TaskID,
				Title:        a.Title,
				TmuxSession:  a.TmuxSession,
				Command:      a.Command,
				Status:       a.Status,
				Activity:     a.Activity,
				Role:         a.Role,
				ParentTaskID: a.ParentTaskID,
				SubtaskID:    a.SubtaskID,
				StartedAt:    a.StartedAt,
				EndedAt:      a.EndedAt,
			}
			if d, ok := durations[a.TaskID]; ok {
				r.WorkingSeconds = d.Working.Seconds()
				r.WaitingSeconds = d.Waiting.Seconds()
			}
			window, buckets := s.sparklineParams()
			timeline, tlErr := s.agents.GetStateTimeline(context.Background(), a.TaskID, window, buckets)
			if tlErr == nil {
				r.StateTimeline = timeline
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
