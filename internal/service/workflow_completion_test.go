package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowCompleteAdvancesAgentOutputIntoNextStep(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	workDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(workDir, "review.md"), []byte("# Review"), 0o600)
	definition, err := workflowengine.Parse("test.yaml", []byte(`
schema_version: 1
name: chain
title: Chain
steps:
  - id: review
    title: Review
    type: agent.run
    with: {prompt: review}
    outputs:
      review: {type: file, path: review.md, media_type: text/markdown}
      findings: {type: json}
  - id: summarize
    title: Summarize
    type: agent.run
    with:
      prompt: "Summarize ${{ steps.review.outputs.findings }}"
`))
	if err != nil {
		t.Fatal(err)
	}
	first := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", WorkingDir: workDir}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{first}, t.TempDir())
	view, err := svc.Start(context.Background(), definition, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	view, err = svc.Complete(context.Background(), view.Run.ID, "review", attempt.ID, map[string]string{"findings": `[{"severity":"high"}]`}, map[string]string{"review": "review.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.starts) != 2 || first.starts[1].With["prompt"] != `Summarize [{"severity":"high"}]` || view.Run.CurrentStepID != "summarize" {
		t.Fatalf("starts=%+v run=%+v", first.starts, view.Run)
	}
	view, err = svc.Complete(context.Background(), view.Run.ID, "summarize", view.Attempts[1].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || task.Status != "Done" {
		t.Fatalf("run=%+v task=%+v", view.Run, task)
	}
}
