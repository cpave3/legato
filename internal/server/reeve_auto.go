package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// ReeveAutoSwitch says what the Reeve runs without asking, and turns all
// of it off (service.ReeveRules).
type ReeveAutoSwitch interface {
	Auto(ctx context.Context) (service.AutoStatus, error)
	AutoOff(ctx context.Context, by string) (service.AutoOffResult, error)
}

func (s *Server) reeveAuto() ReeveAutoSwitch {
	sw, _ := s.reeveRules.(ReeveAutoSwitch)
	return sw
}

// reeveAutoHandler answers GET /api/reeve/auto: whether anything runs
// without the person, what, and the last sets that ran so.
func (s *Server) reeveAutoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sw := s.reeveAuto()
		if sw == nil {
			s.writeError(w, http.StatusServiceUnavailable, "the Reeve's rules are not available")
			return
		}
		status, err := sw.Auto(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

// reeveAutoOffHandler answers POST /api/reeve/auto/off, the off switch:
// every level, rule and host operation at auto back to approve, with no
// signature, since it only makes the Reeve ask more.
func (s *Server) reeveAutoOffHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sw := s.reeveAuto()
		if sw == nil {
			s.writeError(w, http.StatusServiceUnavailable, "the Reeve's rules are not available")
			return
		}
		result, err := sw.AutoOff(r.Context(), "web")
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.broadcastSettingsChanged()
		s.writeJSON(w, http.StatusOK, result)
	}
}
