package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// interruptedAgentDTO is an agent a crash interrupted, as the web UI lists it.
type interruptedAgentDTO struct {
	TaskID        string   `json:"task_id"`
	Title         string   `json:"title"`
	AgentKind     string   `json:"agent_kind"`
	WorkingDir    string   `json:"working_dir,omitempty"`
	Engram        string   `json:"engram,omitempty"`
	Sandboxed     bool     `json:"sandboxed"`
	Ephemeral     bool     `json:"ephemeral"`
	Resources     []string `json:"resources"`
	InterruptedAt string   `json:"interrupted_at,omitempty"`
	Resumable     bool     `json:"resumable"`
	CanStartNew   bool     `json:"can_start_new"`
	Reason        string   `json:"reason,omitempty"`
}

func (s *Server) interruptedAgents(w http.ResponseWriter) (service.InterruptedAgents, bool) {
	agents, ok := s.agents.(service.InterruptedAgents)
	if !ok {
		s.writeError(w, http.StatusServiceUnavailable, "this Legato does not keep interrupted agents")
	}
	return agents, ok
}

// interruptedAgentsHandler serves GET /api/agents/interrupted: the agents
// that were running when Legato last stopped and whose tmux session was
// gone when it started again.
func (s *Server) interruptedAgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.interruptedAgents(w)
		if !ok {
			return
		}
		list, err := agents.ListInterruptedAgents(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]interruptedAgentDTO, 0, len(list))
		for _, a := range list {
			dto := interruptedAgentDTO{
				TaskID: a.TaskID, Title: a.Title, AgentKind: a.AgentKind, WorkingDir: a.WorkingDir, Engram: a.Engram,
				Sandboxed: a.Sandboxed, Ephemeral: a.Ephemeral, Resources: a.Resources,
				Resumable: a.Resumable, CanStartNew: a.CanStartNew, Reason: a.Reason,
			}
			if dto.Resources == nil {
				dto.Resources = []string{}
			}
			if !a.InterruptedAt.IsZero() {
				dto.InterruptedAt = a.InterruptedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, dto)
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"agents": out})
	}
}

// interruptedResumeHandler serves POST /api/agents/{task_id}/resume: the
// interrupted agent starts again as it was, resuming its conversation, or
// with {"fresh": true} a new agent starts on its task. 409 with the reason
// when it cannot be; 404 when the task has no interrupted agent.
func (s *Server) interruptedResumeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.interruptedAgents(w)
		if !ok {
			return
		}
		var req struct {
			Fresh bool `json:"fresh"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			s.writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		taskID := r.PathValue("task_id")
		if err := agents.ResumeInterruptedAgent(r.Context(), taskID, req.Fresh); err != nil {
			s.writeInterruptedError(w, taskID, err)
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "resumed": true})
	}
}

// interruptedDismissHandler serves POST /api/agents/{task_id}/dismiss: the
// interrupted agent is marked ended.
func (s *Server) interruptedDismissHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.interruptedAgents(w)
		if !ok {
			return
		}
		taskID := r.PathValue("task_id")
		if err := agents.DismissInterruptedAgent(r.Context(), taskID); err != nil {
			s.writeInterruptedError(w, taskID, err)
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "dismissed": true})
	}
}

// interruptedResumeAllHandler serves POST /api/agents/interrupted/resume-all:
// every interrupted agent that can be resumed is, and the answer says how
// each went.
func (s *Server) interruptedResumeAllHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.interruptedAgents(w)
		if !ok {
			return
		}
		type result struct {
			TaskID string `json:"task_id"`
			Error  string `json:"error,omitempty"`
		}
		results := []result{}
		for _, r := range agents.ResumeAllInterruptedAgents(r.Context()) {
			entry := result{TaskID: r.TaskID}
			if r.Err != nil {
				entry.Error = r.Err.Error()
			}
			results = append(results, entry)
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

func (s *Server) writeInterruptedError(w http.ResponseWriter, taskID string, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, taskID+" has no interrupted agent")
	case errors.Is(err, service.ErrAgentNotResumable), errors.Is(err, service.ErrTaskResumeConflict):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}
