package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowStartResolvesRunTitleFromExpandedInputs(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	metadata := `{"pull_request":{"number":42}}`
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-42", Title: "Review checkout", Status: "Backlog", Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("review.yaml", []byte(`
schema_version: 1
name: review-pr
title: Review pull request
run_title: "Review PR #${{ workflow.inputs.card.metadata.pull_request.number }}: ${{ workflow.inputs.card.title }}"
inputs:
  card: {type: task, required: true}
steps:
  - id: review
    title: Review
    type: agent.run
    with: {prompt: review}
`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}

	view, err := NewWorkflowService(db, []WorkflowExecutor{executor}).Start(context.Background(), definition, map[string]string{"card": "task-42"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	const want = "Review PR #42: Review checkout"
	if view.Run.WorkflowTitle != want {
		t.Fatalf("workflow title = %q, want %q", view.Run.WorkflowTitle, want)
	}
	generated, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if generated.Title != want {
		t.Fatalf("execution task title = %q, want %q", generated.Title, want)
	}
}
