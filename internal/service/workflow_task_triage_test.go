package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestConcurrentTaskTriageRunsPublishTheirOwnReports(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, task := range []store.Task{
		{ID: "task-1", Title: "Broken export", DescriptionMD: "Export hangs", Status: "Backlog"},
		{ID: "task-2", Title: "Missing import", DescriptionMD: "Import is absent", Status: "Backlog"},
	} {
		if err := db.CreateTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
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
      prompt: Triage ${{ workflow.inputs.task.title }} to ${{ outputs.report.path }}
      working_dir: `+working+`
    outputs:
      report: {type: file, path: "task-triage-${{ workflow.inputs.task.id }}.md"}
  - id: note
    title: Add triage note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body_path: ${{ steps.triage.outputs.report.path }}
    outputs:
      note_id: {type: string}
      task_id: {type: string}
  - id: cleanup
    title: Remove triage report
    type: file.remove
    with:
      working_dir: `+working+`
      path: "task-triage-${{ workflow.inputs.task.id }}.md"
`))
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", WorkingDir: working}}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agent, NewTaskNoteExecutor(db, nil), NewFileRemoveExecutor()}, filepath.Join(t.TempDir(), "artifacts"))
	runs := map[string]*store.WorkflowRunView{}
	for _, taskID := range []string{"task-1", "task-2"} {
		view, err := svc.Start(context.Background(), definition, map[string]string{"task": taskID})
		if err != nil {
			t.Fatal(err)
		}
		runs[taskID] = view
	}

	reports := map[string]string{
		"task-1": "## Export solution\n\nAdd a bounded context.",
		"task-2": "## Import solution\n\nRestore the import.",
	}
	for _, taskID := range []string{"task-2", "task-1"} {
		filename := "task-triage-" + taskID + ".md"
		reportPath := filepath.Join(working, filename)
		if err := os.WriteFile(reportPath, []byte(reports[taskID]), 0o600); err != nil {
			t.Fatal(err)
		}
		view := runs[taskID]
		attempt := view.Attempts[0]
		view, err = svc.Complete(context.Background(), view.Run.ID, "triage", attempt.ID, nil, map[string]string{"report": filename})
		if err != nil {
			t.Fatal(err)
		}
		if view.Run.Status != "succeeded" || len(view.Artifacts) != 1 || view.Artifacts[0].MediaType != "text/markdown" {
			t.Fatalf("run = %+v artifacts = %#v", view.Run, view.Artifacts)
		}
		if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
			t.Fatalf("source report still exists: %v", err)
		}
	}

	for taskID, want := range reports {
		notes, err := db.ListTaskNotes(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if len(notes) != 1 || notes[0].Body != want {
			t.Fatalf("task %s notes = %#v", taskID, notes)
		}
	}
}
