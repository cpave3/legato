package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// SetupChecklist is what is left to set up (service.SetupChecklist).
type SetupChecklist interface {
	Status(ctx context.Context) service.SetupStatus
	SetDismissed(ctx context.Context, dismissed bool) error
}

// SetSetupChecklist serves the web UI's setup checklist.
func (s *Server) SetSetupChecklist(list SetupChecklist) { s.setup = list }

// setupSummary is the part of the checklist /api/settings carries, so the
// chip costs no request of its own.
type setupSummary struct {
	Show  bool `json:"show"`
	Done  int  `json:"done"`
	Total int  `json:"total"`
}

func (s *Server) setupSummary(ctx context.Context) *setupSummary {
	if s.setup == nil {
		return nil
	}
	st := s.setup.Status(ctx)
	return &setupSummary{Show: st.Show, Done: st.Done, Total: st.Total}
}

// setupHandler serves GET /api/setup: the checklist.
func (s *Server) setupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.setup == nil {
			s.writeError(w, http.StatusServiceUnavailable, "no setup checklist here")
			return
		}
		s.writeJSON(w, http.StatusOK, s.setup.Status(r.Context()))
	}
}

// setupDismissedHandler serves PUT /api/setup/dismissed {dismissed}: hides
// the chip on every device, or brings it back.
func (s *Server) setupDismissedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.setup == nil {
			s.writeError(w, http.StatusServiceUnavailable, "no setup checklist here")
			return
		}
		var req struct {
			Dismissed bool `json:"dismissed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.setup.SetDismissed(r.Context(), req.Dismissed); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgSettingsChanged})
		s.writeJSON(w, http.StatusOK, s.setup.Status(r.Context()))
	}
}
