package server

import (
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
)

// taskNotesHandler is GET /api/tasks/{id}/notes: the task's notes alone,
// oldest first, each with its full body, source and time. A task's
// summary cuts its latest note short; its full read carries the
// description twice.
func (s *Server) taskNotesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		detail, err := s.board.GetCard(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || err == nil && detail == nil {
			s.writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		notes := detail.Notes
		if notes == nil {
			notes = []store.TaskNote{}
		}
		s.writeJSON(w, http.StatusOK, notes)
	}
}

// taskPlansHandler is GET /api/tasks/{id}/plans: the task's plans in
// brief, newest first, whatever their status. GET /api/plans/{plan_id}
// reads one's text.
func (s *Server) taskPlansHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		plans, err := s.plans.TaskPlans(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, plans)
	}
}
