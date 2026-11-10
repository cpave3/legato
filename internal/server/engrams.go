package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// EngramService says whether engrams are offered and which exist
// (service.EngramService).
type EngramService interface {
	Status(ctx context.Context) service.EngramStatus
	Picker(ctx context.Context, req service.EngramPickerRequest) (service.EngramPicker, error)
	Remember(ctx context.Context, req service.EngramPickerRequest, name string) error
	Bindings(ctx context.Context) (service.EngramBindingsView, error)
	Unbind(ctx context.Context, place, name string) error
	SetDefault(ctx context.Context, place, name string) error
}

// SetEngramService lets the spawn dialog offer engrams.
func (s *Server) SetEngramService(engrams EngramService) { s.engrams = engrams }

// engramsHandler serves GET /api/engrams?dir=…|repo=…&task_id=…: whether
// engrams are offered now, the place, and the engrams in picker order with
// their marks and the one to pre-select. engram is probed on each request.
func (s *Server) engramsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.engrams == nil {
			s.writeJSON(w, http.StatusOK, service.EngramPicker{EngramStatus: service.EngramStatus{Mode: "off"}, Engrams: []service.EngramChoice{}})
			return
		}
		q := r.URL.Query()
		dir := q.Get("dir")
		if s.directories != nil {
			dir = s.directories.Expand(dir)
		}
		picker, err := s.engrams.Picker(r.Context(), service.EngramPickerRequest{Dir: dir, Repo: q.Get("repo"), TaskID: q.Get("task_id")})
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, picker)
	}
}

// rememberEngram is the spawn dialog's Remember: it binds the engram picked
// to where the agent starts and makes it the default there, or with none
// picked clears the default. It never stops the spawn: one it cannot do (a
// job clone named only by its ID) is logged.
func (s *Server) rememberEngram(ctx context.Context, remember bool, dir, repo, name string) {
	if !remember || s.engrams == nil {
		return
	}
	if err := s.engrams.Remember(ctx, service.EngramPickerRequest{Dir: dir, Repo: repo}, name); err != nil {
		log.Printf("engram: remembering %q for %s%s: %v", name, dir, repo, err)
	}
}

// engramBindingsHandler serves Settings' Engram bindings:
// GET /api/engram-bindings lists them by place with the probe's state, and
// DELETE /api/engram-bindings?place=…&engram=… unbinds one.
func (s *Server) engramBindingsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.engrams == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no engram bindings")
			return
		}
		switch r.Method {
		case http.MethodGet:
			view, err := s.engrams.Bindings(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, view)
		case http.MethodDelete:
			place, name := r.URL.Query().Get("place"), r.URL.Query().Get("engram")
			if place == "" || name == "" {
				s.writeError(w, http.StatusBadRequest, "place and engram are required")
				return
			}
			if err := s.engrams.Unbind(r.Context(), place, name); err != nil {
				s.writeError(w, engramBindingStatus(err), err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// engramDefaultHandler serves PUT /api/engram-bindings/default
// {place, engram}: engram, bound to the place, becomes its default; null
// or empty clears the default.
func (s *Server) engramDefaultHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.engrams == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no engram bindings")
			return
		}
		var req struct {
			Place  string  `json:"place"`
			Engram *string `json:"engram"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Place == "" {
			s.writeError(w, http.StatusBadRequest, "place is required")
			return
		}
		name := ""
		if req.Engram != nil {
			name = *req.Engram
		}
		if err := s.engrams.SetDefault(r.Context(), req.Place, name); err != nil {
			s.writeError(w, engramBindingStatus(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func engramBindingStatus(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
