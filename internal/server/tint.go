package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// TintService is this Legato's tint (service.TintService).
type TintService interface {
	Tint(ctx context.Context) (string, error)
	SetTint(ctx context.Context, tint string) error
}

// SetTintService lets the settings say and change this Legato's tint.
func (s *Server) SetTintService(tints TintService) { s.tints = tints }

// tint is this Legato's tint, "" for none or when it cannot be read.
func (s *Server) tint(ctx context.Context) string {
	if s.tints == nil {
		return ""
	}
	tint, err := s.tints.Tint(ctx)
	if err != nil {
		log.Printf("tint: %v", err)
	}
	return tint
}

// tintHandler serves PUT /api/settings/tint {tint}: one of service.Tints,
// or "" for none. Every web UI using this server is told.
func (s *Server) tintHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.tints == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server cannot be tinted")
			return
		}
		var req struct {
			Tint string `json:"tint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.tints.SetTint(r.Context(), req.Tint); err != nil {
			if errors.Is(err, service.ErrUnknownTint) {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgSettingsChanged})
		s.writeJSON(w, http.StatusOK, map[string]string{"tint": s.tint(r.Context())})
	}
}
