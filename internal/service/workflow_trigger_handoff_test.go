package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// handedOffTrigger moves a card handed off from a main ("workstation") into
// Done, where a workflow with extra trigger lines waits, and returns the
// trigger requests and how many runs started.
func handedOffTrigger(t *testing.T, extra string) ([]store.WorkflowTriggerRequest, int) {
	t.Helper()
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for i, name := range []string{"Doing", "Done"} {
		if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	task := store.Task{ID: "card-1", Title: "Fix it", Status: "Doing"}
	h := store.Handoff{MainInstanceID: "srv-0123456789abcdef", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T-1", TaskID: "card-1",
		Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", Branch: "legato/t-1", State: store.HandoffInProgress,
		RequestHash: "x", CreatedAt: "2026-11-17T09:00:00Z", UpdatedAt: "2026-11-17T09:00:00Z"}
	if err := db.CreateHandoffCard(ctx, task, nil, h); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "on-done.yaml"), []byte(`schema_version: 1
name: on-done
title: On done
inputs: {task: {type: task, required: true}}
on:
  task.entered_column:
    column: Done
    inputs: {task: "${{ event.task }}"}
`+extra+`steps:
  - {id: note, title: Note, type: task.note, with: {task: "${{ workflow.inputs.task }}"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "task.note", result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	if err := NewBoardService(db, events.New()).MoveCard(ctx, "card-1", "Done"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(ctx, "card-1"); err != nil {
		t.Fatal(err)
	}
	requests, err := db.ListWorkflowTriggerRequests(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	return requests, len(executor.starts)
}

func TestAWorkersTriggerSkipsACardHandedOffFromAMain(t *testing.T) {
	requests, starts := handedOffTrigger(t, "")
	if len(requests) != 1 || requests[0].Status != "skipped" || starts != 0 {
		t.Fatalf("requests=%+v starts=%d", requests, starts)
	}
	if !strings.Contains(requests[0].Error, "handed off from workstation") || !strings.Contains(requests[0].Error, "handed_off: true") {
		t.Fatalf("why skipped: %q", requests[0].Error)
	}
}

func TestATriggerThatOptsInRunsOnAHandedOffCard(t *testing.T) {
	requests, starts := handedOffTrigger(t, "    handed_off: true\n")
	if len(requests) != 1 || requests[0].Status != "started" || starts != 1 {
		t.Fatalf("requests=%+v starts=%d", requests, starts)
	}
}
