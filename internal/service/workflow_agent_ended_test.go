package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// startsTaskAgent is an agent.dispatch that leaves a running agent on the
// workflow task, as the real one does.
type startsTaskAgent struct {
	store  *store.Store
	starts int
	// exits leaves no agent behind: one that ended before the run moved on.
	exits bool
}

func (e *startsTaskAgent) Type() string { return "agent.dispatch" }
func (e *startsTaskAgent) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts++
	if e.exits {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
	}
	if err := e.store.InsertAgentSession(ctx, store.AgentSession{TaskID: step.TaskID, TmuxSession: "legato-" + step.TaskID, Status: "running"}); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
}
func (e *startsTaskAgent) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

type discussionFixture struct {
	svc     *WorkflowService
	store   *store.Store
	agent   *startsTaskAgent
	stop    *fakeWorkflowExecutor
	cleanup *fakeWorkflowExecutor
	def     *workflowengine.Definition
}

func newDiscussionFixture(t *testing.T) discussionFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definition, err := workflowengine.Parse("discuss.yaml", []byte(`
schema_version: 1
name: discuss
title: Discuss
steps:
  - id: discuss
    title: Discuss
    type: agent.dispatch
    with:
      task: ${{ workflow.task }}
      working_dir: /repo
      prompt: Read the diff.
  - id: end
    title: End discussion
    type: agent.stop
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
  - id: cleanup
    title: Remove worktree
    type: worktree.remove
    with: {expected_path: /repo}
`))
	if err != nil {
		t.Fatal(err)
	}
	agent := &startsTaskAgent{store: db}
	stop := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	cleanup := &fakeWorkflowExecutor{typeName: "worktree.remove", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true}}}
	svc := NewWorkflowService(db, []WorkflowExecutor{agent, stop, cleanup})
	return discussionFixture{svc: svc, store: db, agent: agent, stop: stop, cleanup: cleanup, def: definition}
}

func (f discussionFixture) endAgent(t *testing.T, taskID string) {
	t.Helper()
	if err := f.store.UpdateAgentSessionStatus(context.Background(), taskID, "dead"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentEndedStepWaitsWhileTheAgentRunsAndStartsWhenItEnds(t *testing.T) {
	f := newDiscussionFixture(t)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "waiting" || view.Run.CurrentStepID != "end" || len(f.stop.starts) != 0 {
		t.Fatalf("run = %+v, stop starts = %d; want it waiting on the agent", view.Run, len(f.stop.starts))
	}

	f.endAgent(t, view.Run.TaskID)
	changed, err := f.svc.AgentEnded(ctx, view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}

	if len(changed) != 1 || changed[0] != view.Run.ID {
		t.Fatalf("changed runs = %v, want %s", changed, view.Run.ID)
	}
	after, err := f.svc.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Run.Status != "succeeded" || len(f.stop.starts) != 1 || len(f.cleanup.starts) != 1 {
		t.Fatalf("run = %+v, stop = %d, cleanup = %d; want both run and the run done", after.Run, len(f.stop.starts), len(f.cleanup.starts))
	}
}

func TestAgentEndedStepStartsAtOnceWhenTheAgentIsAlreadyGone(t *testing.T) {
	f := newDiscussionFixture(t)
	f.agent.exits = true

	view, err := f.svc.Start(context.Background(), f.def, nil)
	if err != nil {
		t.Fatal(err)
	}

	if view.Run.Status != "succeeded" || len(f.stop.starts) != 1 || len(f.cleanup.starts) != 1 {
		t.Fatalf("run = %+v, stop = %d, cleanup = %d; want the run to finish without waiting", view.Run, len(f.stop.starts), len(f.cleanup.starts))
	}
}

func TestAgentEndedLeavesTheRunWaitingWhileTheTaskStillHasAnAgent(t *testing.T) {
	f := newDiscussionFixture(t)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := f.svc.AgentEnded(ctx, view.Run.TaskID)

	if err != nil || len(changed) != 0 || len(f.stop.starts) != 0 {
		t.Fatalf("changed = %v, err = %v, stop = %d; want nothing while the agent runs", changed, err, len(f.stop.starts))
	}
}

func TestAgentEndedLoopFinishesARunWhenItsAgentDies(t *testing.T) {
	f := newDiscussionFixture(t)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	changes := bus.Subscribe(events.EventWorkflowChanged)
	stop := f.svc.StartAgentEndedLoop(ctx, bus)
	defer stop()

	f.endAgent(t, view.Run.TaskID)
	bus.Publish(events.Event{Type: events.EventAgentDied, Payload: events.AgentDiedPayload{TaskID: view.Run.TaskID}})

	select {
	case event := <-changes:
		if payload, _ := event.Payload.(events.WorkflowChangedPayload); payload.RunID != view.Run.ID {
			t.Fatalf("workflow change = %+v, want run %s", event.Payload, view.Run.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the run was not moved on after its agent died")
	}
	if after, _ := f.svc.Show(ctx, view.Run.ID); after.Run.Status != "succeeded" {
		t.Fatalf("run = %+v, want succeeded", after.Run)
	}
}

// An agent that ended while no Legato was running is noticed when one starts.
func TestAgentEndedLoopFinishesRunsWhoseAgentEndedBeforeItStarted(t *testing.T) {
	f := newDiscussionFixture(t)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.endAgent(t, view.Run.TaskID)

	stop := f.svc.StartAgentEndedLoop(ctx, events.New())
	defer stop()

	if after, _ := f.svc.Show(ctx, view.Run.ID); after.Run.Status != "succeeded" {
		t.Fatalf("run = %+v, want succeeded after the startup check", after.Run)
	}
}

// A run waiting on a conversation is not waiting on the person, so it does
// not count toward the Workflows badge.
func TestARunWaitingOnALiveAgentDoesNotNeedAttention(t *testing.T) {
	f := newDiscussionFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.def, nil); err != nil {
		t.Fatal(err)
	}

	count, err := f.svc.AttentionCount(ctx)

	if err != nil || count != 0 {
		t.Fatalf("attention count = %d, %v; want 0 while the agent runs", count, err)
	}
}

// runsTaskAgent is an agent.run that starts an agent on the workflow task and
// waits for it to complete the step; finishing it removes the agent, as the
// real one does.
type runsTaskAgent struct {
	store    *store.Store
	finished int
}

func (e *runsTaskAgent) Type() string { return "agent.run" }
func (e *runsTaskAgent) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if err := e.store.InsertAgentSession(ctx, store.AgentSession{TaskID: step.TaskID, TmuxSession: "legato-" + step.TaskID, Status: "running"}); err != nil {
		return WorkflowExecutionResult{}, err
	}
	session, err := e.store.GetAgentSessionByTaskID(ctx, step.TaskID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session}, nil
}
func (e *runsTaskAgent) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *runsTaskAgent) Finish(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	e.finished++
	return e.store.UpdateAgentSessionStatus(ctx, attempt.TaskID, "dead")
}

func TestAgentRunThatKeepsItsAgentLeavesItRunningUntilItEnds(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definition, err := workflowengine.Parse("review.yaml", []byte(`
schema_version: 1
name: review
title: Review
steps:
  - id: review
    title: Review
    type: agent.run
    with:
      prompt: Review the diff.
      keep_agent: true
  - id: end
    title: End review
    type: agent.stop
    trigger: agent_ended
    with:
      task: ${{ workflow.task }}
  - id: cleanup
    title: Remove worktree
    type: worktree.remove
    with: {expected_path: /repo}
`))
	if err != nil {
		t.Fatal(err)
	}
	review := &runsTaskAgent{store: db}
	stop := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	cleanup := &fakeWorkflowExecutor{typeName: "worktree.remove", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true}}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{review, stop, cleanup}, t.TempDir())
	ctx := context.Background()
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}

	completed, err := svc.Complete(ctx, view.Run.ID, "review", view.Attempts[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if completed.Run.Status != "waiting" || completed.Run.CurrentStepID != "end" || review.finished != 0 {
		t.Fatalf("run = %+v, finished = %d; want it waiting on the agent it kept", completed.Run, review.finished)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, view.Run.TaskID); err != nil {
		t.Fatalf("the review agent is gone: %v", err)
	}

	if err := db.UpdateAgentSessionStatus(ctx, view.Run.TaskID, "dead"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AgentEnded(ctx, view.Run.TaskID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Run.Status != "succeeded" || len(cleanup.starts) != 1 {
		t.Fatalf("run = %+v, cleanup = %d; want the worktree removed once the agent ended", after.Run, len(cleanup.starts))
	}
}
