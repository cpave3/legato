package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// MsgUpdateChanged says where this Legato's updates stand changed: a check,
// a new mode, an update installing or installed.
const MsgUpdateChanged = "update_changed"

// UpdateService keeps this Legato on the newest release
// (service.UpdateService).
type UpdateService interface {
	Status(ctx context.Context) service.UpdateStatus
	Check(ctx context.Context) (service.UpdateStatus, error)
	Apply(ctx context.Context) error
	ApplyTool(ctx context.Context, name string) error
	SetMode(ctx context.Context, mode string) error
}

// SetUpdateService serves the updates of this Legato.
func (s *Server) SetUpdateService(updates UpdateService) { s.updates = updates }

// updatesHandler serves GET /api/updates: where updates stand.
func (s *Server) updatesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.haveUpdates(w) {
			return
		}
		s.writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
	}
}

// updateCheckHandler serves POST /api/updates/check: asks GitHub now.
func (s *Server) updateCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.haveUpdates(w) {
			return
		}
		status, err := s.updates.Check(r.Context())
		switch {
		case errors.Is(err, service.ErrNotARelease):
			s.writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			s.writeError(w, http.StatusBadGateway, err.Error())
		default:
			s.writeJSON(w, http.StatusOK, status)
		}
	}
}

// updateApplyHandler serves POST /api/updates/apply: installs the newest
// release and restarts on it. It answers 202 at once; the update goes on
// in the background and update_changed tells how it goes.
func (s *Server) updateApplyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.haveUpdates(w) {
			return
		}
		status := s.updates.Status(r.Context())
		if !status.Release {
			s.writeError(w, http.StatusConflict, service.ErrNotARelease.Error())
			return
		}
		if !status.Available {
			s.writeError(w, http.StatusConflict, service.ErrNoUpdate.Error())
			return
		}
		go func() {
			if err := s.updates.Apply(context.Background()); err != nil {
				log.Printf("updates: %v", err)
			}
		}()
		s.writeJSON(w, http.StatusAccepted, status)
	}
}

// updateToolApplyHandler serves POST /api/updates/tools/{name}/apply:
// installs a tool's newest release (st update). It answers 202 at once;
// update_changed tells how it goes. 409 for a tool the updater does not
// look after or with nothing newer.
func (s *Server) updateToolApplyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.haveUpdates(w) {
			return
		}
		name := r.PathValue("name")
		status := s.updates.Status(r.Context())
		var tool *service.ToolStatus
		for i := range status.Tools {
			if status.Tools[i].Name == name {
				tool = &status.Tools[i]
			}
		}
		switch {
		case tool == nil || !tool.Managed:
			s.writeError(w, http.StatusConflict, service.ErrToolNotManaged.Error())
			return
		case !tool.Available:
			s.writeError(w, http.StatusConflict, service.ErrNoUpdate.Error())
			return
		}
		go func() {
			if err := s.updates.ApplyTool(context.Background(), name); err != nil {
				log.Printf("updates: %v", err)
			}
		}()
		s.writeJSON(w, http.StatusAccepted, status)
	}
}

// updateModeHandler serves PUT /api/settings/updates {mode}: auto, ask or
// off.
func (s *Server) updateModeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.haveUpdates(w) {
			return
		}
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.updates.SetMode(r.Context(), req.Mode); err != nil {
			if errors.Is(err, service.ErrUnknownUpdateMode) {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
	}
}

func (s *Server) haveUpdates(w http.ResponseWriter) bool {
	if s.updates == nil {
		s.writeError(w, http.StatusServiceUnavailable, "this server does not update itself")
		return false
	}
	return true
}

// StartUpdateEvents forwards EventUpdateChanged to web clients.
func (s *Server) StartUpdateEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventUpdateChanged)
	go func() {
		for range ch {
			s.hub.Broadcast(WSMessage{Type: MsgUpdateChanged})
		}
	}()
}
