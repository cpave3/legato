package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Workflow control (spec LEG-482 §1, §6; LEG-486): what a step's failure
// does (on_failure: fail, continue or pause), the gate a paused step waits
// at (Retry, Skip, Stop), and the gate step, which asks the operator to
// choose. if: is checked in advance.

// WorkflowStepError is an executor's error that carries a code and a link
// beside its text, such as review_loop_active and the run of the loop that
// holds the pull request. A step's failure keeps both: with on_failure:
// continue the step's outputs are status, error and error_code; a paused
// step's needs-you item and the run page show the link.
type WorkflowStepError struct {
	Code    string
	Link    string
	Message string
	Err     error
	// Status is the step's status output when it is not failed, and
	// Outputs are more outputs its failure has: a workflow.run step whose
	// child stopped says stopped, with the child's run_id (LEG-489).
	Status  string
	Outputs map[string]any
}

func (e *WorkflowStepError) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Err != nil:
		return e.Err.Error()
	}
	return e.Code
}

func (e *WorkflowStepError) Unwrap() error { return e.Err }

// stepFailure is an error as a step's failure: its text, and its code and
// link when it carries them.
func stepFailure(err error) store.StepFailure {
	failure := store.StepFailure{Message: err.Error()}
	var typed *WorkflowStepError
	if errors.As(err, &typed) {
		failure.Code, failure.Link, failure.Status, failure.Outputs = typed.Code, typed.Link, typed.Status, typed.Outputs
	}
	return failure
}

// WorkflowStepPaused is a step paused after a failure (on_failure: pause):
// its run waits for Retry, Skip or Stop.
const WorkflowStepPaused = store.WorkflowStepPaused

// The actions a paused step takes (StepAction).
const (
	PausedRetry = "retry"
	PausedSkip  = "skip"
	PausedStop  = "stop"
)

// failRecorded fails a step that is not being advanced in this call (an
// agent's step, a step waiting on its service), and follows its
// on_failure: a continued step's run goes on here, in a host, or in a host
// later (it is left due an advance); a paused one waits.
func (s *WorkflowService) failRecorded(ctx context.Context, runID, path, attemptID string, failure error) error {
	mode, err := s.store.FailWorkflowStepWith(ctx, runID, path, attemptID, stepFailure(failure))
	if err != nil {
		return err
	}
	if mode == store.FailurePaused {
		s.changed(ctx, runID, path, WorkflowChangeStepPaused)
		return nil
	}
	s.changed(ctx, runID, path, WorkflowChangeStepFailed)
	if mode == store.FailureContinued && !s.recordOnly {
		if _, err := s.advanceRecorded(ctx, runID, path); err != nil {
			return err
		}
	}
	return nil
}

// pausedReason says why a paused step needs the operator, with the link its
// error carries.
func pausedReason(step store.WorkflowStep) string {
	reason := "Step failed: " + step.Title
	if step.Title == "" {
		reason = "Step failed: " + step.StepID
	}
	if step.Error != "" {
		reason += ": " + step.Error
	}
	if step.ErrorLink != "" {
		reason += " (" + step.ErrorLink + ")"
	}
	return reason
}

// pausedStepAction takes Retry, Skip or Stop on a paused step.
func (s *WorkflowService) pausedStepAction(ctx context.Context, view *store.WorkflowRunView, stepID, action string) (*store.WorkflowRunView, error) {
	switch action {
	case PausedRetry:
		return s.Retry(ctx, view.Run.ID, stepID, false)
	case PausedSkip:
		return s.SkipStep(ctx, view.Run.ID, stepID)
	case PausedStop:
		return s.Stop(ctx, view.Run.ID, "stopped at the failure of step "+stepID)
	}
	return nil, fmt.Errorf("a paused step takes retry, skip or stop, not %q: %w", action, store.ErrWorkflowStateConflict)
}

// SkipStep skips a step paused after a failure: its outputs are missing,
// and the run goes on with the next step.
func (s *WorkflowService) SkipStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if view.Run.Status != "waiting" || view.Run.CurrentStepID != stepID {
		return nil, store.ErrWorkflowStateConflict
	}
	next := ""
	if index+1 < len(definition.Steps) {
		next = definition.Steps[index+1].ID
	}
	if err := s.store.SkipWorkflowStep(ctx, runID, stepID, next); err != nil {
		return nil, err
	}
	s.changed(ctx, runID, stepID, WorkflowChangeStepSkipped)
	if next != "" {
		if err := s.advance(ctx, definition, inputs, runID, view.Run.TaskID, index+1, prior, "", false); err != nil {
			return nil, &StepRecordedError{StepID: stepID, NextStepID: next, Err: err}
		}
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

// GateChoiceAction is the step action (and the needs-you item's action)
// that chooses a gate's option.
func GateChoiceAction(option string) string { return gateChoicePrefix + option }

const gateChoicePrefix = "choose:"

// gateBodyLimit is how much of a gate's body_path is read.
const gateBodyLimit = 64 << 10

// GateDetails is what a gate step shows (the run view's Details): what it
// asks, its details, its options and, once chosen, the choice.
type GateDetails struct {
	Title   string   `json:"title"`
	Body    string   `json:"body,omitempty"`
	Options []string `json:"options"`
	Choice  string   `json:"choice,omitempty"`
}

// gateExecutor is the gate step: it waits on the operator's choice, which
// completes it with the choice output, or Stop, which ends the run as
// stopped.
type gateExecutor struct {
	workflows *WorkflowService
}

// Type is workflowengine.GateStepType, spelt out for the catalog test.
func (g *gateExecutor) Type() string { return "gate" }

func (g *gateExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if _, err := gateOptions(step.With["options"]); err != nil {
		return WorkflowExecutionResult{}, err
	}
	if title, _ := step.With["title"].(string); strings.TrimSpace(title) == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("gate needs a title")
	}
	view, err := g.workflows.store.GetWorkflowRun(ctx, step.RunID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if _, err := gateBody(step.With, view.Artifacts); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

func (g *gateExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// gateOptions are a gate's options, Continue and Stop when it names none.
func gateOptions(value any) ([]string, error) {
	if value == nil {
		return append([]string(nil), workflowengine.DefaultGateOptions...), nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("gate options must be a list of names")
	}
	seen := map[string]bool{}
	options := make([]string, 0, len(list))
	for _, item := range list {
		name, ok := item.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" || seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("gate options must be different, non-empty names")
		}
		seen[strings.ToLower(name)] = true
		options = append(options, name)
	}
	if len(options) == 0 {
		return append([]string(nil), workflowengine.DefaultGateOptions...), nil
	}
	return options, nil
}

// gateBody is a gate's details: body, or the file at body_path (at most
// gateBodyLimit read). body_path must be one of the run's own file outputs,
// the copy Legato collected into its data folder, which no agent's sandbox
// sees: a path in an agent's checkout could be made a link to a file the
// sandbox hides, and the gate would show it (LEG-486 review).
func gateBody(with map[string]any, artifacts []store.WorkflowArtifact) (string, error) {
	if body, ok := with["body"].(string); ok {
		return body, nil
	}
	path, _ := with["body_path"].(string)
	if path == "" {
		return "", nil
	}
	managed := false
	for _, artifact := range artifacts {
		managed = managed || artifact.ManagedPath == path
	}
	if !managed {
		return "", fmt.Errorf("gate body_path must be an earlier step's file output, ${{ steps.<id>.outputs.<file>.path }}: %q", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("gate body_path: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, gateBodyLimit))
	if err != nil {
		return "", fmt.Errorf("gate body_path: %w", err)
	}
	return string(data), nil
}

// details reads a gate's attempt's resolved with back from its step.
func (g *gateExecutor) details(ctx context.Context, attempt store.WorkflowStepAttempt) (GateDetails, bool) {
	view, err := g.workflows.store.GetWorkflowRun(ctx, attempt.RunID)
	if err != nil {
		return GateDetails{}, false
	}
	for _, step := range view.Steps {
		if step.Path != attempt.Path {
			continue
		}
		var with map[string]any
		if json.Unmarshal([]byte(step.ResolvedInputsJSON), &with) != nil {
			return GateDetails{}, false
		}
		details := GateDetails{}
		details.Title, _ = with["title"].(string)
		details.Body, _ = gateBody(with, view.Artifacts)
		details.Options, _ = gateOptions(with["options"])
		if step.Status == "succeeded" {
			var outputs map[string]any
			if json.Unmarshal([]byte(step.OutputsJSON), &outputs) == nil {
				details.Choice, _ = outputs["choice"].(string)
			}
		}
		return details, true
	}
	return GateDetails{}, false
}

// Details is what the run page shows of the gate.
func (g *gateExecutor) Details(ctx context.Context, attempt store.WorkflowStepAttempt) any {
	if details, ok := g.details(ctx, attempt); ok {
		return details
	}
	return nil
}

// Attention is the question while the gate waits, and its options as
// actions.
func (g *gateExecutor) Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string) {
	details, ok := g.details(ctx, attempt)
	if !ok {
		return "", nil
	}
	actions := make([]string, len(details.Options))
	for i, option := range details.Options {
		actions[i] = GateChoiceAction(option)
	}
	return details.Title, actions
}

// Act takes the operator's choice: Stop ends the run as stopped; any other
// option completes the step with it as choice.
func (g *gateExecutor) Act(ctx context.Context, attempt store.WorkflowStepAttempt, action, _ string) error {
	details, ok := g.details(ctx, attempt)
	option, chose := strings.CutPrefix(action, gateChoicePrefix)
	if !ok || !chose {
		return fmt.Errorf("a gate takes a choice of its options: %w", store.ErrWorkflowStateConflict)
	}
	found := false
	for _, offered := range details.Options {
		found = found || offered == option
	}
	if !found {
		return fmt.Errorf("the gate does not offer %q: %w", option, store.ErrWorkflowStateConflict)
	}
	if strings.EqualFold(option, workflowengine.GateStop) {
		_, err := g.workflows.Stop(ctx, attempt.RunID, "stopped at the gate: "+details.Title)
		return err
	}
	_, err := g.workflows.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, map[string]any{"choice": option})
	return err
}
