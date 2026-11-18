package service

import (
	"context"
	"encoding/json"
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
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if !hasWaitingAttempt(view, stepID, attemptID) {
		return nil, store.ErrWorkflowStateConflict
	}
	if outputs == nil {
		outputs = map[string]any{}
	}
	encoded, err := json.Marshal(outputs)
	if err != nil {
		return nil, err
	}
	next := ""
	if index+1 < len(definition.Steps) {
		next = definition.Steps[index+1].ID
	}
	if err := s.store.CompleteWorkflowStep(ctx, runID, stepID, attemptID, string(encoded), next); err != nil {
		return nil, err
	}
	prior[stepID] = outputs
	if next != "" {
		if err := s.advance(ctx, definition, inputs, runID, view.Run.TaskID, index+1, prior, "", false); err != nil {
			return nil, &StepRecordedError{StepID: stepID, NextStepID: next, Err: err}
		}
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

// FailWaiting fails a waiting step, and with it the run, with reason.
func (s *WorkflowService) FailWaiting(ctx context.Context, runID, stepID, attemptID, reason string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if !hasWaitingAttempt(view, stepID, attemptID) {
		return nil, store.ErrWorkflowStateConflict
	}
	if err := s.store.FailWorkflowStep(ctx, runID, stepID, attemptID, reason); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func hasWaitingAttempt(view *store.WorkflowRunView, stepID, attemptID string) bool {
	for _, attempt := range view.Attempts {
		if attempt.ID == attemptID && attempt.StepID == stepID && attempt.Status == WorkflowStepWaiting {
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
		if previous, ok := latest[attempt.StepID]; !ok || attempt.AttemptNumber >= previous.AttemptNumber {
			latest[attempt.StepID] = attempt
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
	// Attention is the reason, and the actions beyond another_pass and
	// end that the step offers.
	Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string)
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
		if detector, ok := s.executors[attempt.ExecutorType].(WorkflowStepAttention); ok {
			if reason, actions := detector.Attention(ctx, *attempt); reason != "" {
				attempt.NeedsAttention, attempt.AttentionReason, attempt.AttentionActions = true, reason, actions
			}
		}
	}
}

// StepAction takes an action on a step waiting on its service; text is a
// message's.
func (s *WorkflowService) StepAction(ctx context.Context, runID, stepID, action, text string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.StepID != stepID || attempt.Status != WorkflowStepWaiting {
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
