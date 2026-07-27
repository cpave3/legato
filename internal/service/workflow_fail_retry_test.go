package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowFailStopsAgentAndAllowsRetry(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: retry\ntitle: Retry\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	if err != nil {
		t.Fatal(err)
	}
	executor := &killingWorkflowExecutor{fakeWorkflowExecutor: fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := view.Attempts[0]
	if _, err := svc.Fail(context.Background(), view.Run.ID, "run", first.ID, "review failed"); err != nil {
		t.Fatal(err)
	}
	if len(executor.cancelled) != 1 || executor.cancelled[0].ID != first.ID {
		t.Fatalf("cancelled = %+v, want attempt %s", executor.cancelled, first.ID)
	}
	view, err = svc.Retry(context.Background(), view.Run.ID, "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attempts) != 2 || view.Attempts[1].AttemptNumber != 2 || view.Attempts[1].Status != "waiting_agent" {
		t.Fatalf("attempts after retry = %+v", view.Attempts)
	}
}
