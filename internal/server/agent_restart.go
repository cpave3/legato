package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// agentRestartService is the agent service as the restart route needs it.
type agentRestartService interface {
	service.AgentRestarter
	AttachedResources(ctx context.Context, taskID string) ([]string, error)
}

// agentRestartHandler serves POST /api/agents/{task_id}/restart: the running
// agent is stopped and started again with its agent tool, directory and
// resources, resuming its conversation. 409 when it cannot be (a swarm or a
// workflow runs it, or it has no conversation to resume), and it is left
// running.
func (s *Server) agentRestartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.agents.(agentRestartService)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "agent service not available")
			return
		}
		taskID := r.PathValue("task_id")
		names, err := agents.AttachedResources(r.Context(), taskID)
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "no agent is running for "+taskID)
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := agents.RestartAgent(r.Context(), taskID, s.restartResources(r, names)); err != nil {
			switch {
			case errors.Is(err, service.ErrAgentNotRestartable), errors.Is(err, service.ErrTaskResumeConflict):
				s.writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, store.ErrNotFound):
				s.writeError(w, http.StatusNotFound, "no agent is running for "+taskID)
			default:
				s.writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "restarted": true})
	}
}

// restartResources finds the resources an agent had, by name. One removed
// since it was given is left out rather than stopping the restart.
func (s *Server) restartResources(r *http.Request, names []string) []service.Resource {
	if s.resources == nil {
		return nil
	}
	var resources []service.Resource
	for _, name := range names {
		if found, err := s.resources.Resolve(r.Context(), []string{name}); err == nil {
			resources = append(resources, found...)
		}
	}
	return resources
}
