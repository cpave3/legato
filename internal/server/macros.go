package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/service"
)

// MacroSource gives the macros, read at each use.
type MacroSource interface {
	List(ctx context.Context) ([]service.MacroEntry, error)
}

// SetMacros sets the list of macros to serve from /api/macros when no
// macro service is wired.
func (s *Server) SetMacros(m []macros.Macro) {
	s.macros = m
}

// SetMacroService serves /api/macros from the macro service: built-ins,
// config.yaml's and the database's, read at each request.
func (s *Server) SetMacroService(m MacroSource) {
	s.macroSvc = m
}

func (s *Server) macrosHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if s.macroSvc != nil {
			list, err := s.macroSvc.List(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if list == nil {
				list = []service.MacroEntry{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"macros": list})
			return
		}
		_ = json.NewEncoder(w).Encode(macros.ListResult{Macros: s.macros})
	}
}
