package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type promptingWorkflowExecutor struct {
	fakeWorkflowExecutor
	prompted bool
}

func (e *promptingWorkflowExecutor) Prompt(context.Context, store.WorkflowStepAttempt, string) error {
	e.prompted = true
	return nil
}

func TestWorkflowShowFlagsIdleAgentAsNeedingAttention(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: attention\ntitle: Attention\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	executor := &promptingWorkflowExecutor{fakeWorkflowExecutor: fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", AgentSession: &store.AgentSession{ID: 7, TaskID: "placeholder", TmuxSession: "legato-placeholder", AgentKind: "chimera", Status: "running"}}}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(context.Background(), store.AgentSession{TaskID: view.Run.TaskID, TmuxSession: "legato-" + view.Run.TaskID, AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(context.Background(), view.Run.TaskID, ""); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Show(context.Background(), view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Attempts[0].NeedsAttention || view.Attempts[0].AgentActivity != "" || view.Attempts[0].AttentionReason != "agent idle before step completion" {
		t.Fatalf("attempt=%+v", view.Attempts[0])
	}
	if _, err := svc.PromptStep(context.Background(), view.Run.ID, "run"); err != nil {
		t.Fatal(err)
	}
	if !executor.prompted {
		t.Fatal("executor was not prompted")
	}
}
