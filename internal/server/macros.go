package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MacroSource gives the macros, read at each use.
type MacroSource interface {
	List(ctx context.Context) ([]service.MacroEntry, error)
}

// SetMacros sets the list of macros to serve from /api/macros when no
// macro service is wired.
func (s *Server) SetMacros(m []config.Macro) {
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
			body := map[string]any{"macros": list, "suggested": service.MacroSuggestionsFor()}
			if src, ok := s.macroSvc.(macroTiers); ok {
				tiers, err := src.Tiers(r.Context())
				if err != nil {
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				body["tiers"] = tiers
			}
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		_ = json.NewEncoder(w).Encode(macroListResult{Macros: s.macros})
	}
}

// macroListResult is the JSON shape returned by GET /api/macros.
type macroListResult struct {
	Macros []config.Macro `json:"macros"`
}

// macroTiers gives each agent tool's tiers, for the macro form.
type macroTiers interface {
	Tiers(ctx context.Context) (map[string][]service.AgentTier, error)
}

// macroWriter changes the macros: the service's Save, SetFavorite, Delete,
// Reset and Rename.
type macroWriter interface {
	Save(ctx context.Context, name string, m config.Macro, by string) error
	SetFavorite(ctx context.Context, name string, favorite bool, by string) error
	Delete(ctx context.Context, name string) error
	Reset(ctx context.Context, name string) error
	Rename(ctx context.Context, from, to, by string) error
}

func (s *Server) macroWriter(w http.ResponseWriter) (macroWriter, bool) {
	writer, ok := s.macroSvc.(macroWriter)
	if !ok {
		s.writeError(w, http.StatusServiceUnavailable, "macros cannot be changed here")
	}
	return writer, ok
}

// macroWritten answers a change to a macro, and tells every client.
func (s *Server) macroWritten(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMacroNotFound), errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrBuiltinMacro), errors.Is(err, service.ErrNotBuiltinMacro),
		errors.Is(err, service.ErrBuiltinMacroName), errors.Is(err, service.ErrMacroNameTaken),
		errors.Is(err, config.ErrEntryPinned):
		s.writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.broadcastSettingsChanged()
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// macroEntryHandler serves PUT /api/macros/{name} (a macro's fields, its
// message as message or keys; {"favorite": true} alone stars it, false
// takes the star away) and DELETE /api/macros/{name}.
func (s *Server) macroEntryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writer, ok := s.macroWriter(w)
		if !ok {
			return
		}
		name := r.PathValue("name")
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var m config.Macro
			if err == nil {
				err = json.Unmarshal(body, &m)
			}
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "want {\"message\": \"…\", \"applies_to\": \"task\", \"agent\": \"codex\", \"tier\": \"large\", \"favorite\": true}: "+err.Error())
				return
			}
			var given struct {
				Keys     *string `json:"keys"`
				Message  *string `json:"message"`
				Favorite *bool   `json:"favorite"`
			}
			_ = json.Unmarshal(body, &given)
			if given.Keys == nil && given.Message == nil && given.Favorite != nil {
				s.macroWritten(w, writer.SetFavorite(r.Context(), name, *given.Favorite, "web"))
				return
			}
			err = writer.Save(r.Context(), name, m, "web")
			if err == nil && given.Favorite != nil {
				err = writer.SetFavorite(r.Context(), name, *given.Favorite, "web")
			}
			s.macroWritten(w, err)
		case http.MethodDelete:
			s.macroWritten(w, writer.Delete(r.Context(), name))
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// macroActionHandler serves POST /api/macros/{name}/reset and
// POST /api/macros/{name}/rename {"name": "<new name>"}.
func (s *Server) macroActionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := r.PathValue("name")
		writer, ok := s.macroWriter(w)
		if !ok {
			return
		}
		if action == "reset" {
			s.macroWritten(w, writer.Reset(r.Context(), name))
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			s.writeError(w, http.StatusBadRequest, "want {\"name\": \"<new name>\"}")
			return
		}
		s.macroWritten(w, writer.Rename(r.Context(), name, req.Name, "web"))
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
			Name    string           `json:"name"`
			PR      *service.MacroPR `json:"pr"`
			Account string           `json:"account"`
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
		result, err := starter.Start(r.Context(), service.MacroStart{Name: req.Name, TaskID: taskID, PR: req.PR, Account: req.Account})
		switch {
		case errors.Is(err, service.ErrMacroNotFound), errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		case errors.Is(err, service.ErrTaskResumeConflict), errors.Is(err, service.ErrMacroFolderGone):
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
