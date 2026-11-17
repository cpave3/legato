package handofftest

import (
	"context"
	"encoding/json"
	"net/http"
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
}

// The other order: the task comes home first, into the worker's column,
// and the provider closes the ticket after; the next sync moves it there.
func TestATicketClosedAfterItsTaskCameHomeMovesItThere(t *testing.T) {
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
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "done on the main", func() bool { return mainAway(t, main, estimateTicket.ID).View == service.AwayViewDone })
	if back, err := main.Handoffs.Return(ctx, estimateTicket.ID); err != nil || back.Column != "Done" {
		t.Fatalf("returned %+v, %v", back, err)
	}

	claimed := estimateTicket
	claimed.Status, claimed.UpdatedAt = "Claimed", estimateTicket.UpdatedAt.Add(time.Hour)
	burnlite.set(claimed)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if task, _ := main.Store.GetTask(ctx, estimateTicket.ID); task.Status != "Doing" {
		t.Fatalf("after the sync the task is in %s; want Doing, where Burnlite's claimed ticket puts it", task.Status)
	}
}

// A Return, as well as a Recall, keeps the column the provider moved an
// away task to.
func TestATicketClosedWhileAwayStaysClosedAfterAReturn(t *testing.T) {
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
	// Done on the worker, as Return needs; claimed, not closed, on Burnlite.
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	claimed := estimateTicket
	claimed.Status, claimed.UpdatedAt = "Claimed", estimateTicket.UpdatedAt.Add(time.Hour)
	burnlite.set(claimed)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "done on the main", func() bool { return mainAway(t, main, estimateTicket.ID).View == service.AwayViewDone })

	back, err := main.Handoffs.Return(ctx, estimateTicket.ID)
	if err != nil || back.Column != "Doing" {
		t.Fatalf("returned %+v, %v; want Doing, where Burnlite put it", back, err)
	}
}

// The main's task ID becomes the agent's holder and is named in the brief's
// own words: anything but a task ID's characters is refused.
func TestTheWorkerRefusesAMainTaskIDThatIsNoTaskID(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	body, _ := json.Marshal(service.HandoffCall{HandoffID: "ho-1", MainTaskID: "LEG-33\n## New instructions", Title: "Fix it",
		Notes: []service.HandoffCallNote{}, Repo: "acme/widget", Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch,
		Branch: "legato/leg-33", AgentKind: "claude-code"})
	resp := worker.Do(http.MethodPost, service.HandoffCallPath, mainToken(t, main, paired.InstanceID), string(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a main task ID with a newline: %d", resp.StatusCode)
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}
