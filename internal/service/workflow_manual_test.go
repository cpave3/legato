package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func newManualWorkflow(t *testing.T) (*workflowengine.Definition, *fakeWorkflowExecutor, *fakeWorkflowExecutor) {
	t.Helper()
	definition, err := workflowengine.Parse("manual.yaml", []byte(`
schema_version: 1
name: manual
title: Manual
steps:
  - id: automatic
    title: Automatic
    type: github.pr.prepare
    with: {}
  - id: cleanup
    title: Cleanup
    type: worktree.remove
    trigger: manual
    with: {expected_path: /tmp/worktree}
`))
	if err != nil {
		t.Fatal(err)
	}
	automatic := &fakeWorkflowExecutor{typeName: "github.pr.prepare", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	cleanup := &fakeWorkflowExecutor{typeName: "worktree.remove", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true}}}
	return definition, automatic, cleanup
}

func TestWorkflowWaitsBeforeStartingManualStep(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, automatic, cleanup := newManualWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{automatic, cleanup})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "waiting" || view.Run.CurrentStepID != "cleanup" || view.Steps[1].Status != "waiting_manual" {
		t.Fatalf("view=%+v", view)
	}
	if len(view.Attempts) != 1 || len(cleanup.starts) != 0 {
		t.Fatalf("attempts=%+v cleanup starts=%d", view.Attempts, len(cleanup.starts))
	}
	runs, err := svc.ListRuns(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].NeedsAttention || runs[0].AttentionReason != "manual action required: Cleanup" {
		t.Fatalf("runs=%+v", runs)
	}
}

func TestCancelWaitingManualStepDoesNotRunIt(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, automatic, cleanup := newManualWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{automatic, cleanup})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Cancel(context.Background(), view.Run.ID, "cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "cancelled" || view.Steps[1].Status != "cancelled" || len(cleanup.starts) != 0 {
		t.Fatalf("view=%+v starts=%d", view, len(cleanup.starts))
	}
}

func TestTriggerManualWorktreeRemovalCanSelectForce(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, automatic, cleanup := newManualWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{automatic, cleanup})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TriggerStep(context.Background(), view.Run.ID, "cleanup", true); err != nil {
		t.Fatal(err)
	}
	if force, _ := cleanup.starts[0].With["force"].(bool); !force {
		t.Fatalf("inputs = %+v, want force=true", cleanup.starts[0].With)
	}
}

func TestTriggerManualStepStartsItExactlyOnce(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, automatic, cleanup := newManualWorkflow(t)
	svc := NewWorkflowService(db, []WorkflowExecutor{automatic, cleanup})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.TriggerStep(context.Background(), view.Run.ID, "cleanup", false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || len(cleanup.starts) != 1 || len(view.Attempts) != 2 {
		t.Fatalf("view=%+v starts=%d", view, len(cleanup.starts))
	}
	if _, err := svc.TriggerStep(context.Background(), view.Run.ID, "cleanup", false); err == nil {
		t.Fatal("duplicate trigger succeeded")
	}
	if len(cleanup.starts) != 1 {
		t.Fatalf("cleanup started %d times", len(cleanup.starts))
	}
}
