package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// WorkflowStepWaiting is the status an executor returns for a step that waits
// on its own service rather than on an agent or a person. The step and its
// attempt stay "waiting" (the run stays "running") until that service calls
// CompleteWaiting or FailWaiting. No agent session is tied to it, so an agent
// exiting does not fail it, and a restart leaves it waiting: the service finds
// its steps again with WaitingAttempts. Cancelling the run calls the
// executor's Cancel.
const WorkflowStepWaiting = "waiting"

// WaitingAttempts lists the attempts of one executor type that wait on its
// service, for that service to pick up again after a restart.
func (s *WorkflowService) WaitingAttempts(ctx context.Context, executorType string) ([]store.WorkflowStepAttempt, error) {
	return s.store.ListWaitingWorkflowAttemptsByExecutor(ctx, executorType)
}

// CompleteWaiting finishes a waiting step with its outputs, as a
// deterministic executor's would be, and runs the steps after it.
func (s *WorkflowService) CompleteWaiting(ctx context.Context, runID, stepID, attemptID string, outputs map[string]any) (*store.WorkflowRunView, error) {
	at, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if !hasWaitingAttempt(at.view, stepID, attemptID) {
		return nil, store.ErrWorkflowStateConflict
	}
	if outputs == nil {
		outputs = map[string]any{}
	}
	encoded, err := json.Marshal(outputs)
	if err != nil {
		return nil, err
	}
	next := at.next
	if err := s.store.CompleteWorkflowStep(ctx, runID, stepID, attemptID, string(encoded), next); err != nil {
		return nil, err
	}
	s.changed(ctx, runID, stepID, WorkflowChangeStepCompleted)
	if next != "" {
		if err := s.advance(ctx, runID, "", false); err != nil {
			return nil, &StepRecordedError{StepID: stepID, NextStepID: next, Err: err}
		}
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

// FailWaiting fails a waiting step with reason; the step's on_failure
// decides what becomes of the run.
func (s *WorkflowService) FailWaiting(ctx context.Context, runID, stepID, attemptID, reason string) (*store.WorkflowRunView, error) {
	return s.FailWaitingError(ctx, runID, stepID, attemptID, errors.New(reason))
}

// FailWaitingError is FailWaiting with an error that may carry a code and
// a link (WorkflowStepError).
func (s *WorkflowService) FailWaitingError(ctx context.Context, runID, stepID, attemptID string, failure error) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if !hasWaitingAttempt(view, stepID, attemptID) {
		return nil, store.ErrWorkflowStateConflict
	}
	if err := s.failRecorded(ctx, runID, stepID, attemptID, failure); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func hasWaitingAttempt(view *store.WorkflowRunView, stepID, attemptID string) bool {
	for _, attempt := range view.Attempts {
		if attempt.ID == attemptID && attempt.Path == stepID && attempt.Status == WorkflowStepWaiting {
			return true
		}
	}
	return false
}

// WorkflowStepDetailer is an executor that says more of its step than its
// status: Show puts it in the run view's Details.
type WorkflowStepDetailer interface {
	Details(ctx context.Context, attempt store.WorkflowStepAttempt) any
}

// addDetails asks each step's executor about its latest attempt.
func (s *WorkflowService) addDetails(ctx context.Context, view *store.WorkflowRunView) {
	latest := map[string]store.WorkflowStepAttempt{}
	for _, attempt := range view.Attempts {
		if previous, ok := latest[attempt.Path]; !ok || attempt.AttemptNumber >= previous.AttemptNumber {
			latest[attempt.Path] = attempt
		}
	}
	for stepID, attempt := range latest {
		detailer, ok := s.executors[attempt.ExecutorType].(WorkflowStepDetailer)
		if !ok {
			continue
		}
		if details := detailer.Details(ctx, attempt); details != nil {
			if view.Details == nil {
				view.Details = map[string]any{}
			}
			view.Details[stepID] = details
		}
	}
}

// WorkflowStepAttention is an executor whose waiting step can need the
// person: a reason, or "" when it does not.
type WorkflowStepAttention interface {
	// Attention is the reason, and the actions beyond another_pass that
	// the step offers: end for a stuck step, message.
	Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string)
}

// WorkflowStepNeed is what a step waiting on its service needs of the
// operator: a reason ("" for nothing), the Attention signal it raises, and
// the actions it offers, the first the primary.
type WorkflowStepNeed struct {
	Reason  string
	Signal  string
	Actions []string
	// Steward says the step's review loop is stewarded: End ends the
	// steward with outcome ended.
	Steward bool
}

// WorkflowStepNeeder is a WorkflowStepAttention that also names the
// signal and the primary action (a review loop's steps); waitingAttention
// asks it first.
type WorkflowStepNeeder interface {
	Need(ctx context.Context, attempt store.WorkflowStepAttempt) WorkflowStepNeed
}

// WorkflowNotice is an Attention notice an executor raises about a step
// that no longer waits, such as a steward that ended idle: one item per
// run step, which the operator dismisses or which goes by itself.
type WorkflowNotice struct {
	RunID    string
	StepID   string
	Signal   string
	Revision string
	Title    string
	Reason   string
}

// WorkflowNoticer is an executor with notices to raise.
type WorkflowNoticer interface {
	Notices(ctx context.Context) []WorkflowNotice
}

// Notices are the notices of every executor that has any.
func (s *WorkflowService) Notices(ctx context.Context) []WorkflowNotice {
	var out []WorkflowNotice
	for _, executor := range s.executors {
		if noticer, ok := executor.(WorkflowNoticer); ok {
			out = append(out, noticer.Notices(ctx)...)
		}
	}
	return out
}

// WorkflowStepActor is an executor whose waiting step takes actions, such
// as a stuck review loop's another_pass and end.
type WorkflowStepActor interface {
	Act(ctx context.Context, attempt store.WorkflowStepAttempt, action, text string) error
}

// waitingAttention marks the waiting attempts whose executor says they need
// the person.
func (s *WorkflowService) waitingAttention(ctx context.Context, view *store.WorkflowRunView) {
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.Status != WorkflowStepWaiting {
			continue
		}
		if needer, ok := s.executors[attempt.ExecutorType].(WorkflowStepNeeder); ok {
			if need := needer.Need(ctx, *attempt); need.Reason != "" {
				attempt.NeedsAttention, attempt.AttentionReason, attempt.AttentionActions = true, need.Reason, need.Actions
				attempt.AttentionSignal, attempt.AttentionSteward = need.Signal, need.Steward
			}
			continue
		}
		if detector, ok := s.executors[attempt.ExecutorType].(WorkflowStepAttention); ok {
			if reason, actions := detector.Attention(ctx, *attempt); reason != "" {
				attempt.NeedsAttention, attempt.AttentionReason, attempt.AttentionActions = true, reason, actions
			}
		}
	}
}

// StepAction takes an action on a step waiting on its service (a gate's
// choice, a review loop's another_pass); text is a message's. On a step
// paused after a failure it takes retry, skip or stop.
func (s *WorkflowService) StepAction(ctx context.Context, runID, stepID, action, text string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, step := range view.Steps {
		if step.Path == stepID && step.Status == WorkflowStepPaused {
			return s.pausedStepAction(ctx, view, stepID, action)
		}
	}
	for _, attempt := range view.Attempts {
		if attempt.Path != stepID || attempt.Status != WorkflowStepWaiting {
			continue
		}
		actor, ok := s.executors[attempt.ExecutorType].(WorkflowStepActor)
		if !ok {
			break
		}
		if err := actor.Act(ctx, attempt, action, text); err != nil {
			return nil, err
		}
		return s.Show(ctx, runID)
	}
	return nil, fmt.Errorf("workflow step %q takes no action %q now: %w", stepID, action, store.ErrWorkflowStateConflict)
}

// Executor is the registered executor of a step type, or nil.
func (s *WorkflowService) Executor(executorType string) WorkflowExecutor {
	return s.executors[executorType]
}
