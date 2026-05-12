package server

import (
	"encoding/json"
	"net/http"

	"github.com/cpave3/legato/internal/engine/macros"
)

// SetMacros sets the list of macros to serve from /api/macros.
func (s *Server) SetMacros(m []macros.Macro) {
	s.macros = m
}

func (s *Server) macrosHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := macros.ListResult{Macros: s.macros}
		_ = json.NewEncoder(w).Encode(resp)
	}
}
