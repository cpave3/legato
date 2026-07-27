package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowBackingTaskUsesWorkflowWorkspaceAndMovesToDone(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: inspect
title: Inspect task
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with: {prompt: inspect}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	view, err := NewWorkflowService(db, []WorkflowExecutor{executor}).Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Done" {
		t.Fatalf("status = %q", task.Status)
	}
	if task.WorkspaceID == nil {
		t.Fatal("workflow task has no workspace")
	}
	workspace, err := db.GetWorkspace(context.Background(), *task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Name != "Workflows" {
		t.Fatalf("workspace = %q", workspace.Name)
	}
}

func TestWorkflowBackingTaskPreservesExplicitWorkspace(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspaceID, err := db.CreateWorkspace(context.Background(), store.Workspace{Name: "Reviews"})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: inspect
title: Inspect task
steps:
  - id: inspect
    title: Inspect
    type: agent.run
    with: {prompt: inspect}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	view, err := NewWorkflowService(db, []WorkflowExecutor{executor}).Start(context.Background(), definition, nil, &workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkspaceID == nil || *task.WorkspaceID != workspaceID {
		t.Fatalf("workspace = %v", task.WorkspaceID)
	}
	if task.Status != "Backlog" {
		t.Fatalf("status = %q", task.Status)
	}
}
