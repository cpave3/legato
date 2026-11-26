package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// LEG-652: a workflow that imports a ticket names the card by the text of
// ticket.import's task output, and the board steps after it act on that card.
func TestBoardStepsActOnACardATicketImportMade(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	ctx := context.Background()
	createWorkflowMoveTask(t, db, "LEG-651", "Triage", 0)

	definition, err := workflowengine.Parse("import.yaml", []byte(`
schema_version: 2
name: import
title: Import a ticket
steps:
  - id: card
    title: Import
    type: ticket.import
    with: {key: LEG-651, provider: burnlite}
  - id: mark
    title: Mark it
    type: task.metadata.set
    with:
      task: ${{ steps.card.outputs.task }}
      values: {design.proposed: true}
  - id: read
    title: Read it
    type: task.context
    with:
      task: ${{ steps.card.outputs.task }}
  - id: ready
    title: Move it
    type: task.move
    with:
      task: ${{ steps.card.outputs.task }}
      column: Ready
`))
	if err != nil {
		t.Fatal(err)
	}
	board := NewBoardService(db, events.New())
	imported := &fakeWorkflowExecutor{typeName: "ticket.import", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"task": "LEG-651"}}}
	view, err := NewWorkflowService(db, []WorkflowExecutor{
		imported,
		NewTaskMetadataExecutor(db),
		NewTaskContextExecutor(board),
		NewTaskMoveExecutor(NewWorkflowTaskMover(board, nil)),
	}).Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %+v", view.Run)
	}
	task, err := db.GetTask(ctx, "LEG-651")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Ready" {
		t.Fatalf("status = %q", task.Status)
	}
	metadata, err := store.ParseTaskMetadata(task.Metadata)
	design, _ := metadata["design"].(map[string]any)
	if err != nil || design["proposed"] != true {
		t.Fatalf("metadata = %#v, err = %v", metadata, err)
	}
}

// The board steps take their task as a task input or as a task ID as text,
// as task.note does, and refuse anything else.
func TestBoardStepsTakeATaskInputOrATaskID(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	ctx := context.Background()
	createWorkflowMoveTask(t, db, "LEG-651", "Triage", 0)
	board := NewBoardService(db, events.New())
	steps := map[string]struct {
		executor WorkflowExecutor
		with     map[string]any
	}{
		"task.metadata.set": {NewTaskMetadataExecutor(db), map[string]any{"values": map[string]any{"seen": true}}},
		"task.move":         {NewTaskMoveExecutor(NewWorkflowTaskMover(board, nil)), map[string]any{"column": "Ready"}},
		"task.context":      {NewTaskContextExecutor(board), map[string]any{}},
		"task.working_dir.resolve": {NewTaskWorkingDirResolveExecutor(db), map[string]any{
			"fallback": "/projects/legato",
		}},
	}
	tasks := []struct {
		name    string
		task    any
		present bool
		want    string // the error, or "" when the step succeeds
	}{
		{"task input", map[string]any{"id": "LEG-651"}, true, ""},
		{"task ID", "LEG-651", true, ""},
		{"task ID with spaces", "  LEG-651 ", true, ""},
		{"empty task ID", "", true, "task id is required"},
		{"task input without an id", map[string]any{}, true, "task id is required"},
		{"no task", nil, false, "task must be a task input or a task ID"},
		{"a number", 42, true, "task must be a task input or a task ID"},
	}
	for typeName, step := range steps {
		for _, tc := range tasks {
			t.Run(typeName+"/"+tc.name, func(t *testing.T) {
				with := map[string]any{}
				for k, v := range step.with {
					with[k] = v
				}
				if tc.present {
					with["task"] = tc.task
				}
				_, err := step.executor.Start(ctx, ResolvedWorkflowStep{With: with})
				switch {
				case tc.want == "" && err != nil:
					t.Fatalf("Start: %v", err)
				case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
					t.Fatalf("err = %v, want %q", err, tc.want)
				}
			})
		}
	}
}
