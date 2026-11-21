package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// RelatedAgentsService gives the agents on the other side of an agent's
// review loops (service.RelatedAgentsService).
type RelatedAgentsService interface {
	RelatedAgents(ctx context.Context, agentID string) ([]service.RelatedAgent, error)
}

// SetRelatedAgentsService serves /api/agents/{task_id}/related.
func (s *Server) SetRelatedAgentsService(svc RelatedAgentsService) { s.relatedAgents = svc }

// relatedAgentsHandler serves GET /api/agents/{task_id}/related: the
// reviewer of an author's review loops, or a reviewer's author (or the
// linked board task of a run with no author agent), for the chat's Related
// panel. Without the service there are none.
func (s *Server) relatedAgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		rows := []service.RelatedAgent{}
		if s.relatedAgents != nil {
			found, err := s.relatedAgents.RelatedAgents(r.Context(), r.PathValue("task_id"))
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rows = append(rows, found...)
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"related_agents": rows})
	}
}
