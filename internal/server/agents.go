package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const (
	defaultSparklineWindow  = 10 * time.Minute
	defaultSparklineBuckets = 10
)

// SetNtfyConfigured records whether an ntfy channel is wired so the UI
// can conditionally show notification toggles.
func (s *Server) SetNtfyConfigured(v bool) {
	s.ntfyConfigured = v
}

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
	ID                 int        `json:"id"`
	TaskID             string     `json:"task_id"`
	ExecutionID        string     `json:"execution_id,omitempty"`
	AttributionTaskID  string     `json:"attribution_task_id"`
	ExecutionKind      string     `json:"execution_kind,omitempty"`
	WorkflowRunID      string     `json:"workflow_run_id,omitempty"`
	Title              string     `json:"task_title"`
	TmuxSession        string     `json:"tmux_session"`
	Command            string     `json:"command"`
	AgentKind          string     `json:"agent_kind"`
	Status             string     `json:"status"`
	Activity           string     `json:"activity"`
	Role               string     `json:"role,omitempty"`
	ParentTaskID       string     `json:"parent_task_id,omitempty"`
	SubtaskID          string     `json:"subtask_id,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	WorkingSeconds     float64    `json:"working_seconds"`
	WaitingSeconds     float64    `json:"waiting_seconds"`
	ErrorSeconds       float64    `json:"error_seconds"`
	StateTimeline      []string   `json:"state_timeline,omitempty"`
	NotifyEnabled      bool       `json:"notify_enabled"`
	HasChimeraEndpoint bool       `json:"has_chimera_endpoint"`
}

func agentResponse(agent service.AgentSession) AgentResponse {
	return AgentResponse{
		ID: agent.ID, TaskID: agent.TaskID, ExecutionID: agent.ExecutionID,
		AttributionTaskID: agent.AttributionTaskID, ExecutionKind: agent.ExecutionKind,
		WorkflowRunID: agent.WorkflowRunID, Title: agent.Title, TmuxSession: agent.TmuxSession,
		Command: agent.Command, AgentKind: agent.AgentKind, Status: agent.Status,
		Activity: agent.Activity, Role: agent.Role, ParentTaskID: agent.ParentTaskID,
		SubtaskID: agent.SubtaskID, StartedAt: agent.StartedAt, EndedAt: agent.EndedAt,
	}
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
			r := agentResponse(a)
			if d, ok := durations[a.TaskID]; ok {
				r.WorkingSeconds = d.Working.Seconds()
				r.WaitingSeconds = d.Waiting.Seconds()
				r.ErrorSeconds = d.Error.Seconds()
			}
			window, buckets := s.sparklineParams()
			timeline, tlErr := s.agents.GetStateTimeline(context.Background(), a.TaskID, window, buckets)
			if tlErr == nil {
				r.StateTimeline = timeline
			}
			if s.agents != nil {
				r.NotifyEnabled, _ = s.agents.GetTaskNotifyEnabled(context.Background(), a.TaskID)
				r.HasChimeraEndpoint = s.agents.HasChimeraEndpoint(context.Background(), a.TaskID)
			}
			resp[i] = r
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) agentNotifyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}

		taskID := r.PathValue("task_id")
		if taskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}

		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := s.agents.SetTaskNotifyEnabled(context.Background(), taskID, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"enabled": req.Enabled})
	}
}

func (s *Server) tasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
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
						ID:     c.ID,
						Title:  c.Title,
						Status: c.Status,
					}
				}
				resp[col.Name] = cardResp
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read body", http.StatusBadRequest)
				return
			}
			var req struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Column      string `json:"column"`
				Priority    string `json:"priority"`
				WorkspaceID *int   `json:"workspace_id,omitempty"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(body, &raw)
			if _, specified := raw["workspace_id"]; !specified {
				req.WorkspaceID = s.defaultWorkspaceID
			}
			if req.Title == "" || req.Column == "" {
				http.Error(w, "title and column are required", http.StatusBadRequest)
				return
			}
			ctx := context.Background()
			card, err := s.board.CreateTask(ctx, req.Title, req.Description, req.Column, req.Priority, req.WorkspaceID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.notifyTaskChanged(card.ID)
			resp := CardResponse{ID: card.ID, Title: card.Title, Priority: card.Priority, Status: card.Status}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(resp)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

type taskAgentService interface {
	CaptureOutput(context.Context, string) (string, error)
	DefaultAdapter() string
	GetTaskChimeraSessionID(context.Context, string) (string, error)
	SpawnAgent(context.Context, string, int, int, ...service.AgentSpawnOptions) error
}

func (s *Server) taskAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		agents, ok := s.agents.(taskAgentService)
		if !ok {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}
		taskID := r.PathValue("id")
		if _, err := s.board.GetCard(r.Context(), taskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := agents.CaptureOutput(r.Context(), taskID); err == nil {
			s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "created": false})
			return
		}
		var req struct {
			AgentKind            string `json:"agent_kind"`
			WorkingDir           string `json:"working_dir"`
			ChimeraSessionPolicy string `json:"chimera_session_policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.WorkingDir == "" {
			req.WorkingDir = s.workDir
		}
		opts := service.AgentSpawnOptions{AgentKind: req.AgentKind, WorkingDir: req.WorkingDir}
		effectiveAdapter := req.AgentKind
		if effectiveAdapter == "" {
			effectiveAdapter = agents.DefaultAdapter()
		}
		if req.ChimeraSessionPolicy != "" && req.ChimeraSessionPolicy != "resume" && req.ChimeraSessionPolicy != "new" {
			http.Error(w, "chimera_session_policy must be resume or new", http.StatusBadRequest)
			return
		}
		if req.ChimeraSessionPolicy != "" && effectiveAdapter != "chimera" {
			http.Error(w, "chimera_session_policy requires the Chimera adapter", http.StatusBadRequest)
			return
		}
		if effectiveAdapter == "chimera" {
			sessionID, err := agents.GetTaskChimeraSessionID(r.Context(), taskID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if req.ChimeraSessionPolicy != "" && sessionID == "" {
				http.Error(w, "the task no longer has a Chimera session to "+req.ChimeraSessionPolicy, http.StatusConflict)
				return
			}
			if sessionID != "" {
				opts.ChimeraSessionExists = req.ChimeraSessionPolicy
				if opts.ChimeraSessionExists == "" {
					opts.ChimeraSessionExists = "resume"
				}
				opts.ChimeraSessionID = sessionID
			}
		}
		if err := agents.SpawnAgent(r.Context(), taskID, 0, 0, opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusCreated, map[string]any{"task_id": taskID, "created": true})
	}
}
