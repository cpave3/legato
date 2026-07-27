package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestTaskTriageWorkflowAddsAgentReportAsTaskNote(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Broken export", DescriptionMD: "Export hangs", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	working := t.TempDir()
	definition, err := workflowengine.Parse("triage.yaml", []byte(`
schema_version: 1
name: triage-task
title: Triage task
inputs:
  task: {type: task, required: true}
steps:
  - id: triage
    title: Triage
    type: agent.run
    with:
      prompt: Triage ${{ workflow.inputs.task.title }}
      working_dir: `+working+`
    outputs:
      report: {type: file, path: triage.md, media_type: text/markdown}
  - id: note
    title: Add triage note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body_path: ${{ steps.triage.outputs.report.path }}
    outputs:
      note_id: {type: string}
      task_id: {type: string}
`))
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", WorkingDir: working}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agent, NewTaskNoteExecutor(db, nil)}, filepath.Join(t.TempDir(), "artifacts"))
	view, err := svc.Start(context.Background(), definition, map[string]string{"task": "task-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, "triage.md"), []byte("## Solution\n\nAdd a bounded context."), 0600); err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	view, err = svc.Complete(context.Background(), view.Run.ID, "triage", attempt.ID, nil, map[string]string{"report": "triage.md"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %+v", view.Run)
	}
	notes, err := db.ListTaskNotes(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "## Solution\n\nAdd a bounded context." {
		t.Fatalf("notes = %#v", notes)
	}
}
