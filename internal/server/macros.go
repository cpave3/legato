package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MacroSource gives the macros, read at each use.
type MacroSource interface {
	List(ctx context.Context) ([]service.MacroEntry, error)
}

// SetMacros sets the list of macros to serve from /api/macros when no
// macro service is wired.
func (s *Server) SetMacros(m []macros.Macro) {
	s.macros = m
}

// SetMacroService serves /api/macros from the macro service: built-ins,
// config.yaml's and the database's, read at each request.
func (s *Server) SetMacroService(m MacroSource) {
	s.macroSvc = m
}

func (s *Server) macrosHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if s.macroSvc != nil {
			list, err := s.macroSvc.List(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if list == nil {
				list = []service.MacroEntry{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"macros": list})
			return
		}
		_ = json.NewEncoder(w).Encode(macros.ListResult{Macros: s.macros})
	}
}

// macroStarter starts a macro on a task: the service's Start.
type macroStarter interface {
	Start(ctx context.Context, req service.MacroStart) (service.MacroStartResult, error)
}

// macroStartHandler serves POST /api/tasks/{id}/macro {name, pr?}: the
// macro starts the task's agent (201) or is queued to the one running (200).
func (s *Server) macroStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		starter, ok := s.macroSvc.(macroStarter)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "macros cannot start agents here")
			return
		}
		var req struct {
			Name string           `json:"name"`
			PR   *service.MacroPR `json:"pr"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.Name) == "" {
			s.writeError(w, http.StatusBadRequest, "want {\"name\": \"<macro>\", \"pr\": {\"repo\": \"owner/name\", \"pr_number\": 12}}")
			return
		}
		if req.PR != nil && (req.PR.Repo == "" || req.PR.Number <= 0) {
			s.writeError(w, http.StatusBadRequest, "a pull request needs repo and pr_number")
			return
		}
		taskID := r.PathValue("id")
		if _, err := s.board.GetCard(r.Context(), taskID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if away, ok := s.board.(interface {
			RefuseAway(ctx context.Context, taskID string) error
		}); ok {
			if err := away.RefuseAway(r.Context(), taskID); err != nil {
				s.writeError(w, taskErrorStatus(err), err.Error())
				return
			}
		}
		result, err := starter.Start(r.Context(), service.MacroStart{Name: req.Name, TaskID: taskID, PR: req.PR})
		switch {
		case errors.Is(err, service.ErrMacroNotFound), errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		case errors.Is(err, service.ErrTaskResumeConflict):
			s.writeError(w, http.StatusConflict, err.Error())
			return
		case err != nil:
			s.writeError(w, taskErrorStatus(err), err.Error())
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		status := http.StatusOK
		if result.Started {
			status = http.StatusCreated
		}
		s.writeJSON(w, status, result)
	}
}
