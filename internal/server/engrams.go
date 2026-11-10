package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// EngramService says whether engrams are offered and which exist
// (service.EngramService).
type EngramService interface {
	Status(ctx context.Context) service.EngramStatus
}

// SetEngramService lets the spawn dialog offer engrams.
func (s *Server) SetEngramService(engrams EngramService) { s.engrams = engrams }

// engramsHandler serves GET /api/engrams: whether engrams are offered now
// and the engrams engram lists. engram is probed on each request.
func (s *Server) engramsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.engrams == nil {
			s.writeJSON(w, http.StatusOK, service.EngramStatus{Mode: "off", Engrams: []service.EngramIdentity{}})
			return
		}
		s.writeJSON(w, http.StatusOK, s.engrams.Status(r.Context()))
	}
}
