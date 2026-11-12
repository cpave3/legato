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

// ReeveWatches are the agents the Reeve watches (service.ReeveWatcher).
// Watching changes nothing the operator cares about, so its routes are
// reads: the Reeve's own token watches without an approval.
type ReeveWatches interface {
	Watch(ctx context.Context, reeveTaskID, taskID string, events []string) error
	Unwatch(ctx context.Context, taskID string) error
	List(ctx context.Context) ([]service.ReeveWatchInfo, error)
}

// SetReeveWatches serves the Reeve's watches.
func (s *Server) SetReeveWatches(w ReeveWatches) { s.reeveWatches = w }

func (s *Server) registerReeveWatchRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/reeve/watches", s.reeveWatchesHandler())
	mux.HandleFunc("POST /api/reeve/watches", s.reeveWatchHandler())
	mux.HandleFunc("DELETE /api/reeve/watches/{task_id}", s.reeveUnwatchHandler())
}

func (s *Server) haveReeveWatches(w http.ResponseWriter) bool {
	if s.reeveWatches == nil {
		s.writeError(w, http.StatusServiceUnavailable, "watching agents is not available on this server")
		return false
	}
	return true
}

func (s *Server) reeveWatchesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeveWatches(w) {
			return
		}
		watches, err := s.reeveWatches.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"watches": watches, "events": service.ReeveWatchEvents})
	}
}

func (s *Server) reeveWatchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeveWatches(w) {
			return
		}
		var in struct {
			TaskID string   `json:"task_id"`
			Events []string `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.TaskID) == "" {
			s.writeError(w, http.StatusBadRequest, `give {"task_id": "<task>", "events": [...]} (events optional: all when left out)`)
			return
		}
		// The session that asked: the Reeve's own token names it; the
		// operator's watch is for whichever session runs.
		asker := ""
		if id, ok := ReeveCaller(r.Context()); ok {
			asker = id.TaskID
		}
		err := s.reeveWatches.Watch(r.Context(), asker, in.TaskID, in.Events)
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		watches, _ := s.reeveWatches.List(r.Context())
		for _, watch := range watches {
			if watch.TaskID == in.TaskID {
				s.writeJSON(w, http.StatusCreated, watch)
				return
			}
		}
		s.writeJSON(w, http.StatusCreated, map[string]string{"task_id": in.TaskID})
	}
}

func (s *Server) reeveUnwatchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeveWatches(w) {
			return
		}
		err := s.reeveWatches.Unwatch(r.Context(), r.PathValue("task_id"))
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		default:
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	}
}
