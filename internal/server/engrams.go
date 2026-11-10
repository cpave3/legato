package server

import (
	"context"
	"log"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// EngramService says whether engrams are offered and which exist
// (service.EngramService).
type EngramService interface {
	Status(ctx context.Context) service.EngramStatus
	Picker(ctx context.Context, req service.EngramPickerRequest) (service.EngramPicker, error)
	Remember(ctx context.Context, req service.EngramPickerRequest, name string) error
}

// SetEngramService lets the spawn dialog offer engrams.
func (s *Server) SetEngramService(engrams EngramService) { s.engrams = engrams }

// engramsHandler serves GET /api/engrams?dir=…|repo=…&task_id=…: whether
// engrams are offered now, the place, and the engrams in picker order with
// their marks and the one to pre-select. engram is probed on each request.
func (s *Server) engramsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.engrams == nil {
			s.writeJSON(w, http.StatusOK, service.EngramPicker{EngramStatus: service.EngramStatus{Mode: "off"}, Engrams: []service.EngramChoice{}})
			return
		}
		q := r.URL.Query()
		dir := q.Get("dir")
		if s.directories != nil {
			dir = s.directories.Expand(dir)
		}
		picker, err := s.engrams.Picker(r.Context(), service.EngramPickerRequest{Dir: dir, Repo: q.Get("repo"), TaskID: q.Get("task_id")})
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, picker)
	}
}

// rememberEngram is the spawn dialog's Remember: it binds the engram picked
// to where the agent starts and makes it the default there, or with none
// picked clears the default. It never stops the spawn: one it cannot do (a
// job clone named only by its ID) is logged.
func (s *Server) rememberEngram(ctx context.Context, remember bool, dir, repo, name string) {
	if !remember || s.engrams == nil {
		return
	}
	if err := s.engrams.Remember(ctx, service.EngramPickerRequest{Dir: dir, Repo: repo}, name); err != nil {
		log.Printf("engram: remembering %q for %s%s: %v", name, dir, repo, err)
	}
}
