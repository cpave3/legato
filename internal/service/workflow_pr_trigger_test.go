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

func changesRequestedFixture(t *testing.T, trigger string) (*store.Store, *WorkflowTriggerCoordinator) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(context.Background(), store.Task{ID: "T-1", Title: "Fix", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	definition := `schema_version: 1
name: address
title: Address feedback
inputs:
  task: {type: task, required: true}
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
on:
  pr.changes_requested:
` + trigger + `    inputs:
      task: ${{ event.task }}
      repo: ${{ event.repo }}
      pr_number: ${{ event.pr_number }}
steps:
  - {id: stop, title: Stop, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`
	if err := os.WriteFile(filepath.Join(dir, "address.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	return db, coordinator
}

func review(id int64) events.PRChangesRequestedPayload {
	return events.PRChangesRequestedPayload{TaskID: "T-1", Repo: "acme/web", Number: 12, URL: "https://github.com/acme/web/pull/12", ReviewID: id, Reviewer: "alice"}
}

func TestChangesRequestedStartsTheWorkflowWithTheTaskAndPR(t *testing.T) {
	db, coordinator := changesRequestedFixture(t, "    max_runs_per_day: 2\n")
	ctx := context.Background()
	for _, id := range []int64{1, 1, 2, 3} { // review 1 twice: once
		if err := coordinator.ChangesRequested(ctx, review(id)); err != nil {
			t.Fatal(err)
		}
	}
	started := requestsWith(t, db, "started")
	if len(started) != 2 {
		t.Fatalf("started = %d, want 2", len(started))
	}
	run, _ := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	for _, want := range []string{`"id":"T-1"`, `"repo":"acme/web"`, `"pr_number":12`} {
		if !strings.Contains(run.Run.InputsJSON, want) {
			t.Fatalf("inputs %s lack %s", run.Run.InputsJSON, want)
		}
	}
	// The third review in a day for the task meets the loop stop.
	if held := requestsWith(t, db, "held"); len(held) != 1 {
		t.Fatalf("held = %d", len(held))
	}
}

func TestAPausedChangesRequestedTriggerMakesNothing(t *testing.T) {
	db, coordinator := changesRequestedFixture(t, "    enabled: false\n")
	if err := coordinator.ChangesRequested(context.Background(), review(1)); err != nil {
		t.Fatal(err)
	}
	if n := requestCount(t, db); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}
