package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// waitingExecutor parks its step until its service completes or fails it.
type waitingExecutor struct {
	typeName string
	starts   []ResolvedWorkflowStep
	cancels  []store.WorkflowStepAttempt
}

func (w *waitingExecutor) Type() string { return w.typeName }
func (w *waitingExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	w.starts = append(w.starts, step)
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}
func (w *waitingExecutor) Cancel(_ context.Context, attempt store.WorkflowStepAttempt) error {
	w.cancels = append(w.cancels, attempt)
	return nil
}

func newWaitingWorkflow(t *testing.T) (*store.Store, *workflowengine.Definition, *waitingExecutor, *fakeWorkflowExecutor) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definition, err := workflowengine.Parse("waiting.yaml", []byte(`
schema_version: 1
name: waiting
title: Waiting
steps:
  - id: review
    title: Review
    type: github.pr.prepare
    with: {}
    outputs:
      outcome: {type: string}
  - id: after
    title: After
    type: file.remove
    with: {outcome: "${{ steps.review.outputs.outcome }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	waiting := &waitingExecutor{typeName: "github.pr.prepare"}
	after := &fakeWorkflowExecutor{typeName: "file.remove", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	return db, definition, waiting, after
}

func TestAWaitingStepParksUntilItsServiceCompletesIt(t *testing.T) {
	ctx := context.Background()
	db, definition, waiting, after := newWaitingWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{waiting, after})
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" || view.Run.CurrentStepID != "review" || view.Steps[0].Status != WorkflowStepWaiting || view.Attempts[0].Status != WorkflowStepWaiting {
		t.Fatalf("view = %+v", view)
	}
	if len(after.starts) != 0 {
		t.Fatalf("the next step started while the first waited")
	}
	attempt := view.Attempts[0]
	view, err = svc.CompleteWaiting(ctx, attempt.RunID, attempt.StepID, attempt.ID, map[string]any{"outcome": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || view.Steps[0].Status != "succeeded" {
		t.Fatalf("view = %+v", view)
	}
	if len(after.starts) != 1 || after.starts[0].With["outcome"] != "approved" {
		t.Fatalf("after starts = %+v", after.starts)
	}
	if _, err := svc.CompleteWaiting(ctx, attempt.RunID, attempt.StepID, attempt.ID, map[string]any{"outcome": "approved"}); err == nil {
		t.Fatal("completing a finished step again succeeded")
	}
}

func TestFailingAWaitingStepFailsTheRun(t *testing.T) {
	ctx := context.Background()
	db, definition, waiting, after := newWaitingWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{waiting, after})
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	view, err = svc.FailWaiting(ctx, attempt.RunID, attempt.StepID, attempt.ID, "the pull request is gone")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || view.Steps[0].Status != "failed" || view.Steps[0].Error != "the pull request is gone" || len(after.starts) != 0 {
		t.Fatalf("view = %+v", view)
	}
}

func TestCancellingARunReachesItsWaitingStepsExecutor(t *testing.T) {
	ctx := context.Background()
	db, definition, waiting, after := newWaitingWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{waiting, after})
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Cancel(ctx, view.Run.ID, "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "cancelled" || view.Steps[0].Status != "cancelled" || view.Attempts[0].Status != "cancelled" {
		t.Fatalf("view = %+v", view)
	}
	if len(waiting.cancels) != 1 || waiting.cancels[0].StepID != "review" {
		t.Fatalf("cancels = %+v", waiting.cancels)
	}
}

func TestAWaitingStepStillWaitsAfterARestart(t *testing.T) {
	ctx := context.Background()
	db, definition, waiting, after := newWaitingWorkflow(t)
	view, err := NewWorkflowService(db, []WorkflowExecutor{waiting, after}).Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A new Legato on the same database, which also sweeps ended agents.
	restarted := NewWorkflowService(db, []WorkflowExecutor{waiting, after})
	if _, err := restarted.AgentEnded(ctx, view.Run.TaskID); err != nil {
		t.Fatal(err)
	}
	view, err = restarted.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" || view.Steps[0].Status != WorkflowStepWaiting || view.Attempts[0].Status != WorkflowStepWaiting {
		t.Fatalf("view = %+v", view)
	}
	attempt := view.Attempts[0]
	if _, err := restarted.CompleteWaiting(ctx, attempt.RunID, attempt.StepID, attempt.ID, map[string]any{"outcome": "closed"}); err != nil {
		t.Fatal(err)
	}
	waits, err := restarted.WaitingAttempts(ctx, "github.pr.prepare")
	if err != nil {
		t.Fatal(err)
	}
	if len(waits) != 0 {
		t.Fatalf("waits = %+v", waits)
	}
}

func TestWaitingAttemptsListsAnExecutorsParkedSteps(t *testing.T) {
	ctx := context.Background()
	db, definition, waiting, after := newWaitingWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{waiting, after})
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	waits, err := svc.WaitingAttempts(ctx, "github.pr.prepare")
	if err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0].ID != view.Attempts[0].ID {
		t.Fatalf("waits = %+v", waits)
	}
	if others, _ := svc.WaitingAttempts(ctx, "file.remove"); len(others) != 0 {
		t.Fatalf("others = %+v", others)
	}
}

func TestAnOptionalTaskNoteWithNoTaskAddsNothing(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	executor := NewTaskNoteExecutor(db)
	for _, task := range []any{"", nil, map[string]any{"id": ""}} {
		result, err := executor.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"task": task, "body": "summary", "optional": true}})
		if err != nil {
			t.Fatalf("task %#v: %v", task, err)
		}
		if result.Status != "succeeded" || result.Outputs["note_id"] != "" || result.Outputs["skipped"] != true {
			t.Fatalf("task %#v: result = %+v", task, result)
		}
	}
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"task": "", "body": "summary"}}); err == nil {
		t.Fatal("a note with no task and no optional succeeded")
	}
}

func TestATaskNoteTakesATaskIDAsText(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "abc12345", Title: "Linked"}); err != nil {
		t.Fatal(err)
	}
	result, err := NewTaskNoteExecutor(db).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"task": "abc12345", "body": "summary", "optional": true}})
	if err != nil {
		t.Fatal(err)
	}
	notes, _ := db.ListTaskNotes(context.Background(), "abc12345")
	if len(notes) != 1 || result.Outputs["task_id"] != "abc12345" {
		t.Fatalf("notes = %+v result = %+v", notes, result)
	}
}
