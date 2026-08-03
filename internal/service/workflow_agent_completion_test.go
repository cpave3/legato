package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type killingWorkflowExecutor struct {
	fakeWorkflowExecutor
	store                    *store.Store
	cancelled                []store.WorkflowStepAttempt
	finished                 []store.WorkflowStepAttempt
	cancelObservedStepStatus string
	cancelObservedStarts     int
}

func (e *killingWorkflowExecutor) Cancel(_ context.Context, a store.WorkflowStepAttempt) error {
	e.cancelled = append(e.cancelled, a)
	return nil
}
func (e *killingWorkflowExecutor) Finish(ctx context.Context, a store.WorkflowStepAttempt) error {
	e.finished = append(e.finished, a)
	view, _ := e.store.GetWorkflowRun(ctx, a.RunID)
	for _, step := range view.Steps {
		if step.StepID == a.StepID {
			e.cancelObservedStepStatus = step.Status
		}
	}
	e.cancelObservedStarts = len(e.starts)
	return nil
}

func TestWorkflowCompleteStopsAgentBeforeNextSharedTaskStep(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: chain\ntitle: Chain\nsteps:\n  - id: one\n    title: One\n    type: agent.run\n    with: {prompt: one}\n  - id: two\n    title: Two\n    type: agent.run\n    with: {prompt: two}\n"))
	executor := &killingWorkflowExecutor{store: db, fakeWorkflowExecutor: fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{executor}, t.TempDir())
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Complete(context.Background(), view.Run.ID, "one", view.Attempts[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.finished) != 1 || len(executor.starts) != 2 {
		t.Fatalf("finished=%d starts=%d", len(executor.finished), len(executor.starts))
	}
	if executor.cancelObservedStepStatus != "succeeded" || executor.cancelObservedStarts != 2 {
		t.Fatalf("cancel observed status=%q starts=%d", executor.cancelObservedStepStatus, executor.cancelObservedStarts)
	}
}
