package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

const MsgServerThemeChanged = "server_theme_changed"

type ThemePreferenceService interface {
	Current(context.Context) (service.ThemePreference, error)
	Change(context.Context, uint64, string, string) (service.ThemePreference, error)
}

func (s *Server) SetThemePreferenceService(themes ThemePreferenceService) { s.themes = themes }

// StartServerThemeEvents rereads authority in clients after any process saves.
func (s *Server) StartServerThemeEvents(bus *events.Bus) func() {
	return s.broadcastInvalidations(bus, MsgServerThemeChanged, events.EventServerThemeChanged, events.EventServerThemeInvalidated)
}

func (s *Server) themePreferenceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.themes == nil {
			s.writeError(w, http.StatusServiceUnavailable, "server theme preference unavailable")
			return
		}
		if r.Method == http.MethodGet {
			current, err := s.themes.Current(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "could not read server theme")
				return
			}
			s.writeJSON(w, http.StatusOK, current)
			return
		}
		var req struct {
			ID       *string `json:"id"`
			Revision *uint64 `json:"expected_revision"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		if err := decodeJSON(r, &req); err != nil || req.ID == nil || req.Revision == nil {
			s.writeError(w, http.StatusBadRequest, "id and expected_revision are required")
			return
		}
		value, err := s.themes.Change(r.Context(), *req.Revision, *req.ID, "web")
		switch {
		case errors.Is(err, service.ErrUnknownTheme):
			s.writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrThemeConflict):
			s.writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, "could not save server theme")
		default:
			s.writeJSON(w, http.StatusOK, value)
		}
	}
}
