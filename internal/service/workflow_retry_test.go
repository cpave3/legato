package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type forceRetryExecutor struct {
	starts []ResolvedWorkflowStep
}

func (e *forceRetryExecutor) Type() string { return "worktree.remove" }
func (e *forceRetryExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts = append(e.starts, step)
	if force, _ := step.With["force"].(bool); !force {
		return WorkflowExecutionResult{}, errors.New("dirty worktree")
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true}}, nil
}
func (e *forceRetryExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func TestWorkflowRetryStartsSecondAttemptAndKeepsHistory(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: retry\ntitle: Retry\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := view.Attempts[0]
	if _, err := svc.Fail(context.Background(), view.Run.ID, "run", first.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Retry(context.Background(), view.Run.ID, "run", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attempts) != 2 || view.Attempts[1].AttemptNumber != 2 || view.Attempts[0].Status != "failed" {
		t.Fatalf("attempts=%+v", view.Attempts)
	}
}

func TestWorkflowWorktreeRemovalCanRetryWithForce(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: cleanup
title: Cleanup
steps:
  - id: remove
    title: Remove worktree
    type: worktree.remove
    trigger: manual
    with: {expected_path: /tmp/worktree, force: true}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &forceRetryExecutor{}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.TriggerStep(context.Background(), view.Run.ID, "remove", false)
	if err != nil {
		t.Fatal(err)
	}
	if force, _ := executor.starts[0].With["force"].(bool); force {
		t.Fatalf("initial inputs = %+v, want explicit force=false", executor.starts[0].With)
	}
	if view.Steps[0].Status != "failed" {
		t.Fatalf("status = %q, want failed", view.Steps[0].Status)
	}
	view, err = svc.Retry(context.Background(), view.Run.ID, "remove", true)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || len(executor.starts) != 2 {
		t.Fatalf("view=%+v starts=%+v", view, executor.starts)
	}
	if force, _ := executor.starts[1].With["force"].(bool); !force {
		t.Fatalf("retry inputs = %+v, want force=true", executor.starts[1].With)
	}
}
