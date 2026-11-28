package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// ManagedHookService is Legato's managed hooks, for Settings › Managed hooks.
type ManagedHookService interface {
	Dir() string
	List(ctx context.Context) ([]service.ManagedHookListing, error)
	SetEnabled(ctx context.Context, name string, enabled bool) error
}

// SetManagedHookService serves /api/settings/managed-hooks.
func (s *Server) SetManagedHookService(svc ManagedHookService) {
	s.managedHooks = svc
}

const managedHooksUnavailable = "managed hooks are not available here"

// managedHooksHandler serves GET /api/settings/managed-hooks: the
// managed-hooks folder and every hook in it, found again at each ask, with
// its state and any error. repositories, the repository offs, stays empty
// until they are built.
func (s *Server) managedHooksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.managedHooks == nil {
			s.writeError(w, http.StatusServiceUnavailable, managedHooksUnavailable)
			return
		}
		list, err := s.managedHooks.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"dir": s.managedHooks.Dir(), "hooks": list, "repositories": []any{}})
	}
}

// managedHookHandler serves PUT /api/settings/managed-hooks/{name}
// {enabled}: turns a hook on or off for every agent started from now on,
// and answers the hook.
func (s *Server) managedHookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.managedHooks == nil {
			s.writeError(w, http.StatusServiceUnavailable, managedHooksUnavailable)
			return
		}
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Enabled == nil {
			s.writeError(w, http.StatusBadRequest, "say whether the hook is on: {\"enabled\": true} or false")
			return
		}
		name := r.PathValue("name")
		notFound := "no usable managed hook named " + name + " in the managed-hooks folder"
		if err := s.managedHooks.SetEnabled(r.Context(), name, *req.Enabled); err != nil {
			if errors.Is(err, service.ErrManagedHookNotFound) {
				s.writeError(w, http.StatusNotFound, notFound)
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		list, err := s.managedHooks.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, hook := range list {
			if hook.Name == name {
				s.writeJSON(w, http.StatusOK, hook)
				return
			}
		}
		s.writeError(w, http.StatusNotFound, notFound)
	}
}
