package handofftest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
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
