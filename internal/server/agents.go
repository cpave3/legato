package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const (
	defaultSparklineWindow  = 10 * time.Minute
	defaultSparklineBuckets = 10
	agentPreviewMaxRunes    = 4000
)

var agentPreviewANSI = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

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
	// Sandboxed is true when the agent runs in Legato's sandbox.
	Sandboxed bool `json:"sandboxed"`
	// Unsandboxed is true when the sandbox is on and this agent opted out:
	// it sees the person's whole home folder.
	Unsandboxed bool `json:"unsandboxed,omitempty"`
	// ContextUsed and ContextWindow are the last known fill of a native
	// agent's context window; later changes arrive as agent_context.
	ContextUsed   int `json:"context_used,omitempty"`
	ContextWindow int `json:"context_window,omitempty"`
}

func agentResponse(agent service.AgentSession) AgentResponse {
	return AgentResponse{
		ID: agent.ID, TaskID: agent.TaskID, ExecutionID: agent.ExecutionID,
		AttributionTaskID: agent.AttributionTaskID, ExecutionKind: agent.ExecutionKind,
		WorkflowRunID: agent.WorkflowRunID, Title: agent.Title, TmuxSession: agent.TmuxSession,
		Command: agent.Command, AgentKind: agent.AgentKind, Status: agent.Status,
		Activity: agent.Activity, Role: agent.Role, ParentTaskID: agent.ParentTaskID,
		SubtaskID: agent.SubtaskID, StartedAt: agent.StartedAt, EndedAt: agent.EndedAt,
		Sandboxed: agent.Sandboxed,
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
			Title      string   `json:"title"`
			AgentKind  string   `json:"agent_kind"`
			WorkingDir string   `json:"working_dir"`
			Resources  []string `json:"resources"`
			// Sandbox false starts this agent outside Legato's sandbox.
			Sandbox *bool `json:"sandbox"`
			// Prompt is the agent's first message.
			Prompt string `json:"prompt"`
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
		// The spawn dialog writes paths under the home directory with a tilde.
		if s.directories != nil {
			req.WorkingDir = s.directories.Expand(req.WorkingDir)
		}

		resources, err := s.resolveResources(r.Context(), req.Resources)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		opts := service.AgentSpawnOptions{
			AgentKind:   req.AgentKind,
			WorkingDir:  req.WorkingDir,
			Resources:   resources,
			Unsandboxed: req.Sandbox != nil && !*req.Sandbox,
			Prompt:      req.Prompt,
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

		ctx := r.Context()
		cancelSwarm, err := s.isSwarmConductor(ctx, req.TaskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if cancelSwarm {
			if s.swarm == nil {
				http.Error(w, "swarm service not available", http.StatusServiceUnavailable)
				return
			}
			err = s.swarm.CancelSwarm(ctx, req.TaskID)
		} else {
			err = s.agents.KillAgent(ctx, req.TaskID)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (s *Server) isSwarmConductor(ctx context.Context, taskID string) (bool, error) {
	agents, err := s.agents.ListAgents(ctx)
	if err != nil {
		return false, err
	}
	for _, agent := range agents {
		if agent.TaskID == taskID {
			return agent.Role == "conductor", nil
		}
	}
	return false, nil
}

func (s *Server) agentPreviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
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
		if s.agents.HasChimeraEndpoint(r.Context(), taskID) {
			http.Error(w, "preview is not available for native agents", http.StatusConflict)
			return
		}

		output, err := s.agents.CaptureOutput(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "running agent not found", http.StatusNotFound)
				return
			}
			http.Error(w, "capture agent preview: "+err.Error(), http.StatusBadGateway)
			return
		}

		content := strings.TrimSpace(agentPreviewANSI.ReplaceAllString(output, ""))
		if utf8.RuneCountInString(content) > agentPreviewMaxRunes {
			runes := []rune(content)
			content = string(runes[len(runes)-agentPreviewMaxRunes:])
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"task_id":     taskID,
			"content":     content,
			"captured_at": time.Now().UTC(),
		})
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
		var native []string
		for i, a := range agents {
			r := agentResponse(a)
			r.Unsandboxed = s.sandboxEnabled && !a.Sandboxed
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
			if reading, ok := s.agentContexts.Context(a.TaskID); ok {
				r.ContextUsed, r.ContextWindow = reading.Used, reading.Window
			}
			if r.HasChimeraEndpoint && a.Status == "running" {
				native = append(native, a.TaskID)
			}
			resp[i] = r
		}
		s.agentContexts.Reconcile(native)

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
	GetTaskResumeSession(context.Context, string, string) (*service.TaskResumeSession, error)
	SpawnAgent(context.Context, string, int, int, ...service.AgentSpawnOptions) error
}

// messageQueuer sends a message to a running agent.
type messageQueuer interface {
	QueueMessage(ctx context.Context, taskID, message string) error
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
		var req struct {
			AgentKind     string   `json:"agent_kind"`
			WorkingDir    string   `json:"working_dir"`
			SessionPolicy string   `json:"session_policy"`
			Resources     []string `json:"resources"`
			Sandbox       *bool    `json:"sandbox"`
			// Prompt is the agent's first message, or a message to the
			// agent already running.
			Prompt string `json:"prompt"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if _, err := agents.CaptureOutput(r.Context(), taskID); err == nil {
			if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
				queuer, ok := s.agents.(messageQueuer)
				if !ok {
					http.Error(w, "this server cannot message a running agent", http.StatusServiceUnavailable)
					return
				}
				if err := queuer.QueueMessage(r.Context(), taskID, prompt); err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
			}
			s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "created": false})
			return
		}
		// The spawn dialog writes paths under the home directory with a tilde.
		if s.directories != nil {
			req.WorkingDir = s.directories.Expand(req.WorkingDir)
		}
		resources, err := s.resolveResources(r.Context(), req.Resources)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		opts := service.AgentSpawnOptions{AgentKind: req.AgentKind, WorkingDir: req.WorkingDir, Resources: resources, Unsandboxed: req.Sandbox != nil && !*req.Sandbox, Prompt: req.Prompt}
		effectiveAdapter := req.AgentKind
		if effectiveAdapter == "" {
			effectiveAdapter = agents.DefaultAdapter()
		}
		if req.SessionPolicy != "" && req.SessionPolicy != "resume" && req.SessionPolicy != "new" {
			http.Error(w, "session_policy must be resume or new", http.StatusBadRequest)
			return
		}
		if req.SessionPolicy != "" && !service.SupportsTaskResume(effectiveAdapter) {
			http.Error(w, "session_policy requires a resumable adapter", http.StatusBadRequest)
			return
		}
		if service.SupportsTaskResume(effectiveAdapter) {
			saved, err := agents.GetTaskResumeSession(r.Context(), taskID, effectiveAdapter)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if saved == nil && (req.SessionPolicy == "resume" || (req.SessionPolicy == "new" && effectiveAdapter == "chimera")) {
				http.Error(w, "the task no longer has a "+effectiveAdapter+" session to "+req.SessionPolicy, http.StatusConflict)
				return
			}
			opts.SessionPolicy = req.SessionPolicy
			if saved != nil {
				if opts.SessionPolicy == "" {
					opts.SessionPolicy = "resume"
				}
				opts.ResumeSessionID = saved.ID
				opts.ResumeWorkingDir = saved.CWD
			}
		}
		if opts.WorkingDir == "" {
			opts.WorkingDir = s.workDir
			if opts.SessionPolicy == "resume" && opts.ResumeWorkingDir != "" {
				opts.WorkingDir = opts.ResumeWorkingDir
			}
		}
		if err := agents.SpawnAgent(r.Context(), taskID, 0, 0, opts); err != nil {
			if errors.Is(err, service.ErrTaskResumeConflict) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusCreated, map[string]any{"task_id": taskID, "created": true})
	}
}
