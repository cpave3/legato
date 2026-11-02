package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// RepositoryPresetService is every repository Legato offers: those of the
// configuration and those added here.
type RepositoryPresetService interface {
	List(ctx context.Context) ([]service.RepositoryPreset, error)
	Save(ctx context.Context, p service.RepositoryPreset) (service.RepositoryPreset, error)
	Delete(ctx context.Context, id int64) error
	WorkflowPresets(ctx context.Context) ([]workflowengine.RepositoryPreset, error)
}

// SetRepositoryPresetService serves /api/repositories, and makes the
// settings and the workflow list name its repositories as they are now.
func (s *Server) SetRepositoryPresetService(svc RepositoryPresetService) { s.repositoryPresets = svc }

// repositories are the repository presets now: the service's, or those
// given at start.
func (s *Server) repositories(ctx context.Context) []workflowengine.RepositoryPreset {
	if s.repositoryPresets == nil {
		return append([]workflowengine.RepositoryPreset(nil), s.workflowRepositories...)
	}
	presets, err := s.repositoryPresets.WorkflowPresets(ctx)
	if err != nil {
		log.Printf("repositories: %v", err)
	}
	return presets
}

func (s *Server) writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRepositoryInvalid):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "no such repository was added here")
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// repositoriesHandler serves /api/repositories: GET lists every repository
// (the configuration's first, each saying whether it can be cloned) and
// POST {name, repo, primary_dir, base_branch} adds one.
func (s *Server) repositoriesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.repositoryPresets == nil {
			s.writeError(w, http.StatusServiceUnavailable, "repositories cannot be listed here")
			return
		}
		switch r.Method {
		case http.MethodGet:
			all, err := s.repositoryPresets.List(r.Context())
			if err != nil {
				s.writeRepositoryError(w, err)
				return
			}
			if all == nil {
				all = []service.RepositoryPreset{}
			}
			s.writeJSON(w, http.StatusOK, map[string]any{"repositories": all})
		case http.MethodPost:
			var req struct {
				Name       string `json:"name"`
				Repo       string `json:"repo"`
				PrimaryDir string `json:"primary_dir"`
				BaseBranch string `json:"base_branch"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			saved, err := s.repositoryPresets.Save(r.Context(), service.RepositoryPreset{Name: req.Name, Repo: req.Repo, PrimaryDir: req.PrimaryDir, BaseBranch: req.BaseBranch})
			if err != nil {
				s.writeRepositoryError(w, err)
				return
			}
			s.writeJSON(w, http.StatusCreated, saved)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// repositoryHandler serves /api/repositories/{id}: DELETE removes one added
// here.
func (s *Server) repositoryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.repositoryPresets == nil {
			s.writeError(w, http.StatusServiceUnavailable, "repositories cannot be changed here")
			return
		}
		if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			s.writeError(w, http.StatusBadRequest, "repository id must be a positive number")
			return
		}
		if err := s.repositoryPresets.Delete(r.Context(), id); err != nil {
			s.writeRepositoryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
