package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowTaskInputResolvesCurrentTaskContext(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, remoteID := "jira", "LEG-42"
	metadata := `{"triaged":false,"repository":{"name":"legato"}}`
	if err := db.CreateTask(context.Background(), store.Task{ID: "LEG-42", Title: "Investigate timeout", Description: "Connection times out", DescriptionMD: "Connection **times out**", Status: "Backlog", Priority: "High", Provider: &provider, RemoteID: &remoteID, Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: triage
title: Triage task
inputs:
  task:
    type: task
    required: true
steps:
  - id: inspect
    title: Inspect task
    type: agent.run
    with:
      task: ${{ workflow.inputs.task }}
      prompt: "Triage ${{ workflow.inputs.task.title }} from ${{ workflow.inputs.task.metadata.repository.name }}: ${{ workflow.inputs.task.description }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run"}
	svc := NewWorkflowService(db, []WorkflowExecutor{executor})
	view, err := svc.Start(context.Background(), definition, map[string]string{"task": "LEG-42"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.RelatedTaskID != "LEG-42" {
		t.Fatalf("related task = %q", view.Run.RelatedTaskID)
	}
	if len(executor.starts) != 1 {
		t.Fatalf("starts = %d", len(executor.starts))
	}
	got := executor.starts[0].With["task"].(map[string]any)
	if got["id"] != "LEG-42" || got["provider"] != "jira" || got["description"] != "Connection **times out**" {
		t.Fatalf("task context = %#v", got)
	}
	if executor.starts[0].With["prompt"] != "Triage Investigate timeout from legato: Connection **times out**" {
		t.Fatalf("prompt = %q", executor.starts[0].With["prompt"])
	}
}
