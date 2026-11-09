package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// RepositoryGuidanceService is the guidance agents get for the repository
// they work in: config.yaml's, then that saved here.
type RepositoryGuidanceService interface {
	List(ctx context.Context) ([]service.RepositoryGuidance, error)
	Save(ctx context.Context, repo, text string) (service.RepositoryGuidance, error)
	Remove(ctx context.Context, repo string) error
}

// SetRepositoryGuidanceService serves /api/settings/repository-guidance.
func (s *Server) SetRepositoryGuidanceService(svc RepositoryGuidanceService) {
	s.repositoryGuidance = svc
}

func (s *Server) writeRepositoryGuidanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRepositoryGuidanceInvalid):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrRepositoryGuidanceConfigured):
		s.writeError(w, http.StatusConflict, err.Error()+"; change it there")
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "this repository has no guidance saved here")
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// repositoryGuidanceHandler serves /api/settings/repository-guidance: GET
// lists the guidance (config.yaml's first, marked configured), PUT
// {repo, text} sets a repository's, and DELETE ?repo= removes one saved here.
func (s *Server) repositoryGuidanceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.repositoryGuidance == nil {
			s.writeError(w, http.StatusServiceUnavailable, "repository guidance is not available here")
			return
		}
		switch r.Method {
		case http.MethodGet:
			all, err := s.repositoryGuidance.List(r.Context())
			if err != nil {
				s.writeRepositoryGuidanceError(w, err)
				return
			}
			if all == nil {
				all = []service.RepositoryGuidance{}
			}
			s.writeJSON(w, http.StatusOK, map[string]any{"guidance": all})
		case http.MethodPut:
			var req struct {
				Repo string `json:"repo"`
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			saved, err := s.repositoryGuidance.Save(r.Context(), req.Repo, req.Text)
			if err != nil {
				s.writeRepositoryGuidanceError(w, err)
				return
			}
			s.writeJSON(w, http.StatusOK, saved)
		case http.MethodDelete:
			repo := strings.TrimSpace(r.URL.Query().Get("repo"))
			if repo == "" {
				s.writeError(w, http.StatusBadRequest, "name the repository: ?repo=owner/name")
				return
			}
			if err := s.repositoryGuidance.Remove(r.Context(), repo); err != nil {
				s.writeRepositoryGuidanceError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
