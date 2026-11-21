package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SessionHistory is the session history: the conversations Legato's agents
// had on this instance (service.SessionHistoryService).
type SessionHistory interface {
	List(ctx context.Context, q service.SessionQuery) (service.SessionPage, error)
	Get(ctx context.Context, id int64) (*service.SessionDetail, error)
	Forget(ctx context.Context, id int64) error
}

// SetSessionHistory serves the session history.
func (s *Server) SetSessionHistory(h SessionHistory) { s.sessionHistory = h }

func (s *Server) registerSessionHistoryRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/sessions", s.sessionHistoryListHandler())
	mux.HandleFunc("GET /api/sessions/{id}", s.sessionHistoryGetHandler())
	mux.HandleFunc("DELETE /api/sessions/{id}", s.sessionHistoryForgetHandler())
}

func (s *Server) haveSessionHistory(w http.ResponseWriter) bool {
	if s.sessionHistory == nil {
		s.writeError(w, http.StatusServiceUnavailable, "the session history is not available on this server")
		return false
	}
	return true
}

func (s *Server) sessionHistoryListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveSessionHistory(w) {
			return
		}
		v := r.URL.Query()
		q := service.SessionQuery{
			Q: v.Get("q"), AgentKind: v.Get("agent_kind"), State: v.Get("state"),
			TaskID: v.Get("task"), Repo: v.Get("repo"), Cursor: v.Get("cursor"),
		}
		if raw := v.Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				s.writeError(w, http.StatusBadRequest, "limit must be a whole number from 1 to 200")
				return
			}
			q.Limit = n
		}
		for _, bound := range []struct {
			name string
			into *time.Time
		}{{"since", &q.Since}, {"until", &q.Until}} {
			if raw := v.Get(bound.name); raw != "" {
				at, err := time.Parse(time.RFC3339, raw)
				if err != nil {
					s.writeError(w, http.StatusBadRequest, bound.name+" must be a time such as 2026-11-21T10:00:00Z")
					return
				}
				*bound.into = at
			}
		}
		page, err := s.sessionHistory.List(r.Context(), q)
		if errors.Is(err, service.ErrBadSessionQuery) {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, page)
	}
}

// sessionID reads the {id} of a session route; false once it has answered.
func (s *Server) sessionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.writeError(w, http.StatusNotFound, "no such session")
		return 0, false
	}
	return id, true
}

func (s *Server) sessionHistoryGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveSessionHistory(w) {
			return
		}
		id, ok := s.sessionID(w, r)
		if !ok {
			return
		}
		detail, err := s.sessionHistory.Get(r.Context(), id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "no such session")
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		default:
			s.writeJSON(w, http.StatusOK, detail)
		}
	}
}

func (s *Server) sessionHistoryForgetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveSessionHistory(w) {
			return
		}
		id, ok := s.sessionID(w, r)
		if !ok {
			return
		}
		err := s.sessionHistory.Forget(r.Context(), id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "no such session")
		case errors.Is(err, service.ErrSessionInUse):
			s.writeError(w, http.StatusConflict, "its agent is running or interrupted: stop or dismiss it first")
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		default:
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	}
}
