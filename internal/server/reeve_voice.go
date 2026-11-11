package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// ReeveVoice is hands-free voice with the Reeve (service.ReeveVoice).
type ReeveVoice interface {
	Status(ctx context.Context) (service.VoiceReadiness, error)
}

// SetReeveVoice serves hands-free voice with the Reeve.
func (s *Server) SetReeveVoice(voice ReeveVoice) { s.reeveVoice = voice }

// reeveVoiceStatusHandler serves GET /api/reeve/voice: whether a spoken
// conversation will work here, and if not why, in one line.
func (s *Server) reeveVoiceStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reeveVoice == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no voice for the Reeve")
			return
		}
		status, err := s.reeveVoice.Status(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}
