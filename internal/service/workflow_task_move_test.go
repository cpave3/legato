package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowMovesSubjectThroughBoardService(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	ctx := context.Background()
	bus := events.New()
	moved := bus.Subscribe(events.EventCardMoved)
	defer bus.Unsubscribe(moved)
	createWorkflowMoveTask(t, db, "ready-1", "Ready", 4)
	createWorkflowMoveTask(t, db, "subject-1", "Triage", 0)

	definition, err := workflowengine.Parse("move.yaml", []byte(`
schema_version: 1
name: move
title: Move task
inputs:
  task: {type: task, required: true}
steps:
  - id: prepare
    title: Prepare
    type: task.metadata.set
    with: {}
  - id: ready
    title: Move to Ready
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
      column: Ready
`))
	if err != nil {
		t.Fatal(err)
	}
	prepare := &fakeWorkflowExecutor{typeName: "task.metadata.set", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}}
	move := NewTaskMoveExecutor(NewWorkflowTaskMover(NewBoardService(db, bus), nil))
	view, err := NewWorkflowService(db, []WorkflowExecutor{prepare, move}).Start(ctx, definition, map[string]string{"task": "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %+v", view.Run)
	}
	task, _ := db.GetTask(ctx, "subject-1")
	if task.Status != "Ready" || task.SortOrder != 5 {
		t.Fatalf("task = %+v", task)
	}
	transition, err := db.LatestTaskTransition(ctx, "subject-1")
	if err != nil || transition.FromColumn != "Triage" || transition.ToColumn != "Ready" {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	select {
	case event := <-moved:
		payload := event.Payload.(map[string]string)
		if payload["id"] != "subject-1" || payload["to"] != "Ready" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("card moved event was not published")
	}
}

type recordingCardMoveSync struct {
	calls [][2]string
	err   error
}

func (s *recordingCardMoveSync) PushMove(_ context.Context, taskID, column string) error {
	s.calls = append(s.calls, [2]string{taskID, column})
	return s.err
}

func TestWorkflowTaskMoverPrefersConfiguredSync(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	createWorkflowMoveTask(t, db, "subject-1", "Triage", 0)
	sync := &recordingCardMoveSync{}
	mover := NewWorkflowTaskMover(NewBoardService(db, events.New()), sync)
	if err := mover.MoveTask(context.Background(), "subject-1", "Ready"); err != nil {
		t.Fatal(err)
	}
	if len(sync.calls) != 1 || sync.calls[0] != [2]string{"subject-1", "Ready"} {
		t.Fatalf("sync calls = %#v", sync.calls)
	}
	task, _ := db.GetTask(context.Background(), "subject-1")
	if task.Status != "Triage" {
		t.Fatalf("board fallback also ran: status = %q", task.Status)
	}
}

func TestWorkflowTaskMoveSameColumnIsIdempotent(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	ctx := context.Background()
	bus := events.New()
	moved := bus.Subscribe(events.EventCardMoved)
	defer bus.Unsubscribe(moved)
	createWorkflowMoveTask(t, db, "subject-1", "Ready", 2)
	executor := NewTaskMoveExecutor(NewWorkflowTaskMover(NewBoardService(db, bus), nil))
	for range 2 {
		result, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "subject-1"}, "column": "Ready"}})
		if err != nil || result.Status != "succeeded" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if _, err := db.LatestTaskTransition(ctx, "subject-1"); err != store.ErrNotFound {
		t.Fatalf("transition err = %v", err)
	}
	select {
	case event := <-moved:
		t.Fatalf("unexpected event: %+v", event)
	default:
	}
}

func TestWorkflowTaskMoveFailureIsPersistedAndRetryAfterLandingIsIdempotent(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	ctx := context.Background()
	bus := events.New()
	moved := bus.Subscribe(events.EventCardMoved)
	defer bus.Unsubscribe(moved)
	createWorkflowMoveTask(t, db, "subject-1", "Triage", 0)
	definition, err := workflowengine.Parse("move.yaml", []byte(`
schema_version: 1
name: move
title: Move
inputs:
  task: {type: task, required: true}
steps:
  - id: move
    title: Move
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
      column: Missing
`))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkflowService(db, []WorkflowExecutor{NewTaskMoveExecutor(NewWorkflowTaskMover(NewBoardService(db, bus), nil))})
	view, err := svc.Start(ctx, definition, map[string]string{"task": "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || view.Steps[0].Status != "failed" || len(view.Attempts) != 1 || !strings.Contains(view.Attempts[0].Error, `column "Missing" not found`) {
		t.Fatalf("failed run = %+v", view)
	}
	task, _ := db.GetTask(ctx, "subject-1")
	if task.Status != "Triage" {
		t.Fatalf("status after failure = %q", task.Status)
	}

	if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Missing", RemoteStatuses: "[]", SortOrder: 3}); err != nil {
		t.Fatal(err)
	}
	if err := NewBoardService(db, bus).MoveCard(ctx, "subject-1", "Missing"); err != nil {
		t.Fatal(err)
	}
	<-moved
	view, err = svc.Retry(ctx, view.Run.ID, "move", false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || len(view.Attempts) != 2 {
		t.Fatalf("retried run = %+v", view)
	}
	select {
	case event := <-moved:
		t.Fatalf("retry emitted duplicate move: %+v", event)
	default:
	}
}

func TestWorkflowTaskMoveRejectsInvalidInputsAndUnknownColumns(t *testing.T) {
	db := newWorkflowMoveStore(t)
	defer db.Close()
	createWorkflowMoveTask(t, db, "subject-1", "Triage", 0)
	createWorkflowMoveTask(t, db, "stale-1", "Stale", 0)
	executor := NewTaskMoveExecutor(NewWorkflowTaskMover(NewBoardService(db, events.New()), nil))
	for name, with := range map[string]map[string]any{
		"malformed task":       {"task": "subject-1", "column": "Ready"},
		"missing id":           {"task": map[string]any{}, "column": "Ready"},
		"empty column":         {"task": map[string]any{"id": "subject-1"}, "column": " "},
		"unknown column":       {"task": map[string]any{"id": "subject-1"}, "column": "Missing"},
		"unmapped same column": {"task": map[string]any{"id": "stale-1"}, "column": "Stale"},
		"missing task":         {"task": map[string]any{"id": "absent"}, "column": "Ready"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{With: with}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	task, _ := db.GetTask(context.Background(), "subject-1")
	if task.Status != "Triage" {
		t.Fatalf("status = %q", task.Status)
	}
}

func newWorkflowMoveStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(t.TempDir() + "/workflow-move.db")
	if err != nil {
		t.Fatal(err)
	}
	for i, column := range []string{"Triage", "Ready", "Done"} {
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: column, RemoteStatuses: "[]", SortOrder: i}); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	return db
}

func createWorkflowMoveTask(t *testing.T, db *store.Store, id, status string, order int) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: status, SortOrder: order, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}
