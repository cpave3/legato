package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// JobCloneService lists and removes the clones Legato made for agents.
type JobCloneService interface {
	ListJobClones(ctx context.Context) ([]service.JobCloneInfo, error)
	RemoveJobClone(ctx context.Context, id int64) error
}

// SetJobCloneService serves /api/job-clones.
func (s *Server) SetJobCloneService(svc JobCloneService) { s.jobClones = svc }

// jobClonesHandler serves GET /api/job-clones: the live job clones, the
// most recently used first, which the spawn dialog offers to work in again.
func (s *Server) jobClonesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.jobClones == nil {
			s.writeError(w, http.StatusServiceUnavailable, "job clones cannot be listed here")
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		clones, err := s.jobClones.ListJobClones(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if clones == nil {
			clones = []service.JobCloneInfo{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"clones": clones})
	}
}

// jobCloneHandler serves DELETE /api/job-clones/{id}: removes the clone now,
// with anything unpushed in it; 409 while an agent works in it.
func (s *Server) jobCloneHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.jobClones == nil {
			s.writeError(w, http.StatusServiceUnavailable, "job clones cannot be removed here")
			return
		}
		if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "a job clone's id is a number")
			return
		}
		switch err := s.jobClones.RemoveJobClone(r.Context(), id); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, service.ErrCloneBusy):
			s.writeError(w, http.StatusConflict, "an agent is working in this clone; stop it first")
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "no such job clone")
		default:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		}
	}
}
