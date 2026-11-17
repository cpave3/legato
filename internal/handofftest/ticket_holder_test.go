package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// The worker's agent claims and closes the ticket as the main's task, so a
// claim the main's agent made earlier is the same holder's.
func TestAHandedOffAgentHoldsItsTicketUnderTheMainsTaskID(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	burnlite := newFakeBurnlite(estimateTicket)
	burnliteTask(t, main, burnlite, "", "")

	if _, err := handOff(t, main, paired.InstanceID, estimateTicket.ID); err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	spawned := worker.Tmux.Spawned()[0]
	if !strings.Contains(strings.Join(spawned.Env, "\n"), "LEGATO_HOLDER=LEG-33") {
		t.Fatalf("env %v; want LEGATO_HOLDER=LEG-33", spawned.Env)
	}
	if brief := briefOf(t, spawned); !strings.Contains(brief, "as holder LEG-33") {
		t.Fatalf("the brief:\n%s", brief)
	}
}

// A ticket closed while its task is away moves the away card on the main
// (the provider's state wins); bringing the task home does not move it
// back to the worker's column.
func TestATicketClosedWhileAwayStaysClosedWhenTheTaskComesHome(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	burnlite := newFakeBurnlite(estimateTicket)
	sync := burnliteTask(t, main, burnlite, "", "")
	away, err := handOff(t, main, paired.InstanceID, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Review"); err != nil {
		t.Fatal(err)
	}

	// The worker's agent closed the ticket; the main's sync hears of it.
	closed := estimateTicket
	closed.Status, closed.UpdatedAt = "Closed", estimateTicket.UpdatedAt.Add(time.Hour)
	burnlite.set(closed)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "heard on the main", func() bool { return mainAway(t, main, estimateTicket.ID).WorkerTaskID != "" })

	back, err := main.Handoffs.Recall(ctx, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := main.Store.GetTask(ctx, estimateTicket.ID)
	if back.Column != "Done" || task.Status != "Done" {
		t.Fatalf("home in %s (card %s); want Done, where Burnlite's closed ticket put it", back.Column, task.Status)
	}
	_ = service.AwayViewDone
}
