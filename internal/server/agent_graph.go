package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// AgentGraphService gives what the web UI's graph of agents needs beyond the
// agent list (service.AgentGraphService).
type AgentGraphService interface {
	Graph(ctx context.Context) (service.AgentGraph, error)
}

// SetAgentGraphService serves /api/agents/graph.
func (s *Server) SetAgentGraphService(svc AgentGraphService) { s.agentGraph = svc }

// agentGraphHandler serves GET /api/agents/graph: each running agent's
// repository, worktree and badges, and the links between agents that only
// Legato's records show (recent peer messages, review runs, the Reeve's
// watches). Without the service there are none.
func (s *Server) agentGraphHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		graph := service.AgentGraph{Agents: map[string]service.AgentGraphPlace{}, Links: []service.AgentLink{}, PeerWindowSeconds: int(service.PeerMessageWindow.Seconds())}
		if s.agentGraph != nil {
			found, err := s.agentGraph.Graph(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			graph = found
		}
		s.writeJSON(w, http.StatusOK, graph)
	}
}
