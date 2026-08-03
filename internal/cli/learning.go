package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// LearningCLIService is deliberately limited to the stable learning operations
// used directly by this package. Retrieval capabilities are kept in separate
// interfaces so that the CLI does not couple composition to the whole service.
type LearningCLIService interface {
	Record(context.Context, service.LearningRecordInput) (*service.LearningView, error)
	Show(context.Context, string) (*service.LearningView, error)
}

type LearningListService interface {
	List(context.Context, service.LearningFilter) ([]store.Learning, error)
}

type LearningSearchService interface {
	SearchKnowledge(context.Context, string, service.LearningSearchFilter) (*service.LearningSearchResult, error)
}

type LearningContextService interface {
	ContextForTask(context.Context, string, int) ([]store.Learning, error)
}

type LearningAcceptService interface {
	Accept(context.Context, string, int, string, string, string) (*service.LearningView, error)
}

type LearningEditService interface {
	Edit(context.Context, string, int, service.LearningEditInput) (*service.LearningView, error)
}

type LearningPromptActivationService interface {
	SetPromptEnabled(context.Context, string, int, bool) (*service.LearningView, error)
}

func LearningSetPromptEnabled(ctx context.Context, svc LearningPromptActivationService, id string, revision int, enabled bool, w io.Writer) error {
	view, err := svc.SetPromptEnabled(ctx, id, revision, enabled)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(view)
}

type LearningRejectService interface {
	Reject(context.Context, string, int) (*service.LearningView, error)
}

type LearningIncorporationService interface {
	ProposeIncorporation(context.Context, string, int, string, string, string) (*store.LearningIncorporation, error)
}

type LearningIncorporationSubmitService interface {
	SubmitIncorporation(context.Context, string, int, []string, string, string) (*store.LearningIncorporation, error)
}

type LearningTargetVerificationService interface {
	VerifyIncorporationTarget(context.Context, string, int, string, bool) (*store.LearningIncorporation, error)
}

type LearningDriftService interface {
	CheckIncorporationDrift(context.Context, string) ([]service.IncorporationDriftResult, error)
}

type ReflectionCLIService interface {
	SubmitReflectionJSON(context.Context, string, []byte) (*service.ReflectionSubmission, error)
}

type WorkflowCompletionService interface {
	Complete(context.Context, string, string, string, map[string]string, map[string]string) (*store.WorkflowRunView, error)
}

func LearningRecord(ctx context.Context, svc LearningCLIService, input service.LearningRecordInput, w io.Writer) error {
	view, err := svc.Record(ctx, input)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(view)
}

func LearningShow(ctx context.Context, svc LearningCLIService, id string, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("learning ID is required")
	}
	view, err := svc.Show(ctx, id)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(view)
}

func LearningList(ctx context.Context, svc LearningListService, scope, status, projectKey, category string, tags []string, limit int, w io.Writer) error {
	items, err := svc.List(ctx, service.LearningFilter{Scope: scope, Status: status, ProjectKey: projectKey, Category: category, Tags: tags, Limit: limit})
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(items)
}

func LearningSearch(ctx context.Context, svc LearningSearchService, query string, filter service.LearningSearchFilter, w io.Writer) error {
	if query == "" {
		return fmt.Errorf("search query is required")
	}
	items, err := svc.SearchKnowledge(ctx, query, filter)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(items)
}

func LearningAccept(ctx context.Context, svc LearningAcceptService, id string, revision int, claim, scope, project string, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("learning ID is required")
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be a positive integer")
	}
	view, err := svc.Accept(ctx, id, revision, claim, scope, project)
	if err != nil {
		return fmt.Errorf("accept learning %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(view)
}

func LearningEdit(ctx context.Context, svc LearningEditService, id string, revision int, input service.LearningEditInput, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("learning ID is required")
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be a positive integer")
	}
	view, err := svc.Edit(ctx, id, revision, input)
	if err != nil {
		return fmt.Errorf("edit learning %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(view)
}

func LearningReject(ctx context.Context, svc LearningRejectService, id string, revision int, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("learning ID is required")
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be a positive integer")
	}
	view, err := svc.Reject(ctx, id, revision)
	if err != nil {
		return fmt.Errorf("reject learning %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(view)
}

func LearningIncorporate(ctx context.Context, svc LearningIncorporationService, id string, revision int, destination, target, action string, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("learning ID is required")
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be a positive integer")
	}
	inc, err := svc.ProposeIncorporation(ctx, id, revision, destination, target, action)
	if err != nil {
		return fmt.Errorf("propose incorporation for learning %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(inc)
}

func LearningIncorporationSubmit(ctx context.Context, svc LearningIncorporationSubmitService, id string, revision int, document []byte, w io.Writer) error {
	if id == "" {
		return fmt.Errorf("incorporation ID is required")
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be a positive integer")
	}
	var input struct {
		References       []string        `json:"references"`
		Verification     json.RawMessage `json:"verification"`
		VerificationKind string          `json:"verification_kind"`
		Result           string          `json:"result"`
	}
	if len(document) == 0 {
		return fmt.Errorf("incorporation submission JSON is required (use --file <path|->)")
	}
	if err := json.Unmarshal(document, &input); err != nil {
		return fmt.Errorf("parse incorporation submission JSON: %w", err)
	}
	kind := input.VerificationKind
	if len(input.Verification) > 0 {
		var text string
		if err := json.Unmarshal(input.Verification, &text); err == nil {
			if kind == "" {
				kind = text
			}
		} else {
			var verification struct {
				Kind   string `json:"kind"`
				Result string `json:"result"`
			}
			if err := json.Unmarshal(input.Verification, &verification); err != nil {
				return fmt.Errorf("verification must be a string or object: %w", err)
			}
			if kind == "" {
				kind = verification.Kind
			}
			if input.Result == "" {
				input.Result = verification.Result
			}
		}
	}
	inc, err := svc.SubmitIncorporation(ctx, id, revision, input.References, kind, input.Result)
	if err != nil {
		return fmt.Errorf("submit incorporation %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(inc)
}

func LearningVerifyTarget(ctx context.Context, svc LearningTargetVerificationService, id string, revision int, repositoryRoot string, confirmed bool, w io.Writer) error {
	if id == "" || revision <= 0 {
		return fmt.Errorf("incorporation ID and positive revision are required")
	}
	inc, err := svc.VerifyIncorporationTarget(ctx, id, revision, repositoryRoot, confirmed)
	if err != nil {
		return fmt.Errorf("verify incorporation target %q: %w", id, err)
	}
	return json.NewEncoder(w).Encode(inc)
}

func LearningDrift(ctx context.Context, svc LearningDriftService, repositoryRoot string, w io.Writer) error {
	results, err := svc.CheckIncorporationDrift(ctx, repositoryRoot)
	if err != nil {
		return fmt.Errorf("check incorporation drift: %w", err)
	}
	return json.NewEncoder(w).Encode(results)
}

func LearningContext(ctx context.Context, svc LearningContextService, taskID string, limit int, w io.Writer) error {
	if taskID == "" {
		return fmt.Errorf("task ID is required")
	}
	result, err := svc.ContextForTask(ctx, taskID, limit)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(result)
}

// LearningReflectionSubmit persists the callback before advancing the exact
// workflow attempt recorded by the reflection request. A validation/persistence
// error therefore cannot accidentally complete the waiting step.
func LearningReflectionSubmit(ctx context.Context, learn ReflectionCLIService, workflows WorkflowCompletionService, requestID string, document []byte, w io.Writer) error {
	if requestID == "" {
		return fmt.Errorf("reflection request ID is required")
	}
	submission, err := learn.SubmitReflectionJSON(ctx, requestID, document)
	if err != nil {
		return err
	}
	if submission == nil || submission.Request == nil {
		return fmt.Errorf("reflection submission returned no request")
	}
	request := submission.Request
	values := map[string]string{
		"available":  "true",
		"request_id": request.ID,
	}
	view, err := workflows.Complete(ctx, request.WorkflowRunID, request.StepID, request.AttemptID, values, nil)
	if err != nil {
		return fmt.Errorf("complete reflection workflow: %w", err)
	}
	return json.NewEncoder(w).Encode(struct {
		Submission *service.ReflectionSubmission `json:"submission"`
		Workflow   *store.WorkflowRunView        `json:"workflow"`
	}{Submission: submission, Workflow: view})
}
