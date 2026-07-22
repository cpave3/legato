package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type PlanService interface {
	Queue(context.Context) ([]service.PlanQueueItem, error)
	Plan(context.Context, string) (*service.PlanView, error)
	Revisions(context.Context, string) ([]store.PlanRevision, error)
	Respond(context.Context, string, string, service.PlanResponseInput) error
	AddComment(context.Context, string, service.PlanCommentInput) (*store.PlanComment, error)
	UpdateComment(context.Context, string, string, string) (*store.PlanComment, error)
	AskQuestion(context.Context, string, string) (string, error)
	RequestChanges(context.Context, string) error
	Approve(context.Context, string) error
	Reject(context.Context, string) error
	Reopen(context.Context, string) error
}

func (s *Server) SetPlanService(svc PlanService) { s.plans = svc }

func (s *Server) requirePlanService(w http.ResponseWriter) bool {
	if s.plans != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "plan service not available")
	return false
}

func (s *Server) writePlanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "plan not found")
	case errors.Is(err, service.ErrPlanQuestionsUnanswered):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) planQueueHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		items, err := s.plans.Queue(r.Context())
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) planHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		view, err := s.plans.Plan(r.Context(), r.PathValue("plan_id"))
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) planRevisionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		revisions, err := s.plans.Revisions(r.Context(), r.PathValue("plan_id"))
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, revisions)
	}
}

func (s *Server) planResponseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		var input service.PlanResponseInput
		if err := decodeJSON(r, &input); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.plans.Respond(r.Context(), r.PathValue("plan_id"), r.PathValue("question_key"), input); err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) planCommentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		var input service.PlanCommentInput
		if err := decodeJSON(r, &input); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := s.plans.AddComment(r.Context(), r.PathValue("plan_id"), input)
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, comment)
	}
}

func (s *Server) planCommentUpdateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		var input struct {
			Body string `json:"body"`
		}
		if err := decodeJSON(r, &input); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := s.plans.UpdateComment(r.Context(), r.PathValue("plan_id"), r.PathValue("comment_id"), input.Body)
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, comment)
	}
}

func (s *Server) planQuestionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		var input struct {
			Text string `json:"text"`
		}
		if err := decodeJSON(r, &input); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		input.Text = strings.TrimSpace(input.Text)
		threadID, err := s.plans.AskQuestion(r.Context(), r.PathValue("plan_id"), input.Text)
		if errors.Is(err, service.ErrAgentOffline) {
			s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "saved", "thread_id": threadID, "warning": err.Error()})
			return
		}
		if err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "thread_id": threadID})
	}
}

func (s *Server) planRequestChangesHandler() http.HandlerFunc {
	return s.planActionHandler("request changes", s.plansRequestChanges)
}
func (s *Server) planApproveHandler() http.HandlerFunc {
	return s.planActionHandler("approve", s.plansApprove)
}
func (s *Server) planRejectHandler() http.HandlerFunc {
	return s.planActionHandler("reject", s.plansReject)
}
func (s *Server) planReopenHandler() http.HandlerFunc {
	return s.planActionHandler("reopen", s.plansReopen)
}

func (s *Server) planActionHandler(_ string, action func(context.Context, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requirePlanService(w) {
			return
		}
		if err := action(r.Context(), r.PathValue("plan_id")); err != nil {
			s.writePlanError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) plansRequestChanges(ctx context.Context, id string) error {
	return s.plans.RequestChanges(ctx, id)
}
func (s *Server) plansApprove(ctx context.Context, id string) error { return s.plans.Approve(ctx, id) }
func (s *Server) plansReject(ctx context.Context, id string) error  { return s.plans.Reject(ctx, id) }
func (s *Server) plansReopen(ctx context.Context, id string) error  { return s.plans.Reopen(ctx, id) }
