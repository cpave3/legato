package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// TaskStackService gives the stacks of branches a task's agents built with
// Staccato, and their st graph import documents.
type TaskStackService interface {
	List(ctx context.Context, taskID string) ([]service.TaskStack, error)
	Document(ctx context.Context, taskID string, cloneID int64) (service.TaskStackDocument, error)
}

// SetTaskStackService serves /api/tasks/{id}/stacks.
func (s *Server) SetTaskStackService(svc TaskStackService) { s.taskStacks = svc }

// taskStacksHandler serves GET /api/tasks/{id}/stacks: the task's stacks,
// read again from the clones that still exist.
func (s *Server) taskStacksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.taskStacks == nil {
			s.writeError(w, http.StatusServiceUnavailable, "stacks cannot be read here")
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		stacks, err := s.taskStacks.List(r.Context(), r.PathValue("id"))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if stacks == nil {
			stacks = []service.TaskStack{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"stacks": stacks})
	}
}

// taskStackDocumentHandler serves GET /api/tasks/{id}/stacks/{clone}/document:
// the stack's st graph import document as a file to save; 409 with the
// reason when it cannot be imported.
func (s *Server) taskStackDocumentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.taskStacks == nil {
			s.writeError(w, http.StatusServiceUnavailable, "stacks cannot be read here")
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		cloneID, err := strconv.ParseInt(r.PathValue("clone"), 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "a clone's id is a number")
			return
		}
		doc, err := s.taskStacks.Document(r.Context(), r.PathValue("id"), cloneID)
		switch {
		case errors.Is(err, service.ErrStackNotImportable):
			s.writeError(w, http.StatusConflict, err.Error())
			return
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "the task has no stack in that clone")
			return
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+doc.Name+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(doc.Data)
	}
}
