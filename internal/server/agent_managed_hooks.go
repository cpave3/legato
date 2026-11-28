package server

import (
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// agentManagedHooksHandler serves GET /api/agents/{task_id}/managed-hooks:
// the managed hooks the agent's spawn gave it, and why each other one is
// off, as recorded on its execution. runs stays empty until the runs op
// (LEG-699). recorded is false for an execution with no records: one from
// before managed hooks, or started while the folder held none.
func (s *Server) agentManagedHooksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		reader, ok := s.agents.(service.ManagedHooksReader)
		if !ok {
			s.writeError(w, http.StatusNotImplemented, "managed hooks are not available")
			return
		}
		taskID := r.PathValue("task_id")
		hooks, err := reader.AgentManagedHooks(r.Context(), taskID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "agent not found")
			return
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{
			"task_id": taskID, "execution_id": hooks.ExecutionID, "recorded": hooks.Recorded, "hooks": hooks.Hooks, "runs": []any{},
		})
	}
}
