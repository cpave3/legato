package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// A workflow's agent.run step whose agent was interrupted (LEG-625) is
// suspended, not failed: resuming the agent resumes its conversation on the
// same attempt, and the step goes on.

// WorkflowSuspendedReason is the error a suspended step, and its run, carry.
const WorkflowSuspendedReason = "Legato stopped while the agent was working"

// legacyAgentExited is how a Legato before suspended steps failed the step
// of an interrupted agent. Such a step is offered Resume too.
const legacyAgentExited = "agent exited before step completion"

// workflowDismissedReason fails a suspended step whose agent the operator
// dismissed.
const workflowDismissedReason = "its agent was dismissed after Legato stopped"

// WorkflowResumedNote is the first message of a workflow step's agent
// resumed after Legato stopped: the step it was on is still open.
func WorkflowResumedNote(stepID, runID string) string {
	return fmt.Sprintf("[legato interrupted] Legato stopped while you were working on workflow step %s of run %s, and your conversation has been resumed. The step is still open: check the state of what you were doing, finish its declared outputs, then run the completion command from your assignment.", stepID, runID)
}

// InterruptedWorkflow is the workflow step an interrupted agent works on.
type InterruptedWorkflow struct {
	RunID    string
	StepID   string
	RunTitle string
}

// interruptedAttempt is the workflow step attempt an interrupted agent's
// resume continues: the suspended one that waits on it, or one an earlier
// Legato failed when the agent was interrupted (reopen), which is still the
// step's latest and where its run stopped. nil for none.
type interruptedAttempt struct {
	attempt store.WorkflowStepAttempt
	reopen  bool
}

func (a *agentService) interruptedAttemptOf(ctx context.Context, s store.AgentSession) *interruptedAttempt {
	if s.WorkflowRunID == "" {
		return nil
	}
	if suspended, err := a.store.ListSuspendedWorkflowAttemptsByAgentSession(ctx, s.ID); err == nil && len(suspended) > 0 {
		return &interruptedAttempt{attempt: suspended[0]}
	}
	view, err := a.store.GetWorkflowRun(ctx, s.WorkflowRunID)
	if err != nil {
		return nil
	}
	for _, attempt := range view.Attempts {
		if attempt.AgentSessionID != nil && *attempt.AgentSessionID == s.ID && legacyInterruptedAttempt(view, attempt) {
			return &interruptedAttempt{attempt: attempt, reopen: true}
		}
	}
	return nil
}

// legacyInterruptedAttempt says whether a failed attempt is one an earlier
// Legato failed when its agent was interrupted, and its step and run
// stopped there: the attempt is its step's latest, at the run's step.
func legacyInterruptedAttempt(view *store.WorkflowRunView, attempt store.WorkflowStepAttempt) bool {
	if attempt.Status != "failed" || attempt.Error != legacyAgentExited || attempt.ExecutorType != "agent.run" {
		return false
	}
	if view.Run.Status != "failed" || view.Run.CurrentStepID != attempt.Path {
		return false
	}
	for _, step := range view.Steps {
		if step.Path == attempt.Path {
			return step.Status == "failed" && step.LatestAttempt == attempt.AttemptNumber
		}
	}
	return false
}

// workflowResumeOptions makes a workflow agent's resume the workflow's own
// again: its execution kind, run, attribution, slot and name, and for an
// open step the env its completion command reads. The new execution names
// no attempt: one execution only starts each attempt (the attempt is
// pointed at the resumed session instead, ResumeWorkflowStep).
func (a *agentService) workflowResumeOptions(s store.AgentSession, opt *AgentSpawnOptions, open *interruptedAttempt) {
	if s.WorkflowRunID == "" && s.ExecutionKind != store.ExecutionWorkflowAgent {
		return
	}
	opt.ExecutionKind = store.ExecutionWorkflowAgent
	opt.WorkflowRunID = s.WorkflowRunID
	opt.AttributionTaskID, opt.Slot, opt.Label = s.AttributionTaskID, s.Slot, s.ExecutionLabel
	opt.ExtraEnv = map[string]string{"LEGATO_TASK_ID": s.TaskID}
	if open == nil {
		return
	}
	attempt := open.attempt
	opt.WorkflowRunID = attempt.RunID
	opt.ExtraEnv["LEGATO_WORKFLOW_RUN_ID"] = attempt.RunID
	opt.ExtraEnv["LEGATO_WORKFLOW_STEP_ID"] = attempt.StepID
	opt.ExtraEnv["LEGATO_WORKFLOW_ATTEMPT_ID"] = attempt.ID
	if a.binaryPath != "" {
		opt.ExtraEnv["LEGATO_WORKFLOW_BINARY"] = a.binaryPath
	}
}

// interruptedWorkflowOf names the step an interrupted agent's resume
// continues, for the lists that show it.
func (a *agentService) interruptedWorkflowOf(ctx context.Context, open *interruptedAttempt) *InterruptedWorkflow {
	if open == nil {
		return nil
	}
	workflow := &InterruptedWorkflow{RunID: open.attempt.RunID, StepID: open.attempt.Path}
	if view, err := a.store.GetWorkflowRun(ctx, open.attempt.RunID); err == nil {
		workflow.RunTitle = view.Run.WorkflowTitle
	}
	return workflow
}

// failInterruptedStep fails the open step of an interrupted agent that will
// not go on: it cannot be resumed, or was dismissed.
func (a *agentService) failInterruptedStep(ctx context.Context, open *interruptedAttempt, reason string) {
	if open == nil {
		return
	}
	attempt := open.attempt
	if !open.reopen {
		if err := a.store.FailWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, reason); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("workflow: failing step %s of %s: %v", attempt.Path, attempt.RunID, err)
		}
	}
	if a.bus != nil {
		a.bus.PublishWorkflowChanged(attempt.RunID)
	}
}

// resumeInterruptedStep starts an interrupted workflow agent again on its
// open step's attempt, which then waits on the new agent.
func (a *agentService) resumeInterruptedStep(ctx context.Context, taskID string, opt AgentSpawnOptions, open *interruptedAttempt) error {
	attempt := open.attempt
	if open.reopen {
		if err := a.store.ReopenWorkflowStepSuspended(ctx, attempt.RunID, attempt.Path, attempt.ID, WorkflowSuspendedReason); err != nil {
			return fmt.Errorf("reopening step %s of %s: %w", attempt.Path, attempt.RunID, err)
		}
	}
	opt.Prompt = WorkflowResumedNote(attempt.Path, attempt.RunID)
	return a.continueWorkflowStep(ctx, taskID, opt, open)
}

// continueWorkflowStep starts a workflow agent on its suspended step's
// attempt, with opt's first message, and points the attempt at it: after
// Legato stopped (resumeInterruptedStep) or a reload.
func (a *agentService) continueWorkflowStep(ctx context.Context, taskID string, opt AgentSpawnOptions, open *interruptedAttempt) error {
	attempt := open.attempt
	if err := a.SpawnAgent(ctx, taskID, 0, 0, opt); err != nil {
		return err
	}
	session, err := a.store.GetAgentSessionInSlot(ctx, taskID, opt.Slot)
	if err != nil {
		return fmt.Errorf("finding the resumed agent of step %s: %w", attempt.Path, err)
	}
	if err := a.store.ResumeWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, *session); err != nil {
		return fmt.Errorf("resuming step %s of %s: %w", attempt.Path, attempt.RunID, err)
	}
	log.Printf("workflow agent resumed run=%s step=%s attempt=%s", attempt.RunID, attempt.Path, attempt.ID)
	if a.bus != nil {
		a.bus.PublishWorkflowChanged(attempt.RunID)
	}
	return nil
}

// waitsOnAgentStep says whether an agent.run step waits on the session.
func (a *agentService) waitsOnAgentStep(ctx context.Context, sessionID int) bool {
	attempts, err := a.store.ListWaitingWorkflowAttemptsByAgentSession(ctx, sessionID)
	if err != nil {
		return false
	}
	for _, attempt := range attempts {
		if attempt.ExecutorType == "agent.run" {
			return true
		}
	}
	return false
}

// workflowNotResumable is the error of a step whose interrupted agent
// cannot be resumed.
func workflowNotResumable(err error) string {
	return WorkflowSuspendedReason + ", and it cannot be resumed: " + reasonOf(err)
}
