package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func (s *Server) learningAvailable(w http.ResponseWriter) bool {
	if s.learning == nil {
		s.writeError(w, http.StatusServiceUnavailable, "learning service not available")
		return false
	}
	return true
}

func learningLimit(r *http.Request) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return 0, nil
	}
	return strconv.Atoi(r.URL.Query().Get("limit"))
}

func learningFilter(r *http.Request) (service.LearningFilter, error) {
	limit, err := learningLimit(r)
	return service.LearningFilter{Status: r.URL.Query().Get("status"), Scope: r.URL.Query().Get("scope"), ProjectKey: r.URL.Query().Get("project_key"), Limit: limit}, err
}

func (s *Server) learningError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrLearningConflict), errors.Is(err, service.ErrReflectionStale):
		s.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case service.IsLearningValidation(err):
		s.writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func learningInputError(message string) error {
	return service.LearningValidationError("%s", message)
}

func decodeLearningJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return service.LearningValidationError("invalid JSON: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return service.LearningValidationError("multiple JSON values")
		}
		return service.LearningValidationError("invalid JSON: %v", err)
	}
	return nil
}

func (s *Server) learningsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			filter, err := learningFilter(r)
			if err != nil {
				s.learningError(w, learningInputError("limit must be an integer"))
				return
			}
			items, err := s.learning.List(r.Context(), filter)
			if err != nil {
				s.learningError(w, err)
				return
			}
			s.writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			var input service.LearningRecordInput
			if err := decodeLearningJSON(r, &input); err != nil {
				s.learningError(w, err)
				return
			}
			view, err := s.learning.Record(r.Context(), input)
			if err != nil {
				s.learningError(w, err)
				return
			}
			s.writeJSON(w, http.StatusCreated, view)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func (s *Server) learningSearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, 405, "method not allowed")
			return
		}
		filter, err := learningFilter(r)
		if err != nil {
			s.learningError(w, learningInputError("limit must be an integer"))
			return
		}
		query := r.URL.Query().Get("q")
		if query == "" {
			query = r.URL.Query().Get("query")
		}
		items, err := s.learning.Search(r.Context(), query, filter)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) learningContextHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, 405, "method not allowed")
			return
		}
		limit, err := learningLimit(r)
		if err != nil {
			s.learningError(w, learningInputError("limit must be an integer"))
			return
		}
		var items []store.Learning
		if taskID := strings.TrimSpace(r.URL.Query().Get("task_id")); taskID != "" {
			items, err = s.learning.ContextForTask(r.Context(), taskID, limit)
		} else {
			items, err = s.learning.Context(r.Context(), r.URL.Query().Get("project_key"), limit)
		}
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) learningPromptPreviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
		if taskID == "" {
			s.learningError(w, learningInputError("task_id is required"))
			return
		}
		preview, err := s.learning.PreviewLearningPromptForTask(r.Context(), taskID, 20, 16*1024)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, preview)
	}
}

func (s *Server) learningShowHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, 405, "method not allowed")
			return
		}
		view, err := s.learning.Show(r.Context(), r.PathValue("id"))
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

type learningDecisionRequest struct {
	Revision   int    `json:"revision"`
	Claim      string `json:"claim,omitempty"`
	Scope      string `json:"scope,omitempty"`
	ProjectKey string `json:"project_key,omitempty"`
}

func (s *Server) learningDecisionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost && !(action == "edit" && r.Method == http.MethodPatch) {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body learningDecisionRequest
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 {
			s.learningError(w, learningInputError("revision must be a positive integer"))
			return
		}
		var view *service.LearningView
		var err error
		if action == "reject" {
			view, err = s.learning.Reject(r.Context(), r.PathValue("id"), body.Revision)
		} else {
			if action == "edit" && body.Claim == "" && body.Scope == "" && body.ProjectKey == "" {
				s.learningError(w, learningInputError("edit requires claim, scope, or project_key"))
				return
			}
			view, err = s.learning.Accept(r.Context(), r.PathValue("id"), body.Revision, body.Claim, body.Scope, body.ProjectKey)
		}
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) learningPromptActivationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Revision int  `json:"revision"`
			Enabled  bool `json:"enabled"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 {
			s.learningError(w, learningInputError("revision must be a positive integer"))
			return
		}
		view, err := s.learning.SetPromptEnabled(r.Context(), r.PathValue("id"), body.Revision, body.Enabled)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) learningIncorporationProposeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body struct {
			Revision             int    `json:"revision"`
			Destination          string `json:"destination"`
			Target               string `json:"target,omitempty"`
			Action               string `json:"action,omitempty"`
			ImplementationTaskID string `json:"implementation_task_id,omitempty"`
			WorkflowRunID        string `json:"workflow_run_id,omitempty"`
			ReviewTourID         string `json:"review_tour_id,omitempty"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 {
			s.learningError(w, learningInputError("revision must be a positive integer"))
			return
		}
		inc, err := s.learning.ProposeIncorporationLinked(r.Context(), r.PathValue("id"), body.Revision, body.Destination, body.Target, body.Action, body.ImplementationTaskID, body.WorkflowRunID, body.ReviewTourID)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusCreated, inc)
	}
}

func (s *Server) learningIncorporationSubmitHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body struct {
			Revision         int      `json:"revision"`
			References       []string `json:"references"`
			VerificationKind string   `json:"verification_kind"`
			Result           string   `json:"result,omitempty"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 {
			s.learningError(w, learningInputError("revision must be a positive integer"))
			return
		}
		inc, err := s.learning.SubmitIncorporation(r.Context(), r.PathValue("id"), body.Revision, body.References, body.VerificationKind, body.Result)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, inc)
	}
}

func (s *Server) learningIncorporationVerifyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body struct {
			Revision int    `json:"revision"`
			Passed   *bool  `json:"passed"`
			Result   string `json:"result,omitempty"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 || body.Passed == nil {
			s.learningError(w, learningInputError("positive revision and passed are required"))
			return
		}
		inc, err := s.learning.VerifyIncorporation(r.Context(), r.PathValue("id"), body.Revision, *body.Passed, body.Result)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, inc)
	}
}

func (s *Server) learningIncorporationVerifyTargetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body struct {
			Revision       int    `json:"revision"`
			RepositoryRoot string `json:"repository_root"`
			Confirmed      bool   `json:"confirmed,omitempty"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		if body.Revision <= 0 || strings.TrimSpace(body.RepositoryRoot) == "" {
			s.learningError(w, learningInputError("positive revision and repository_root are required"))
			return
		}
		inc, err := s.learning.VerifyIncorporationTarget(r.Context(), r.PathValue("id"), body.Revision, body.RepositoryRoot, body.Confirmed)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, inc)
	}
}

func (s *Server) learningIncorporationDriftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		var body struct {
			RepositoryRoot string `json:"repository_root"`
		}
		if err := decodeLearningJSON(r, &body); err != nil {
			s.learningError(w, err)
			return
		}
		results, err := s.learning.CheckIncorporationDrift(r.Context(), body.RepositoryRoot)
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, results)
	}
}

func (s *Server) learningReflectionSubmitHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, service.MaxReflectionDocumentBytes+1))
		if err != nil {
			s.learningError(w, err)
			return
		}
		submission, err := s.learning.SubmitReflectionJSON(r.Context(), r.PathValue("id"), data)
		if err != nil {
			s.learningError(w, err)
			return
		}
		if submission == nil || submission.Request == nil {
			s.learningError(w, errors.New("reflection submission returned no request"))
			return
		}
		if s.reflectionWorkflows == nil {
			s.writeError(w, http.StatusServiceUnavailable, "reflection persisted; workflow completion is temporarily unavailable")
			return
		}
		request := submission.Request
		values := map[string]string{"available": "true", "request_id": request.ID}
		workflow, err := s.reflectionWorkflows.Complete(r.Context(), request.WorkflowRunID, request.StepID, request.AttemptID, values, nil)
		if err != nil {
			// Persistence has succeeded; an exact replay safely retries completion.
			s.writeError(w, http.StatusServiceUnavailable, "reflection persisted; workflow completion failed and may be retried")
			return
		}
		s.writeJSON(w, http.StatusOK, struct {
			Submission *service.ReflectionSubmission `json:"submission"`
			Workflow   *store.WorkflowRunView        `json:"workflow"`
		}{Submission: submission, Workflow: workflow})
	}
}

func (s *Server) learningPromptManifestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.learningAvailable(w) {
			return
		}
		if r.Method != http.MethodGet {
			s.writeError(w, 405, "method not allowed")
			return
		}
		manifest, err := s.learning.ShowPromptManifest(r.Context(), r.PathValue("id"))
		if err != nil {
			s.learningError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, manifest)
	}
}
