package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SettingsMover moves settings config.yaml pins into the database
// (service.SettingsMover).
type SettingsMover interface {
	Movable(ctx context.Context) (service.SettingsMove, error)
	Move(ctx context.Context, req service.SettingsMove, by string) (store.RestartRecord, error)
}

// SetSettingsMover serves the move; restart restarts Legato after it.
func (s *Server) SetSettingsMover(mover SettingsMover, restart func()) {
	s.settingsMover, s.moveRestart = mover, restart
}

// configMoveHandler serves GET /api/config/move (what can move) and POST
// /api/config/move {settings, entries}: the move, then a restart. It
// answers 202 before Legato restarts.
func (s *Server) configMoveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.settingsMover == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings cannot be moved here")
			return
		}
		switch r.Method {
		case http.MethodGet:
			movable, err := s.settingsMover.Movable(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, movable)
		case http.MethodPost:
			var req service.SettingsMove
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			rec, err := s.settingsMover.Move(r.Context(), req, "web")
			switch {
			case errors.Is(err, service.ErrRestartPending):
				s.writeError(w, http.StatusConflict, err.Error())
				return
			case err != nil:
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeJSON(w, http.StatusAccepted, map[string]any{"restart": rec.ID})
			if s.moveRestart != nil {
				go s.moveRestart()
			}
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
