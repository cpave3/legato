package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SettingsRegistry is the settings of the registry with their values and
// sources (service.SettingsService).
type SettingsRegistry interface {
	List(ctx context.Context) ([]service.SettingValue, error)
	Get(ctx context.Context, key string) (service.SettingValue, error)
	Set(ctx context.Context, key string, raw json.RawMessage, by string) error
	Reset(ctx context.Context, key string) error
	Lists(ctx context.Context) ([]service.SettingList, error)
	SetEntry(ctx context.Context, list, name string, raw json.RawMessage, by string) error
	DeleteEntry(ctx context.Context, list, name string) error
	Export(ctx context.Context, pinned bool) ([]byte, error)
}

// SetSettingsService serves the settings kept in the database.
func (s *Server) SetSettingsService(settings SettingsRegistry) { s.settingsRegistry = settings }

// configSettingsHandler serves GET /api/config/settings: every registered
// setting, with its value, its source and whether it waits for a restart.
func (s *Server) configSettingsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.settingsRegistry == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings are not available")
			return
		}
		list, err := s.settingsRegistry.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"settings": list, "file_only": config.FileOnlySettings()})
	}
}

// configSettingHandler serves PUT /api/config/settings/{key} {value} and
// DELETE (reset to the default).
func (s *Server) configSettingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.settingsRegistry == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings are not available")
			return
		}
		key := r.PathValue("key")
		var err error
		switch r.Method {
		case http.MethodPut:
			var req struct {
				Value json.RawMessage `json:"value"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Value) == 0 {
				s.writeError(w, http.StatusBadRequest, "want {\"value\": …}")
				return
			}
			err = s.settingsRegistry.Set(r.Context(), key, req.Value, "web")
		case http.MethodDelete:
			err = s.settingsRegistry.Reset(r.Context(), key)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		switch {
		case errors.Is(err, config.ErrUnknownSetting):
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		case errors.Is(err, service.ErrSettingPinned):
			s.writeError(w, http.StatusConflict, err.Error())
			return
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.broadcastSettingsChanged()
		value, err := s.settingsRegistry.Get(r.Context(), key)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, value)
	}
}

// broadcastSettingsChanged tells every web UI that settings changed.
func (s *Server) broadcastSettingsChanged() {
	s.hub.Broadcast(WSMessage{Type: MsgSettingsChanged})
}

// configListsHandler serves GET /api/config/lists: every list of named
// entries, the file's (pinned) and the database's.
func (s *Server) configListsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.settingsRegistry == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings are not available")
			return
		}
		lists, err := s.settingsRegistry.Lists(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"lists": lists})
	}
}

// configListEntryHandler serves PUT /api/config/lists/{list}/{name}
// {value} and DELETE.
func (s *Server) configListEntryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.settingsRegistry == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings are not available")
			return
		}
		list, name := r.PathValue("list"), r.PathValue("name")
		var err error
		switch r.Method {
		case http.MethodPut:
			var req struct {
				Value json.RawMessage `json:"value"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Value) == 0 {
				s.writeError(w, http.StatusBadRequest, "want {\"value\": {…}}")
				return
			}
			err = s.settingsRegistry.SetEntry(r.Context(), list, name, req.Value, "web")
		case http.MethodDelete:
			err = s.settingsRegistry.DeleteEntry(r.Context(), list, name)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		switch {
		case errors.Is(err, config.ErrUnknownSetting), errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, config.ErrEntryPinned):
			s.writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
		default:
			s.broadcastSettingsChanged()
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	}
}

// configExportHandler serves GET /api/config/export[?pinned=1]: the
// settings set here as YAML shaped like config.yaml, to download.
func (s *Server) configExportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.settingsRegistry == nil {
			s.writeError(w, http.StatusServiceUnavailable, "settings are not available")
			return
		}
		out, err := s.settingsRegistry.Export(r.Context(), r.URL.Query().Get("pinned") == "1")
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="legato-settings.yaml"`)
		_, _ = w.Write(out)
	}
}
