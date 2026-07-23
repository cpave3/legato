package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ReviewService is the subset of review operations exposed over HTTP.
type ReviewService interface {
	Queue(context.Context) ([]service.ReviewQueueItem, error)
	Tour(ctx context.Context, tourID string) (*service.ReviewTourView, error)
	StepDiff(ctx context.Context, tourID, stepID string) ([]gitpkg.FileDiff, error)
	SetReviewed(ctx context.Context, tourID, stepID string, reviewed bool) error
	AskQuestion(ctx context.Context, tourID, stepID string, question service.ReviewQuestion) error
	CreateFinding(ctx context.Context, tourID string, input service.ReviewFindingInput) (*store.ReviewFinding, error)
	RequestPlan(ctx context.Context, tourID string, findingIDs []string) (*store.ReviewPlanRequest, error)
	RestartWithFeedback(ctx context.Context, tourID, feedback string) error
	Complete(ctx context.Context, tourID string) error
	Delete(ctx context.Context, tourID string) error
}

// SetReviewService sets the optional service used by review endpoints.
func (s *Server) SetReviewService(svc ReviewService) {
	s.reviews = svc
}

func (s *Server) reviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		if r.Method == http.MethodDelete {
			if err := s.reviews.Delete(r.Context(), r.PathValue("tour_id")); err != nil {
				s.writeReviewError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		tour, err := s.reviews.Tour(r.Context(), r.PathValue("tour_id"))
		if err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, tour)
	}
}

func (s *Server) reviewStepDiffHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		files, err := s.reviews.StepDiff(r.Context(), r.PathValue("tour_id"), r.PathValue("step_id"))
		if err != nil {
			s.writeReviewError(w, err)
			return
		}
		if files == nil {
			files = []gitpkg.FileDiff{}
		}
		s.writeJSON(w, http.StatusOK, files)
	}
}

func (s *Server) reviewStepReviewedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPatch {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		var req struct {
			Reviewed *bool `json:"reviewed"`
		}
		if err := decodeJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Reviewed == nil {
			s.writeError(w, http.StatusBadRequest, "reviewed is required")
			return
		}
		if err := s.reviews.SetReviewed(r.Context(), r.PathValue("tour_id"), r.PathValue("step_id"), *req.Reviewed); err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) reviewQuestionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		var req struct {
			Text      string                       `json:"text"`
			Selection *service.ReviewLineSelection `json:"selection"`
		}
		if err := decodeJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Text = strings.TrimSpace(req.Text)
		if req.Text == "" {
			s.writeError(w, http.StatusBadRequest, "text is required")
			return
		}
		err := s.reviews.AskQuestion(r.Context(), r.PathValue("tour_id"), r.PathValue("step_id"), service.ReviewQuestion{
			Text: req.Text, Selection: req.Selection,
		})
		if errors.Is(err, service.ErrInvalidLineSelection) {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, service.ErrAgentOffline) {
			s.writeJSON(w, http.StatusAccepted, map[string]string{
				"status": "saved", "warning": err.Error(),
			})
			return
		}
		if err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) reviewFindingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		var req struct {
			Body      string                      `json:"body"`
			Selection *service.ReviewFindingInput `json:"selection"`
		}
		if err := decodeJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		input := service.ReviewFindingInput{Body: req.Body}
		if req.Selection != nil {
			input.StepID = req.Selection.StepID
			input.FilePath = req.Selection.FilePath
			input.HunkAnchor = req.Selection.HunkAnchor
			input.LineStart = req.Selection.LineStart
			input.LineEnd = req.Selection.LineEnd
		}
		finding, err := s.reviews.CreateFinding(r.Context(), r.PathValue("tour_id"), input)
		if err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, finding)
	}
}

func (s *Server) reviewRequestPlanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		var req struct {
			FindingIDs []string `json:"finding_ids"`
		}
		if err := decodeJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		request, err := s.reviews.RequestPlan(r.Context(), r.PathValue("tour_id"), req.FindingIDs)
		if errors.Is(err, service.ErrAgentOffline) {
			s.writeJSON(w, http.StatusAccepted, map[string]any{"request": request, "warning": err.Error()})
			return
		}
		if err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, map[string]any{"request": request})
	}
}

func (s *Server) reviewRegenerateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		var req struct {
			Feedback string `json:"feedback"`
		}
		if err := decodeJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Feedback = strings.TrimSpace(req.Feedback)
		if req.Feedback == "" {
			s.writeError(w, http.StatusBadRequest, "feedback is required")
			return
		}
		if err := s.reviews.RestartWithFeedback(r.Context(), r.PathValue("tour_id"), req.Feedback); err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) reviewCompleteHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireReviewService(w) {
			return
		}
		if err := s.reviews.Complete(r.Context(), r.PathValue("tour_id")); err != nil {
			s.writeReviewError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func decodeJSON(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON")
	}
	var trailing interface{}
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON")
	}
	return nil
}

func (s *Server) requireReviewService(w http.ResponseWriter) bool {
	if s.reviews != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "review service not available")
	return false
}

func (s *Server) writeReviewError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "review not found")
		return
	}
	s.writeError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) reviewQueueHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.reviews == nil {
			s.writeError(w, http.StatusServiceUnavailable, "review service not available")
			return
		}
		items, err := s.reviews.Queue(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if items == nil {
			items = []service.ReviewQueueItem{}
		}
		s.writeJSON(w, http.StatusOK, items)
	}
}
