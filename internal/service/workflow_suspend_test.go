package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// survivingWorkflow is a run whose first step is the card's own agent at
// work, and whose second starts once that step completes.
const survivingWorkflow = `
schema_version: 1
name: survives
title: Survives
inputs:
  task: {type: task, required: true}
steps:
 - id: implement
   title: Implement
   type: agent.run
   with: {task: "${{ workflow.inputs.task }}", prompt: Build it, agent: claude-code}
   outputs:
     pr_number: {type: integer}
 - id: next
   title: Next
   type: agent.run
   with: {prompt: Go on, agent: claude-code}
`

// suspendFixture is a run of survivingWorkflow on LEG-7, at work on its
// implement step, over a Claude Code agent service and a real store.
type suspendFixture struct {
	restartFixture
	workflows *WorkflowService
	runID     string
	attemptID string
	cwd       string
}

func newSuspendFixture(t *testing.T, keep, reported bool) suspendFixture {
	t.Helper()
	ctx := context.Background()
	f := newRestartFixture(t)
	createTask(t, f.store, "LEG-7")
	yaml := survivingWorkflow
	if keep {
		yaml = strings.Replace(yaml, "agent: claude-code}", "agent: claude-code, keep_agent: true}", 1)
	}
	workflows := NewWorkflowService(f.store, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(f.svc, f.store, "/opt/legato"), "/opt/legato")})
	workflows.SetInterruptedAgents(f.interrupted())
	definition, err := workflowengine.Parse("survives.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, definition, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Attempts[0].Status != "waiting_agent" {
		t.Fatalf("attempt = %+v; want it waiting on LEG-7's agent", view.Attempts[0])
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.store.GetExecution(ctx, session.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if reported {
		f.reportConversation(t, "LEG-7", execution.WorkingDir)
	}
	return suspendFixture{restartFixture: f, workflows: workflows, runID: view.Run.ID, attemptID: view.Attempts[0].ID, cwd: execution.WorkingDir}
}

// stopAndStart is the machine going down with Legato and its agents, then
// Legato starting again.
func (f suspendFixture) stopAndStart(t *testing.T) {
	t.Helper()
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f suspendFixture) show(t *testing.T) *store.WorkflowRunView {
	t.Helper()
	view, err := f.workflows.Show(context.Background(), f.runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (f suspendFixture) implement(t *testing.T) (store.WorkflowStep, store.WorkflowStepAttempt) {
	t.Helper()
	view := f.show(t)
	var step store.WorkflowStep
	for _, s := range view.Steps {
		if s.Path == "implement" {
			step = s
		}
	}
	var attempt store.WorkflowStepAttempt
	for _, a := range view.Attempts {
		if a.Path == "implement" {
			attempt = a
		}
	}
	return step, attempt
}

// attention is what the workflow and the interrupted-agents producers
// would put in the queue now.
func (f suspendFixture) attention(t *testing.T) (workflow, interrupted []AttentionInput) {
	t.Helper()
	ctx := context.Background()
	workflow, err := NewWorkflowAttentionAdapter(f.workflows).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err = NewInterruptedAttentionAdapter(f.interrupted()).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return workflow, interrupted
}

func TestAnAgentStepWhoseSessionIsGoneAtStartupIsSuspended(t *testing.T) {
	f := newSuspendFixture(t, true, true)
	f.stopAndStart(t)

	view := f.show(t)
	step, attempt := f.implement(t)
	if view.Run.Status != store.WorkflowSuspended || step.Status != store.WorkflowSuspended || attempt.Status != store.WorkflowSuspended {
		t.Fatalf("run %s, step %s, attempt %s; want all suspended", view.Run.Status, step.Status, attempt.Status)
	}
	if attempt.ID != f.attemptID || !attempt.Resumable {
		t.Fatalf("attempt = %+v; want the same attempt, resumable", attempt)
	}
	list, err := f.interrupted().ListInterruptedAgents(context.Background())
	if err != nil || len(list) != 1 || list[0].TaskID != "LEG-7" || !list[0].Resumable {
		t.Fatalf("interrupted = %+v, %v; want LEG-7, resumable", list, err)
	}
	if list[0].Workflow == nil || list[0].Workflow.RunID != f.runID || list[0].Workflow.StepID != "implement" {
		t.Fatalf("interrupted workflow = %+v; want run %s step implement", list[0].Workflow, f.runID)
	}
	// One item, the interrupted agents': the step needs no item of its own.
	workflow, interrupted := f.attention(t)
	if len(workflow) != 0 || len(interrupted) != 1 {
		t.Fatalf("attention: workflow %+v, interrupted %+v; want only the interrupted agents' item", workflow, interrupted)
	}
}

func TestResumingASuspendedStepResumesItsAgentOnTheSameAttempt(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "agent finished", true: "agent kept"}[keep], func(t *testing.T) {
			ctx := context.Background()
			f := newSuspendFixture(t, keep, true)
			f.stopAndStart(t)
			f.clearSent()

			if err := f.interrupted().ResumeInterruptedAgent(ctx, "LEG-7", false); err != nil {
				t.Fatal(err)
			}

			lines := f.linesEventually(t, "LEG-7", 2)
			if len(lines) < 2 || !strings.HasPrefix(lines[0], "env -u CLAUDE_CONFIG_DIR claude --resume conversation-1") {
				t.Fatalf("lines = %q; want the saved conversation resumed", lines)
			}
			if !strings.HasPrefix(lines[1], "[legato interrupted] ") || !strings.Contains(lines[1], "implement") || !strings.Contains(lines[1], "still open") {
				t.Fatalf("note = %q; want Legato's tagged note that the step is still open", lines[1])
			}
			if dir := f.tmux.spawnDirs["legato-LEG-7"]; dir != f.cwd {
				t.Fatalf("resumed in %q; want %q", dir, f.cwd)
			}
			env := f.tmux.envVarsFor("legato-LEG-7")
			if env["LEGATO_WORKFLOW_RUN_ID"] != f.runID || env["LEGATO_WORKFLOW_STEP_ID"] != "implement" || env["LEGATO_WORKFLOW_ATTEMPT_ID"] != f.attemptID || env["LEGATO_TASK_ID"] != "LEG-7" {
				t.Fatalf("env = %v; want the same attempt's", env)
			}
			session, err := f.store.GetAgentSessionByTaskID(ctx, "LEG-7")
			if err != nil {
				t.Fatal(err)
			}
			if session.WorkflowRunID != f.runID || session.ExecutionKind != store.ExecutionWorkflowAgent || session.AttributionTaskID != "LEG-7" {
				t.Fatalf("resumed session = %+v; want the workflow's execution again", session)
			}
			view := f.show(t)
			step, attempt := f.implement(t)
			if view.Run.Status != "running" || step.Status != "waiting_agent" || attempt.Status != "waiting_agent" || attempt.ID != f.attemptID {
				t.Fatalf("run %s, step %s, attempt %+v", view.Run.Status, step.Status, attempt)
			}
			if attempt.AgentSessionID == nil || *attempt.AgentSessionID != session.ID {
				t.Fatalf("attempt session = %v; want the resumed session %d", attempt.AgentSessionID, session.ID)
			}
			// At work again, as its hooks report.
			if err := f.svc.SetAgentActivity(ctx, "LEG-7", "working", ""); err != nil {
				t.Fatal(err)
			}
			if workflow, interrupted := f.attention(t); len(workflow) != 0 || len(interrupted) != 0 {
				t.Fatalf("attention after resume: workflow %+v, interrupted %+v; want none", workflow, interrupted)
			}

			done, err := f.workflows.Complete(ctx, f.runID, "implement", f.attemptID, map[string]string{"pr_number": "12"}, nil)
			if err != nil {
				t.Fatalf("completing the resumed attempt: %v", err)
			}
			if done.Run.CurrentStepID != "next" || done.Run.Status != "running" {
				t.Fatalf("run = %+v; want it on to the next step", done.Run)
			}
			kept, err := f.store.GetAgentSessionByTaskID(ctx, "LEG-7")
			if keep && (err != nil || kept.ID != session.ID) {
				t.Fatalf("kept agent = %+v, %v; want the resumed agent still running", kept, err)
			}
			if !keep && err == nil {
				t.Fatalf("agent still running after its step completed: %+v", kept)
			}
		})
	}
}

func TestResumeAllResumesWorkflowAgents(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)

	results := f.interrupted().ResumeAllInterruptedAgents(ctx)
	if len(results) != 1 || results[0].TaskID != "LEG-7" || results[0].Err != nil {
		t.Fatalf("resume all = %+v", results)
	}
	if _, attempt := f.implement(t); attempt.Status != "waiting_agent" {
		t.Fatalf("attempt = %+v; want it waiting on its agent again", attempt)
	}
}

func TestTheRunPageResumesASuspendedStep(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)
	f.clearSent()

	view, err := f.workflows.ResumeStep(ctx, f.runID, "implement")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run = %s; want running", view.Run.Status)
	}
	if lines := f.linesEventually(t, "LEG-7", 1); len(lines) == 0 || !strings.HasPrefix(lines[0], "env -u CLAUDE_CONFIG_DIR claude --resume conversation-1") {
		t.Fatalf("lines = %q; want the conversation resumed", lines)
	}
}

func TestAnAgentStepWhoseAgentCannotBeResumedFailsWithTheReason(t *testing.T) {
	ctx := context.Background()
	// It never reported its conversation: there is nothing to resume.
	f := newSuspendFixture(t, false, false)
	f.stopAndStart(t)

	view := f.show(t)
	step, attempt := f.implement(t)
	if view.Run.Status != "failed" || step.Status != "failed" || attempt.Status != "failed" {
		t.Fatalf("run %s, step %s, attempt %s; want all failed", view.Run.Status, step.Status, attempt.Status)
	}
	if !strings.Contains(attempt.Error, "cannot be resumed") || !strings.Contains(attempt.Error, "conversation") {
		t.Fatalf("error = %q; want why it cannot be resumed", attempt.Error)
	}
	// One item, the failed step's: the agent is not kept as interrupted.
	workflow, interrupted := f.attention(t)
	if len(workflow) != 1 || workflow[0].SignalKind != AttentionSignalWorkflowFailed || len(interrupted) != 0 {
		t.Fatalf("attention: workflow %+v, interrupted %+v; want only the failed step's item", workflow, interrupted)
	}
	retried, err := f.workflows.Retry(ctx, f.runID, "implement", false)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Run.Status != "running" || len(retried.Attempts) != 2 || retried.Attempts[1].Status != "waiting_agent" {
		t.Fatalf("retried = %+v; want a new attempt at work", retried)
	}
}

func TestResumeFailsTheStepWhenItsFolderGoesAfterTheSweep(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)
	if err := os.RemoveAll(f.cwd); err != nil {
		t.Fatal(err)
	}

	err := f.interrupted().ResumeInterruptedAgent(ctx, "LEG-7", false)
	if !errors.Is(err, ErrAgentNotResumable) {
		t.Fatalf("resume = %v; want not resumable", err)
	}
	step, attempt := f.implement(t)
	if step.Status != "failed" || !strings.Contains(attempt.Error, "is gone") {
		t.Fatalf("step %s, attempt error %q; want it failed because the folder is gone", step.Status, attempt.Error)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted = %+v; want none once its step failed", list)
	}
}

func TestDismissingASuspendedStepsAgentFailsTheStep(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)

	if err := f.interrupted().DismissInterruptedAgent(ctx, "LEG-7"); err != nil {
		t.Fatal(err)
	}
	step, attempt := f.implement(t)
	if step.Status != "failed" || !strings.Contains(attempt.Error, "dismissed") {
		t.Fatalf("step %s, attempt error %q; want it failed as dismissed", step.Status, attempt.Error)
	}
	workflow, interrupted := f.attention(t)
	if len(workflow) != 1 || workflow[0].SignalKind != AttentionSignalWorkflowFailed || len(interrupted) != 0 {
		t.Fatalf("attention: workflow %+v, interrupted %+v; want only the failed step's item", workflow, interrupted)
	}
}

func TestRetryingASuspendedStepStartsAFreshAgent(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)
	f.clearSent()

	view, err := f.workflows.Retry(ctx, f.runID, "implement", false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" || len(view.Attempts) != 2 || view.Attempts[0].Status != "failed" || view.Attempts[0].Error != "retried by the operator" || view.Attempts[1].Status != "waiting_agent" {
		t.Fatalf("view = %+v", view)
	}
	if lines := f.linesEventually(t, "LEG-7", 1); len(lines) == 0 || strings.Contains(lines[0], "--resume") {
		t.Fatalf("lines = %q; want a fresh conversation", lines)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted = %+v; want none", list)
	}
}

func TestCancellingASuspendedRunDismissesItsAgent(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.stopAndStart(t)

	view, err := f.workflows.Cancel(ctx, f.runID, "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "cancelled" {
		t.Fatalf("run = %s; want cancelled", view.Run.Status)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted = %+v; want none", list)
	}
}

// A step an earlier Legato failed when its agent was interrupted, before
// steps were suspended, is resumed the same way.
func TestAFailedStepWhoseAgentIsInterruptedCanBeResumed(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	f.crash()
	if err := f.store.FailWorkflowStep(ctx, f.runID, "implement", f.attemptID, legacyAgentExited); err != nil {
		t.Fatal(err)
	}
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	step, attempt := f.implement(t)
	if step.Status != "failed" || !attempt.Resumable {
		t.Fatalf("step %s, attempt %+v; want a failed step offered Resume", step.Status, attempt)
	}

	view, err := f.workflows.ResumeStep(ctx, f.runID, "implement")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run = %s; want running", view.Run.Status)
	}
	if _, attempt := f.implement(t); attempt.Status != "waiting_agent" || attempt.ID != f.attemptID {
		t.Fatalf("attempt = %+v; want the same attempt waiting on its agent", attempt)
	}
	if _, err := f.workflows.Complete(ctx, f.runID, "implement", f.attemptID, map[string]string{"pr_number": "3"}, nil); err != nil {
		t.Fatal(err)
	}
}

// A Legato restart keeps tmux, so its agents go on working: nothing is
// suspended, and a step completed from the pane meanwhile still moves the
// run on when Legato is back.
func TestStoppingLegatoWithTmuxAliveLeavesAgentStepsWaiting(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	pane := NewWorkflowService(f.store, nil)
	pane.SetRecordOnly()
	if _, err := pane.Complete(ctx, f.runID, "implement", f.attemptID, map[string]string{"pr_number": "4"}, nil); err != nil {
		t.Fatal(err)
	}

	count, err := f.interrupted().SweepInterruptedAgents(ctx)
	if err != nil || count != 0 {
		t.Fatalf("sweep = %d, %v; want nothing interrupted", count, err)
	}
	if _, err := f.workflows.AdvanceDue(ctx); err != nil {
		t.Fatal(err)
	}
	view := f.show(t)
	if view.Run.Status != "running" || view.Run.CurrentStepID != "next" {
		t.Fatalf("run = %+v; want it on to the next step", view.Run)
	}
}

func TestAnAgentStepWithTmuxAliveStillWaitsAfterTheSweep(t *testing.T) {
	ctx := context.Background()
	f := newSuspendFixture(t, false, true)
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if step, attempt := f.implement(t); step.Status != "waiting_agent" || attempt.Status != "waiting_agent" {
		t.Fatalf("step %s, attempt %s; want both still waiting on the agent", step.Status, attempt.Status)
	}
}

// Steps that wait on a service or on the operator have no agent session:
// the startup sweep leaves them waiting, as it found them.
func TestStepsWaitingOnAServiceOrTheOperatorSurviveTheStartupSweep(t *testing.T) {
	ctx := context.Background()
	f := newRestartFixture(t)
	waiting := &waitingExecutor{typeName: "github.pr.wait"}
	gate := &fakeWorkflowExecutor{typeName: "file.remove", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	workflows := NewWorkflowService(f.store, []WorkflowExecutor{waiting, gate})
	var runs []string
	for _, yaml := range []string{
		"schema_version: 1\nname: wait\ntitle: Wait\nsteps:\n  - {id: wait, title: Wait, type: github.pr.wait, with: {repo: acme/web, pr_number: 1}}\n",
		"schema_version: 2\nname: gate\ntitle: Gate\nsteps:\n  - {id: gate, title: Gate, type: file.remove, start: manual, with: {}}\n",
	} {
		definition, err := workflowengine.Parse("waits.yaml", []byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		view, err := workflows.Start(ctx, definition, nil)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, view.Run.ID)
	}
	// An agent was running too when the machine went down.
	f.interruptedAgent(t, "task-i", t.TempDir(), true)

	want := map[string][2]string{runs[0]: {"running", WorkflowStepWaiting}, runs[1]: {"waiting", "waiting_manual"}}
	for runID, statuses := range want {
		view, err := workflows.Show(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Run.Status != statuses[0] || view.Steps[0].Status != statuses[1] {
			t.Fatalf("run %s: %s, step %s; want %s, %s", runID, view.Run.Status, view.Steps[0].Status, statuses[0], statuses[1])
		}
	}
}
