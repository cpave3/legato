package server

import (
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// SetReviewEngine tells the server whether some Legato on the data folder
// runs the review engine, asked at each GET /api/review-engine.
func (s *Server) SetReviewEngine(status func() service.ReviewEngineStatus) { s.reviewEngine = status }

// reviewEngineHandler serves GET /api/review-engine {running, reason}: what
// a card's menu reads as it opens, offering review passes only while some
// Legato on the data folder runs the engine (spec LEG-559 D9). A server not
// told of the engine says none runs.
func (s *Server) reviewEngineHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status := service.ReviewEngineStatus{Reason: service.ReviewEngineStoppedReason}
		if s.reviewEngine != nil {
			status = s.reviewEngine()
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}
