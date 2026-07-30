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

func TestNotifyOnCompleteSurfacesAttentionItem(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()

	// Definition with notify_on_complete: true
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: review\nnotify_on_complete: true\ntitle: Review PR\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))

	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "succeeded"}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The run should have succeeded (fake executor returns succeeded immediately)
	completed, err := svc.Show(context.Background(), view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Run.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", completed.Run.Status)
	}

	// The attention adapter should surface this as a completed item
	adapter := NewWorkflowAttentionAdapter(svc)
	items, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, item := range items {
		if item.Navigation.Target == view.Run.ID {
			found = true
			if item.Reason != "Workflow succeeded: Review PR" {
				t.Errorf("reason: got %q, want %q", item.Reason, "Workflow succeeded: Review PR")
			}
			// Should have a dismiss action
			var hasDismiss bool
			for _, a := range item.Actions {
				if a.ID == "dismiss" {
					hasDismiss = true
				}
			}
			if !hasDismiss {
				t.Error("completed workflow attention item should have dismiss action")
			}
		}
	}
	if !found {
		t.Fatal("notify_on_complete run should appear in attention snapshot")
	}
}

func TestNotifyOnCompleteFalseDoesNotSurface(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()

	// Definition without notify_on_complete
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: review\ntitle: Review PR\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))

	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "succeeded"}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewWorkflowAttentionAdapter(svc)
	items, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range items {
		if item.Navigation.Target == view.Run.ID {
			t.Fatal("run without notify_on_complete should NOT appear in attention snapshot")
		}
	}
}
