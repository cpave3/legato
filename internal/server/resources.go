package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ResourceService keeps the files and folders agents can be given, and gives
// them to running agents.
type ResourceService interface {
	List(ctx context.Context) ([]service.Resource, error)
	Save(ctx context.Context, resource service.Resource) (service.Resource, error)
	Delete(ctx context.Context, id int64) error
	Resolve(ctx context.Context, names []string) ([]service.Resource, error)
	Attach(ctx context.Context, taskID string, names []string) error
	Attached(ctx context.Context, taskID string) ([]string, error)
}

// SetResourceService gives the server the resources agents can be given.
func (s *Server) SetResourceService(svc ResourceService) {
	s.resources = svc
}

type resourceRequest struct {
	Name  string                 `json:"name"`
	Paths []service.ResourcePath `json:"paths"`
}

func (s *Server) writeResourceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrResourceInvalid):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrNoNativeEndpoint), errors.Is(err, service.ErrSandboxFixed):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// resourcesHandler serves /api/resources: GET lists every resource (the
// configuration's first) and POST {name, paths} saves a new one.
func (s *Server) resourcesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.resources == nil {
			s.writeError(w, http.StatusServiceUnavailable, "resource service not available")
			return
		}
		switch r.Method {
		case http.MethodGet:
			all, err := s.resources.List(r.Context())
			if err != nil {
				s.writeResourceError(w, err)
				return
			}
			if all == nil {
				all = []service.Resource{}
			}
			s.writeJSON(w, http.StatusOK, map[string]any{"resources": all})
		case http.MethodPost:
			var req resourceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			saved, err := s.resources.Save(r.Context(), service.Resource{Name: req.Name, Paths: req.Paths})
			if err != nil {
				s.writeResourceError(w, err)
				return
			}
			s.writeJSON(w, http.StatusCreated, saved)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// resourceHandler serves /api/resources/{id}: PUT {name, paths} replaces a
// saved resource and DELETE removes it.
func (s *Server) resourceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.resources == nil {
			s.writeError(w, http.StatusServiceUnavailable, "resource service not available")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			s.writeError(w, http.StatusBadRequest, "resource id must be a positive number")
			return
		}
		switch r.Method {
		case http.MethodPut:
			var req resourceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			saved, err := s.resources.Save(r.Context(), service.Resource{ID: id, Name: req.Name, Paths: req.Paths})
			if err != nil {
				s.writeResourceError(w, err)
				return
			}
			s.writeJSON(w, http.StatusOK, saved)
		case http.MethodDelete:
			if err := s.resources.Delete(r.Context(), id); err != nil {
				s.writeResourceError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// agentResourcesHandler serves /api/agents/{task_id}/resources: GET names the
// resources the running agent has, and POST {names} gives it more. Both
// answer {attached}.
func (s *Server) agentResourcesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.resources == nil {
			s.writeError(w, http.StatusServiceUnavailable, "resource service not available")
			return
		}
		taskID := r.PathValue("task_id")
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			var req struct {
				Names []string `json:"names"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if err := s.resources.Attach(r.Context(), taskID, req.Names); err != nil {
				s.writeResourceError(w, err)
				return
			}
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		attached, err := s.resources.Attached(r.Context(), taskID)
		if err != nil {
			s.writeResourceError(w, err)
			return
		}
		if attached == nil {
			attached = []string{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"attached": attached})
	}
}

// resolveResources finds the resources a spawn request names. Naming none is
// fine; naming one that does not exist, or any without a resource service, is
// an error, so an agent never starts without what the person picked.
func (s *Server) resolveResources(ctx context.Context, names []string) ([]service.Resource, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if s.resources == nil {
		return nil, errors.New("resources are not available on this server")
	}
	return s.resources.Resolve(ctx, names)
}
