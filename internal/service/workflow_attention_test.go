package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowAttentionCountIncludesRunsBeyondListPage(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	ctx := context.Background()
	for i := 0; i < 101; i++ {
		runID := fmt.Sprintf("run-%03d", i)
		run := store.WorkflowRun{ID: runID, WorkflowName: "manual", WorkflowTitle: "Manual", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "waiting", CurrentStepID: "approve"}
		steps := []store.WorkflowStepRun{{RunID: runID, StepID: "approve", StepOrder: 0, Title: "Approve", ExecutorType: "manual", DefinitionJSON: `{}`, Status: "waiting_manual"}}
		if err := db.CreateWorkflowRun(ctx, run, steps); err != nil {
			t.Fatal(err)
		}
	}
	count, err := NewWorkflowService(db, nil).AttentionCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 101 {
		t.Fatalf("count = %d, want 101", count)
	}
}

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
	view, err := svc.Start(context.Background(), definition, nil)
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

func TestWorkflowShowFlagsAgentErrorAsNeedingAttention(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	definition, _ := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: attention-error\ntitle: Attention\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	executor := &promptingWorkflowExecutor{fakeWorkflowExecutor: fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", AgentSession: &store.AgentSession{ID: 7, TaskID: "placeholder", TmuxSession: "legato-placeholder", AgentKind: "chimera", Status: "running"}}}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(context.Background(), store.AgentSession{TaskID: view.Run.TaskID, TmuxSession: "legato-" + view.Run.TaskID, AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(context.Background(), view.Run.TaskID, store.ActivityError); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Show(context.Background(), view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Attempts[0].NeedsAttention || view.Attempts[0].AgentActivity != store.ActivityError || view.Attempts[0].AttentionReason != "agent error before step completion" {
		t.Fatalf("attempt=%+v", view.Attempts[0])
	}
}

func TestWorkflowCompletionRemainsInHistoryAndOutOfAttention(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	definition, err := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: review\ntitle: Review PR\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "succeeded"}}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Show(context.Background(), view.Run.ID)
	if err != nil || completed.Run.Status != "succeeded" {
		t.Fatalf("completed run = %+v, err = %v", completed, err)
	}
	items, err := NewWorkflowAttentionAdapter(svc).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Navigation.Target == view.Run.ID {
			t.Fatalf("successful run leaked into Attention: %+v", item)
		}
	}
}

func TestNotifyOnCompleteIsRejectedByWorkflowSchema(t *testing.T) {
	_, err := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: review\nnotify_on_complete: true\ntitle: Review PR\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	if err == nil {
		t.Fatal("notify_on_complete remains accepted")
	}
}
