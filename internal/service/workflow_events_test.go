package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// workflowChanges collects what a workflow service publishes on its bus.
type workflowChanges struct {
	t  *testing.T
	ch <-chan events.Event
}

func watchWorkflowChanges(t *testing.T, svc *WorkflowService) *workflowChanges {
	t.Helper()
	bus := events.New()
	svc.SetEventBus(bus)
	return &workflowChanges{t: t, ch: bus.Subscribe(events.EventWorkflowChanged)}
}

// drain gives what has been published so far.
func (w *workflowChanges) drain() []events.WorkflowChangedPayload {
	var got []events.WorkflowChangedPayload
	for {
		select {
		case event := <-w.ch:
			got = append(got, event.Payload.(events.WorkflowChangedPayload))
		case <-time.After(50 * time.Millisecond):
			return got
		}
	}
}

// expect checks that what was published since the last call names the run
// and includes kind for step, and is never completed_step: the advance loop
// acts on that one, and an in-process completion has advanced already.
func (w *workflowChanges) expect(what, runID, stepID, kind string) {
	w.t.Helper()
	got := w.drain()
	found := false
	for _, change := range got {
		if change.RunID != runID {
			w.t.Fatalf("%s: a change of another run: %+v", what, change)
		}
		if change.Kind == "completed_step" {
			w.t.Fatalf("%s: published completed_step: %+v", what, got)
		}
		if change.StepID == stepID && change.Kind == kind {
			found = true
		}
	}
	if !found {
		w.t.Fatalf("%s: want %s of step %q among %+v", what, kind, stepID, got)
	}
}

const publishingWorkflow = `
schema_version: 1
name: publish
title: Publish
steps:
  - id: prepare
    title: Prepare
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: approve
    title: Approve
    type: task.context
    trigger: manual
    with: {task: "${{ workflow.task }}"}
  - id: implement
    title: Implement
    type: agent.run
    with: {prompt: go}
  - id: merged
    title: Merged
    type: github.pr.wait
    with: {repo: acme/widget, pr_number: 12, until: merged}
`

func publishingService(t *testing.T) (*WorkflowService, *workflowengine.Definition, *fakeWorkflowExecutor) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definition, err := workflowengine.Parse("publish.yaml", []byte(publishingWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	waiting := &fakeWorkflowExecutor{typeName: "github.pr.wait", result: WorkflowExecutionResult{Status: WorkflowStepWaiting}}
	svc := NewWorkflowService(db, []WorkflowExecutor{
		&fakeWorkflowExecutor{typeName: "task.context", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}},
		&fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}},
		waiting,
	})
	return svc, definition, waiting
}

func TestEveryRunChangeIsPublished(t *testing.T) {
	ctx := context.Background()
	svc, definition, waiting := publishingService(t)
	changes := watchWorkflowChanges(t, svc)

	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	runID := view.Run.ID
	changes.expect("start", runID, "", WorkflowChangeRunStarted)
	// A list that saw the run start still sees its first steps go by.
	view, _ = svc.Show(ctx, runID)
	if view.Run.CurrentStepID != "approve" {
		t.Fatalf("run at %q", view.Run.CurrentStepID)
	}

	if _, err := svc.TriggerStep(ctx, runID, "approve", false); err != nil {
		t.Fatal(err)
	}
	changes.expect("trigger", runID, "implement", WorkflowChangeStepWaiting)

	view, _ = svc.Show(ctx, runID)
	agentAttempt := latestAttempt(t, view, "implement")
	if _, err := svc.Complete(ctx, runID, "implement", agentAttempt.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	changes.expect("complete", runID, "implement", WorkflowChangeStepCompleted)

	view, _ = svc.Show(ctx, runID)
	waitingAttempt := latestAttempt(t, view, "merged")
	if _, err := svc.FailWaiting(ctx, runID, "merged", waitingAttempt.ID, "closed unmerged"); err != nil {
		t.Fatal(err)
	}
	changes.expect("fail waiting", runID, "merged", WorkflowChangeStepFailed)

	if _, err := svc.Retry(ctx, runID, "merged", false); err != nil {
		t.Fatal(err)
	}
	changes.expect("retry", runID, "merged", WorkflowChangeStepRetried)

	view, _ = svc.Show(ctx, runID)
	waitingAttempt = latestAttempt(t, view, "merged")
	if _, err := svc.CompleteWaiting(ctx, runID, "merged", waitingAttempt.ID, nil); err != nil {
		t.Fatal(err)
	}
	changes.expect("complete waiting", runID, "merged", WorkflowChangeStepCompleted)
	if len(waiting.starts) != 2 {
		t.Fatalf("waiting starts = %d", len(waiting.starts))
	}
}

func TestACancelledAndAFailedRunArePublished(t *testing.T) {
	ctx := context.Background()
	svc, definition, _ := publishingService(t)
	changes := watchWorkflowChanges(t, svc)
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes.drain()
	if _, err := svc.Cancel(ctx, view.Run.ID, "not wanted"); err != nil {
		t.Fatal(err)
	}
	changes.expect("cancel", view.Run.ID, "", WorkflowChangeRunCancelled)

	// An executor that refuses to start fails its step.
	failing := &erroringWorkflowExecutor{typeName: "task.context"}
	db := svc.store
	svc = NewWorkflowService(db, []WorkflowExecutor{failing,
		&fakeWorkflowExecutor{typeName: "agent.run"}, &fakeWorkflowExecutor{typeName: "github.pr.wait"}})
	changes = watchWorkflowChanges(t, svc)
	view, err = svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes.expect("executor failed", view.Run.ID, "prepare", WorkflowChangeStepFailed)
}

func TestATriggeredRunIsPublished(t *testing.T) {
	svc, definition, _ := publishingService(t)
	changes := watchWorkflowChanges(t, svc)
	view, err := svc.StartTriggered(context.Background(), definition, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	changes.expect("triggered start", view.Run.ID, "", WorkflowChangeRunStarted)
}

type erroringWorkflowExecutor struct{ typeName string }

func (e *erroringWorkflowExecutor) Type() string { return e.typeName }
func (e *erroringWorkflowExecutor) Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	return WorkflowExecutionResult{}, errors.New("refused")
}
func (e *erroringWorkflowExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func latestAttempt(t *testing.T, view *store.WorkflowRunView, path string) store.WorkflowStepAttempt {
	t.Helper()
	var found *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if view.Attempts[i].Path == path {
			found = &view.Attempts[i]
		}
	}
	if found == nil {
		t.Fatalf("no attempt of %s in %+v", path, view.Attempts)
	}
	return *found
}
