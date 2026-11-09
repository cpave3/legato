package server

import "net/http"

// SetRestarter gives the server the restart of this Legato: every Legato
// process of its data folder shuts down and starts again in place
// (docs/ai/updates.md § Restarting).
func (s *Server) SetRestarter(restart func()) { s.restart = restart }

// restartHandler serves POST /api/restart. It answers 202 before anything
// stops.
func (s *Server) restartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.restart == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this Legato cannot restart itself")
			return
		}
		s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
		go s.restart()
	}
}
