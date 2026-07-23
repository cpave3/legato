package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type ArtifactService interface {
	TaskArtifacts(context.Context, string) (*service.TaskArtifacts, error)
}

func (s *Server) SetArtifactService(svc ArtifactService) {
	s.artifacts = svc
}

func (s *Server) taskArtifactsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.artifacts == nil {
			s.writeError(w, http.StatusServiceUnavailable, "artifact service not available")
			return
		}
		artifacts, err := s.artifacts.TaskArtifacts(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, artifacts)
	}
}
