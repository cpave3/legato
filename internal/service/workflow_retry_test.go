package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

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
	view, err = svc.Retry(context.Background(), view.Run.ID, "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attempts) != 2 || view.Attempts[1].AttemptNumber != 2 || view.Attempts[0].Status != "failed" {
		t.Fatalf("attempts=%+v", view.Attempts)
	}
}
