package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/service"
)

// ReviewLoopLookup finds the loop that holds a pull request
// (service.ReviewLoopLookup).
type ReviewLoopLookup interface {
	ActiveReviewLoop(ctx context.Context, repo string, pr int) (*service.FoundReviewLoop, error)
}

// SetReviewLoops serves GET /api/review-loops.
func (s *Server) SetReviewLoops(lookup ReviewLoopLookup) { s.reviewLoops = lookup }

// reviewLoopsHandler serves GET /api/review-loops?repo=<owner/name>&pr=<n>:
// {loop: {...}} for the loop that holds the pull request, whichever caller
// opened it, or {loop: null} (spec LEG-559 § API contract). A launcher reads
// it to open a running loop instead of starting another. A missing or
// invalid parameter is a 400; a server not given the lookup finds none.
func (s *Server) reviewLoopsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		repo, raw := r.URL.Query().Get("repo"), r.URL.Query().Get("pr")
		pr, err := strconv.Atoi(raw)
		if err != nil {
			pr = 0
		}
		if err := service.CheckReviewLoopQuery(repo, pr); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var loop *service.FoundReviewLoop
		if s.reviewLoops != nil {
			loop, err = s.reviewLoops.ActiveReviewLoop(r.Context(), repo, pr)
			switch {
			case errors.Is(err, service.ErrBadReviewLoopQuery):
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			case err != nil:
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"loop": loop})
	}
}
