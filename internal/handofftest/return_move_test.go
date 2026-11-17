package handofftest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// A task that comes home into another column moved there: a transition
// (source handoff) the main's workflow triggers act on, and a card.moved
// event naming it.
func TestAReturnIntoAnotherColumnIsARecordedMove(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	moved := main.Bus.Subscribe(events.EventCardMoved)
	taskID, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })

	if _, err := main.Handoffs.Return(ctx, taskID); err != nil {
		t.Fatal(err)
	}

	transition, err := main.Store.LatestTaskTransition(ctx, taskID)
	if err != nil || transition == nil {
		t.Fatalf("no transition: %v", err)
	}
	if transition.Source != "handoff" || transition.FromColumn != "Ready" || transition.ToColumn != "Done" || transition.TaskJSON == "" {
		t.Fatalf("transition %+v", transition)
	}
	select {
	case e := <-moved:
		payload, _ := e.Payload.(map[string]string)
		if payload["id"] != taskID || payload["to"] != "Done" || payload["transition_id"] != transition.ID {
			t.Fatalf("card.moved %+v", e.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no card.moved")
	}
}

func TestARecallIntoTheColumnItLeftIsNoMove(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	// A column the main does not have: the task lands in the one it left.
	if err := worker.Store.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Testing", RemoteStatuses: "[]", SortOrder: 9}); err != nil {
		t.Fatal(err)
	}
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _, _ := workedOn(t, main, worker, paired.InstanceID, "Testing")
	eventually(t, "heard on the main", func() bool { return mainAway(t, main, taskID).WorkerTaskID != "" })

	back, err := main.Handoffs.Recall(ctx, taskID)
	if err != nil || back.Column != "Ready" {
		t.Fatalf("recalled %+v, %v", back, err)
	}
	if transition, err := main.Store.LatestTaskTransition(ctx, taskID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("transition %+v, %v; want none for a task back where it was", transition, err)
	}
}

// End to end: a workflow on the main that starts when a task enters Done
// runs once when a task comes home there, with the task as it came back.
func TestAMainsWorkflowRunsWhenATaskComesHomeIntoItsColumn(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "on-done.yaml"), []byte(`schema_version: 1
name: on-done
title: On done
inputs: {task: {type: task, required: true}}
on:
  task.entered_column:
    column: Done
    inputs: {task: "${{ event.task }}"}
steps:
  - {id: note, title: Note, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: "Curated on the main."}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows := service.NewWorkflowService(main.Store, []service.WorkflowExecutor{service.NewTaskNoteExecutor(main.Store)})
	coordinator := service.NewWorkflowTriggerCoordinator(main.Store, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	t.Cleanup(coordinator.StartEventLoop(context.Background(), main.Bus))

	taskID, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })
	if _, err := main.Handoffs.Return(ctx, taskID); err != nil {
		t.Fatal(err)
	}

	curated := func() int {
		n := 0
		for _, note := range notesOf(t, main, taskID) {
			if note.Body == "Curated on the main." {
				n++
			}
		}
		return n
	}
	eventually(t, "the main's workflow ran", func() bool { return curated() == 1 })
	transition, err := main.Store.LatestTaskTransition(ctx, taskID)
	if err != nil || !strings.Contains(transition.TaskJSON, "Fix the login test's expired cookie") {
		t.Fatalf("the trigger's task: %+v, %v; want the title that came back", transition, err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := curated(); n != 1 {
		t.Fatalf("the workflow ran %d times", n)
	}
}
