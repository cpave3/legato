package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// agentCompactHandler serves POST /api/agents/{task_id}/compact
// ({"instructions": "…"} optional): the agent's tool compacts its context
// its own way (service.AgentCompactor). 202 once the tool took the request;
// the agent's native events then report compaction_started and
// compaction_finished or compaction_failed. 409 while the agent works, 422
// when Legato cannot compact it or its tool takes no focus instructions,
// each with the reason as the error.
func (s *Server) agentCompactHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, ok := s.agents.(service.AgentCompactor)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "agent service not available")
			return
		}
		var req struct {
			Instructions string `json:"instructions"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			s.writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		err := agents.CompactAgent(context.WithoutCancel(r.Context()), r.PathValue("task_id"), req.Instructions)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusAccepted)
		case errors.Is(err, service.ErrAgentWorking):
			s.writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrCompactUnsupported), errors.Is(err, service.ErrCompactFocusUnsupported):
			s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			s.writeError(w, http.StatusBadGateway, err.Error())
		}
	}
}
