package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// DoctorReports is the doctor's last report and a way to run it again
// (service.DoctorReports).
type DoctorReports interface {
	// Latest is a recent report, run now when there is none.
	Latest(ctx context.Context) service.DoctorReport
	// Recheck runs every check again.
	Recheck(ctx context.Context) service.DoctorReport
	// Known is the last report without running anything.
	Known() (service.DoctorReport, bool)
}

// SetDoctor serves the checks of what Legato needs from this machine. A
// report whose verdict changes (a run at start, Check again) tells every
// client its settings changed, so setup_incomplete is read again.
func (s *Server) SetDoctor(reports DoctorReports) {
	s.doctor = reports
	if notifier, ok := reports.(interface{ OnVerdictChange(func()) }); ok {
		notifier.OnVerdictChange(func() { s.hub.Broadcast(WSMessage{Type: MsgSettingsChanged}) })
	}
}

// doctorResponse is a report with whether every required check passed.
type doctorResponse struct {
	service.DoctorReport
	OK bool `json:"ok"`
}

func respondDoctor(r service.DoctorReport) doctorResponse {
	return doctorResponse{DoctorReport: r, OK: r.OK()}
}

// doctorHandler serves GET /api/doctor: the report, behind the token, since
// it says which tools this machine has.
func (s *Server) doctorHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.doctor == nil {
			s.writeError(w, http.StatusServiceUnavailable, "the doctor is not available")
			return
		}
		s.writeJSON(w, http.StatusOK, respondDoctor(s.doctor.Latest(r.Context())))
	}
}

// doctorCheckHandler serves POST /api/doctor/check: Check again.
func (s *Server) doctorCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.doctor == nil {
			s.writeError(w, http.StatusServiceUnavailable, "the doctor is not available")
			return
		}
		s.writeJSON(w, http.StatusOK, respondDoctor(s.doctor.Recheck(r.Context())))
	}
}

// setupIncomplete is all /api/meta, which needs no token, says of the
// checks: whether a required one failed in the last report.
func (s *Server) setupIncomplete() bool {
	if s.doctor == nil {
		return false
	}
	report, known := s.doctor.Known()
	return known && !report.OK()
}
