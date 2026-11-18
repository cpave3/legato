package service

import (
	"context"
	"encoding/json"

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
