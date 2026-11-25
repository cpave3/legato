package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// A suspended step (LEG-625) is an agent.run step whose agent Legato
// stopped with: the agent is interrupted, and resuming it continues the
// step on the same attempt. The agent service suspends and resumes it;
// the run page reaches it here.

// workflowRetriedReason ends a suspended attempt the operator retries.
const workflowRetriedReason = "retried by the operator"

// SetInterruptedAgents lets the run page resume the interrupted agent of a
// suspended step, and Retry and Cancel dismiss it.
func (s *WorkflowService) SetInterruptedAgents(agents InterruptedAgents) { s.interrupted = agents }

// addResumable marks the attempts Resume continues: those an interrupted
// agent that can be resumed works on.
func (s *WorkflowService) addResumable(ctx context.Context, view *store.WorkflowRunView) {
	if s.interrupted == nil {
		return
	}
	open := false
	for _, attempt := range view.Attempts {
		if attempt.Status == store.WorkflowSuspended || attempt.Status == "failed" {
			open = true
		}
	}
	if !open {
		return
	}
	list, err := s.interrupted.ListInterruptedAgents(ctx)
	if err != nil {
		return
	}
	for _, agent := range list {
		if agent.Workflow == nil || agent.Workflow.RunID != view.Run.ID || !agent.Resumable {
			continue
		}
		for i := range view.Attempts {
			attempt := &view.Attempts[i]
			if attempt.Path == agent.Workflow.StepID && (attempt.Status == store.WorkflowSuspended || attempt.Status == "failed") && s.attemptAgentTask(ctx, *attempt) == agent.TaskID {
				attempt.Resumable = true
			}
		}
	}
}

// attemptAgentTask is the task of an attempt's agent session, "" for none.
func (s *WorkflowService) attemptAgentTask(ctx context.Context, attempt store.WorkflowStepAttempt) string {
	if attempt.AgentSessionID == nil {
		return ""
	}
	session, err := s.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil {
		return ""
	}
	return session.TaskID
}

// ResumeStep resumes the interrupted agent of a suspended step (or one an
// earlier Legato failed when it was interrupted), which goes on with the
// same attempt.
func (s *WorkflowService) ResumeStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	if s.interrupted == nil {
		return nil, fmt.Errorf("resuming workflow agents is not available here")
	}
	view, err := s.Show(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.Path != stepID || !attempt.Resumable {
			continue
		}
		if err := s.interrupted.ResumeInterruptedAgent(ctx, s.attemptAgentTask(ctx, attempt), false); err != nil {
			return nil, err
		}
		s.changed(runID, stepID, WorkflowChangeStepRetried)
		return s.Show(ctx, runID)
	}
	return nil, fmt.Errorf("%w: step %s has no interrupted agent to resume", store.ErrWorkflowStateConflict, stepID)
}

// endSuspendedStep fails a suspended step the operator retries, and
// dismisses its interrupted agent: the retry starts afresh.
func (s *WorkflowService) endSuspendedStep(ctx context.Context, view *store.WorkflowRunView, stepID string) error {
	for _, attempt := range view.Attempts {
		if attempt.Path != stepID || attempt.Status != store.WorkflowSuspended {
			continue
		}
		if err := s.store.FailWorkflowStep(ctx, view.Run.ID, stepID, attempt.ID, workflowRetriedReason); err != nil {
			return err
		}
		s.dismissInterrupted(ctx, attempt)
	}
	return nil
}

// dismissSuspendedAgents dismisses the interrupted agents of a cancelled
// run's suspended steps.
func (s *WorkflowService) dismissSuspendedAgents(ctx context.Context, view *store.WorkflowRunView) {
	for _, attempt := range view.Attempts {
		if attempt.Status == store.WorkflowSuspended {
			s.dismissInterrupted(ctx, attempt)
		}
	}
}

func (s *WorkflowService) dismissInterrupted(ctx context.Context, attempt store.WorkflowStepAttempt) {
	if s.interrupted == nil {
		return
	}
	if task := s.attemptAgentTask(ctx, attempt); task != "" {
		if err := s.interrupted.DismissInterruptedAgent(ctx, task); err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Printf("workflow: dismissing the interrupted agent of %s: %v", attempt.Path, err)
		}
	}
}
