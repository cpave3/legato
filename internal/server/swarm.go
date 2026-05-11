package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SwarmService is the subset of service.SwarmService methods used by HTTP handlers.
type SwarmService interface {
	StartSwarm(ctx context.Context, parentID, workingDir string) error
	Dispatch(ctx context.Context, subtaskID string) error
	Message(ctx context.Context, subtaskID, text string, urgent bool) error
	MessageParent(ctx context.Context, parentID, text string, urgent bool) error
	Broadcast(ctx context.Context, parentID, text string, urgent bool) (int, error)
	Close(ctx context.Context, subtaskID string) error
	Finish(ctx context.Context, parentID, summary string) error
	NextStep(ctx context.Context, parentID string) error
	Snapshot(ctx context.Context, parentID string) ([]byte, error)
	ListSubtaskInfos(ctx context.Context, parentID string) ([]service.SwarmSubtaskInfo, error)
	FetchInbox(ctx context.Context, parentID string) ([]service.InboxEntry, error)
	PeekInbox(ctx context.Context, parentID string) ([]service.InboxEntry, error)
	LoadPlan(path string) (*service.SwarmPlan, error)
	InsertPendingPlan(ctx context.Context, parentTaskID, planPath, replySocket string) error
	GetPendingPlan(ctx context.Context, parentTaskID string) (*store.PendingPlanEntry, error)
	ListAllPendingPlans(ctx context.Context) ([]store.PendingPlanEntry, error)
	DeletePendingPlan(ctx context.Context, parentTaskID string) error
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func (s *Server) swarmStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			ParentTaskID string `json:"parent_task_id"`
			WorkingDir   string `json:"working_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.ParentTaskID == "" {
			s.writeError(w, http.StatusBadRequest, "parent_task_id is required")
			return
		}
		if req.WorkingDir == "" {
			s.writeError(w, http.StatusBadRequest, "working_dir is required")
			return
		}
		if err := s.swarm.StartSwarm(r.Context(), req.ParentTaskID, req.WorkingDir); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			if strings.Contains(err.Error(), "already has a running agent") {
				s.writeError(w, http.StatusConflict, err.Error())
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmDispatchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			SubtaskID string `json:"subtask_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SubtaskID == "" {
			s.writeError(w, http.StatusBadRequest, "subtask_id is required")
			return
		}
		if err := s.swarm.Dispatch(r.Context(), req.SubtaskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "subtask not found")
				return
			}
			if strings.Contains(err.Error(), "deferred") || strings.Contains(err.Error(), "not queued") {
				s.writeError(w, http.StatusConflict, err.Error())
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmMessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			TaskID string `json:"task_id"`
			Text   string `json:"text"`
			Urgent bool   `json:"urgent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" || req.Text == "" {
			s.writeError(w, http.StatusBadRequest, "task_id and text are required")
			return
		}
		// Try subtask first, then parent (conductor).
		err := s.swarm.Message(r.Context(), req.TaskID, req.Text, req.Urgent)
		if errors.Is(err, store.ErrNotFound) {
			err = s.swarm.MessageParent(r.Context(), req.TaskID, req.Text, req.Urgent)
		}
		if err != nil {
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not running") {
				s.writeError(w, http.StatusConflict, err.Error())
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmBroadcastHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			ParentTaskID string `json:"parent_task_id"`
			Text         string `json:"text"`
			Urgent       bool   `json:"urgent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ParentTaskID == "" || req.Text == "" {
			s.writeError(w, http.StatusBadRequest, "parent_task_id and text are required")
			return
		}
		count, err := s.swarm.Broadcast(r.Context(), req.ParentTaskID, req.Text, req.Urgent)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "count": count})
	}
}

func (s *Server) swarmCloseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			SubtaskID string `json:"subtask_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SubtaskID == "" {
			s.writeError(w, http.StatusBadRequest, "subtask_id is required")
			return
		}
		if err := s.swarm.Close(r.Context(), req.SubtaskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "subtask not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmFinishHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			ParentTaskID string `json:"parent_task_id"`
			Summary      string `json:"summary"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ParentTaskID == "" {
			s.writeError(w, http.StatusBadRequest, "parent_task_id is required")
			return
		}
		if err := s.swarm.Finish(r.Context(), req.ParentTaskID, req.Summary); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmNextStepHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		var req struct {
			ParentTaskID string `json:"parent_task_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ParentTaskID == "" {
			s.writeError(w, http.StatusBadRequest, "parent_task_id is required")
			return
		}
		if err := s.swarm.NextStep(r.Context(), req.ParentTaskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			msg := err.Error()
			if strings.Contains(msg, "not terminal") {
				s.writeError(w, http.StatusConflict, msg)
				return
			}
			if strings.Contains(msg, "no more steps") {
				s.writeError(w, http.StatusNotFound, msg)
				return
			}
			s.writeError(w, http.StatusInternalServerError, msg)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) swarmStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/swarm/status/")
		if id == "" || id == r.URL.Path {
			s.writeError(w, http.StatusBadRequest, "parent task ID is required")
			return
		}
		snapshot, err := s.swarm.Snapshot(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		infos, err := s.swarm.ListSubtaskInfos(r.Context(), id)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(snapshot, &payload); err != nil {
			// Return raw snapshot + infos if JSON parse fails
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"snapshot":      string(snapshot),
				"subtask_infos": infos,
			})
			return
		}
		type infoJSON struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			Description string   `json:"description,omitempty"`
			Role        string   `json:"role"`
			AgentKind   string   `json:"agent"`
			Status      string   `json:"status"`
			Scope       []string `json:"scope"`
			WorkerID    *int     `json:"worker_agent_id,omitempty"`
			StartedAt   string   `json:"started_at,omitempty"`
			CompletedAt string   `json:"completed_at,omitempty"`
		}
		var infoOut []infoJSON
		for _, i := range infos {
			infoOut = append(infoOut, infoJSON{
				ID:          i.ID,
				Title:       i.Title,
				Description: i.Description,
				Role:        i.Role,
				AgentKind:   i.AgentKind,
				Status:      i.Status,
				Scope:       i.Scope,
				WorkerID:    i.WorkerID,
				StartedAt:   i.StartedAt,
				CompletedAt: i.CompletedAt,
			})
		}
		payload["subtask_infos"] = infoOut
		s.writeJSON(w, http.StatusOK, payload)
	}
}

func (s *Server) swarmInboxHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/swarm/inbox/")
		if id == "" || id == r.URL.Path {
			s.writeError(w, http.StatusBadRequest, "parent task ID is required")
			return
		}
		peek := r.URL.Query().Get("peek") == "true"
		var entries []service.InboxEntry
		var err error
		if peek {
			entries, err = s.swarm.PeekInbox(r.Context(), id)
		} else {
			entries, err = s.swarm.FetchInbox(r.Context(), id)
		}
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "parent task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
	}
}

func (s *Server) swarmPendingPlanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/swarm/pending-plan/")
		if id == "" || id == r.URL.Path {
			s.writeError(w, http.StatusBadRequest, "parent task ID is required")
			return
		}
		// Read from persistent store (survives server restarts)
		entry, err := s.swarm.GetPendingPlan(r.Context(), id)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if entry == nil {
			s.writeError(w, http.StatusNotFound, "no pending plan")
			return
		}
		plan, loadErr := s.swarm.LoadPlan(entry.PlanPath)
		payload := map[string]interface{}{
			"parent_task_id": id,
			"plan_path":      entry.PlanPath,
			"reply_socket":   entry.ReplySocket,
		}
		if plan != nil {
			payload["plan"] = plan
		} else if loadErr != nil {
			payload["load_error"] = loadErr.Error()
		}
		s.writeJSON(w, http.StatusOK, payload)
	}
}

// swarmPendingPlansHandler returns all non-resolved pending plans.
func (s *Server) swarmPendingPlansHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.swarm == nil {
			s.writeError(w, http.StatusServiceUnavailable, "swarm service not available")
			return
		}
		entries, err := s.swarm.ListAllPendingPlans(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]interface{}, 0, len(entries))
		for _, entry := range entries {
			plan, loadErr := s.swarm.LoadPlan(entry.PlanPath)
			payload := map[string]interface{}{
				"parent_task_id": entry.ParentTaskID,
				"plan_path":      entry.PlanPath,
				"reply_socket":   entry.ReplySocket,
			}
			if plan != nil {
				payload["plan"] = plan
			} else if loadErr != nil {
				payload["load_error"] = loadErr.Error()
			}
			out = append(out, payload)
		}
		s.writeJSON(w, http.StatusOK, out)
	}
}

// handlePlanVerdict receives a plan verdict from a WS client. It forwards the
// verdict to the conductor via IPC, deletes the persistent pending-plan entry,
// and broadcasts a swarm_changed so all clients dismiss the modal.
func (s *Server) handlePlanVerdict(client *wsClient, msg WSMessage) {
	if msg.ReplySocket == "" {
		return
	}
	_ = ipc.Send(msg.ReplySocket, ipc.Message{
		Type:     "plan_verdict",
		Status:   msg.Status,
		Notes:    msg.Notes,
		PlanPath: msg.PlanPath,
	})
	// Remove from persistent store so the plan no longer shows up on fresh loads.
	if msg.ParentTaskID != "" && s.swarm != nil {
		_ = s.swarm.DeletePendingPlan(context.Background(), msg.ParentTaskID)
	}
	// Notify all connected web clients so they dismiss the modal.
	var status string
	switch msg.Status {
	case "approved":
		status = "plan_applied"
	case "rejected":
		status = "rejected"
	default:
		status = msg.Status
	}
	s.hub.Broadcast(WSMessage{
		Type:         MsgSwarmChanged,
		ParentTaskID: msg.ParentTaskID,
		Status:       status,
	})
	if s.bus != nil {
		s.bus.Publish(events.Event{
			Type: events.EventSwarmChanged,
			Payload: events.SwarmChangedPayload{
				ParentTaskID: msg.ParentTaskID,
				NewStatus:    status,
			},
			At: time.Now(),
		})
	}
}
