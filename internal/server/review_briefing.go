package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/reviewbriefing"
	"github.com/cpave3/legato/internal/service"
)

// ReviewBriefingService reads the review briefing (.review/) on a running
// agent's branch.
type ReviewBriefingService interface {
	Briefing(ctx context.Context, taskID string) (*reviewbriefing.Index, error)
	Page(ctx context.Context, taskID, path string) (string, error)
}

// SetReviewBriefingService enables the review briefing API. resourceDomains
// are the hosts a briefing's page may load scripts, styles and fonts from,
// as for HTML an agent shows.
func (s *Server) SetReviewBriefingService(briefings ReviewBriefingService, resourceDomains []string) {
	s.reviewBriefings = briefings
	s.reviewBriefingDomains = append([]string{}, resourceDomains...)
}

type reviewBriefingDTO struct {
	Branch string                `json:"branch,omitempty"`
	Base   string                `json:"base,omitempty"`
	Pages  []reviewbriefing.Page `json:"pages"`
}

// reviewBriefingHandler lists the pages of the briefing on the agent's
// branch. An agent with no checkout, or a branch without a briefing, has
// none: the web UI asks for every agent it shows.
func (s *Server) reviewBriefingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.reviewBriefings == nil {
			s.writeJSON(w, http.StatusOK, reviewBriefingDTO{Pages: []reviewbriefing.Page{}})
			return
		}
		index, err := s.reviewBriefings.Briefing(r.Context(), r.PathValue("task_id"))
		if errors.Is(err, service.ErrNoReviewBriefing) {
			s.writeJSON(w, http.StatusOK, reviewBriefingDTO{Pages: []reviewbriefing.Page{}})
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		pages := index.Pages
		if pages == nil {
			pages = []reviewbriefing.Page{}
		}
		s.writeJSON(w, http.StatusOK, reviewBriefingDTO{Branch: index.Branch, Base: index.Base, Pages: pages})
	}
}

// reviewBriefingPageHandler reads one page, with the files it uses put in.
// It is always inside JSON, so the page is never served from Legato's own
// address: the web UI draws it in a sandboxed frame.
func (s *Server) reviewBriefingPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.reviewBriefings == nil {
			s.writeError(w, http.StatusServiceUnavailable, "review briefings are unavailable")
			return
		}
		path := r.URL.Query().Get("path")
		html, err := s.reviewBriefings.Page(r.Context(), r.PathValue("task_id"), path)
		switch {
		case errors.Is(err, service.ErrUnknownReviewPage), errors.Is(err, service.ErrNoReviewBriefing):
			s.writeError(w, http.StatusNotFound, "no such page in the agent's review briefing")
			return
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		s.writeJSON(w, http.StatusOK, map[string]any{"path": path, "html": html, "resource_domains": s.reviewBriefingDomains})
	}
}
