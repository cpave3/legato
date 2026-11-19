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

func triggerSwitchFixture(t *testing.T, enabled string) (*store.Store, *WorkflowTriggerCoordinator, BoardService) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for i, name := range []string{"Backlog", "Triage"} {
		if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"T-1", "T-2", "T-3"} {
		if err := db.CreateTask(ctx, store.Task{ID: id, Title: id, Status: "Backlog"}); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	definition := `schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Triage
` + enabled + `    inputs: {task: "${{ event.task }}"}
steps:
  - {id: close, title: Close agent, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`
	if err := os.WriteFile(filepath.Join(dir, "triage.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	return db, coordinator, NewBoardService(db, events.New())
}

func moveAndCapture(t *testing.T, board BoardService, coordinator *WorkflowTriggerCoordinator, taskID string) {
	t.Helper()
	if err := board.MoveCard(context.Background(), taskID, "Triage"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
}

func requestCount(t *testing.T, db *store.Store) int {
	t.Helper()
	n := 0
	for _, status := range []string{"pending", "starting", "started", "skipped", "failed", "waiting_input", "waiting_confirmation"} {
		list, err := db.ListWorkflowTriggerRequests(context.Background(), status)
		if err != nil {
			t.Fatal(err)
		}
		n += len(list)
	}
	return n
}

func TestAPausedTriggerMakesNoRequest(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "    enabled: false\n")
	moveAndCapture(t, board, coordinator, "T-1")
	if n := requestCount(t, db); n != 0 {
		t.Fatalf("a trigger paused in its file made %d requests", n)
	}
	// Switched on here, it beats the file.
	if err := coordinator.SetTriggerEnabled(context.Background(), "triage", workflowengine.TriggerTaskEnteredColumn, true); err != nil {
		t.Fatal(err)
	}
	moveAndCapture(t, board, coordinator, "T-2")
	if n := requestCount(t, db); n != 1 {
		t.Fatalf("a trigger switched on here made %d requests", n)
	}
	states, err := coordinator.TriggerStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := states["triage"]
	if len(got) != 1 || !got[0].Enabled || got[0].EnabledByDefault || !got[0].Overridden {
		t.Fatalf("states = %+v", states)
	}
}

func TestATriggerOnInItsFileCanBePausedHere(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "")
	ctx := context.Background()
	if err := coordinator.SetTriggerEnabled(ctx, "triage", workflowengine.TriggerTaskEnteredColumn, false); err != nil {
		t.Fatal(err)
	}
	moveAndCapture(t, board, coordinator, "T-1")
	if n := requestCount(t, db); n != 0 {
		t.Fatalf("a trigger paused here made %d requests", n)
	}
	states, _ := coordinator.TriggerStates(ctx)
	if got := states["triage"]; len(got) != 1 || got[0].Enabled || !got[0].EnabledByDefault {
		t.Fatalf("states = %+v", states)
	}
	// Back to the file's default.
	if err := coordinator.ResetTrigger(ctx, "triage", workflowengine.TriggerTaskEnteredColumn); err != nil {
		t.Fatal(err)
	}
	moveAndCapture(t, board, coordinator, "T-3")
	if n := requestCount(t, db); n != 1 {
		t.Fatalf("after reset: %d requests", n)
	}
}

func TestSwitchingATriggerThatDoesNotExistIsRefused(t *testing.T) {
	_, coordinator, _ := triggerSwitchFixture(t, "")
	ctx := context.Background()
	for _, c := range [][2]string{{"nope", workflowengine.TriggerTaskEnteredColumn}, {"triage", "schedule"}} {
		if err := coordinator.SetTriggerEnabled(ctx, c[0], c[1], true); err == nil || !strings.Contains(err.Error(), "no") {
			t.Fatalf("%v: %v", c, err)
		}
	}
}
