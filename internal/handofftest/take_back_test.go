package handofftest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Take back, Drop, removing a worker and unpairing a main (LEG-26): the
// ways out when a worker is gone or no longer wanted.

// homeWithoutResults checks a task came back without its worker's results:
// not away, in the column it left, with the main's note that says so.
func homeWithoutResults(t *testing.T, main *Main, taskID, column string) {
	t.Helper()
	ctx := context.Background()
	if away, err := main.Handoffs.Away(ctx, taskID); err != nil || away != nil {
		t.Fatalf("still away: %+v, %v", away, err)
	}
	if err := service.RefuseAway(ctx, main.Store, taskID); err != nil {
		t.Fatalf("the task is still refused: %v", err)
	}
	task, err := main.Store.GetTask(ctx, taskID)
	if err != nil || task.Status != column || task.ArchivedAt != nil {
		t.Fatalf("task %+v, %v", task, err)
	}
	found := false
	for _, n := range notesOf(t, main, taskID) {
		if strings.HasPrefix(n.Body, "Taken back from vm-1 without its results") {
			found = true
		}
		if n.Source == service.NoteSourceWorker {
			t.Fatalf("a worker's note came back with a take back: %+v", n)
		}
	}
	if !found {
		t.Fatalf("no take back note: %+v", notesOf(t, main, taskID))
	}
}

// droppedOnWorker checks the worker stopped the agent, closed the handoff
// and archived the card with a note that the main took it back.
func droppedOnWorker(t *testing.T, worker *Worker, main *Main, away service.AwayTask) {
	t.Helper()
	archivedOnWorker(t, worker, main, away)
	notes, err := worker.Store.ListTaskNotes(context.Background(), away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if strings.Contains(n.Body, "workstation took this task back") {
			return
		}
	}
	t.Fatalf("no drop note on the worker's card: %+v", notes)
}

// unreachable stops the worker and moves the clock past the grace, so the
// main shows its away tasks unreachable.
func unreachable(t *testing.T, main *Main, worker *Worker, workerID, taskID string) {
	t.Helper()
	worker.Stop()
	eventually(t, "the drop", func() bool { return !main.Connections.Connected(workerID) })
	worker.Clock.Advance(31 * time.Second)
	main.Connections.CheckReach(context.Background())
	if got := mainAway(t, main, taskID); got.View != service.AwayViewUnreachable {
		t.Fatalf("not unreachable: %+v", got)
	}
}

func TestTakingBackATaskFromAnUnreachableWorkerDropsItOnTheNextConnect(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })

	// A reachable worker's task is recalled, not taken back.
	if _, err := main.Handoffs.TakeBack(ctx, taskID); !errors.Is(err, service.ErrReturnRefused) {
		t.Fatalf("taking back a task its worker can return: %v", err)
	}
	unreachable(t, main, worker, paired.InstanceID, taskID)

	back, err := main.Handoffs.TakeBack(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if back.TaskID != taskID || back.Column != "Ready" || !back.TakenBack {
		t.Fatalf("taken back %+v", back)
	}
	homeWithoutResults(t, main, taskID, "Ready")
	// Taking it back again finds nothing away.
	if _, err := main.Handoffs.TakeBack(ctx, taskID); !errors.Is(err, service.ErrReturnRefused) {
		t.Fatalf("taking back a task at home: %v", err)
	}
	// The Drop is owed, and keeps the worker's connection wanted.
	if owes, err := main.Handoffs.Owes(ctx, paired.InstanceID); err != nil || !owes {
		t.Fatalf("owes %v, %v", owes, err)
	}

	// The worker comes back: the main sends the Drop on connect.
	worker.Restart()
	eventually(t, "the drop on the worker", func() bool {
		h := workerHandoff(t, worker, main, away.HandoffID)
		return h.ClosedAt != nil
	})
	droppedOnWorker(t, worker, main, away)
	eventually(t, "nothing owed", func() bool {
		owes, err := main.Handoffs.Owes(ctx, paired.InstanceID)
		return err == nil && !owes
	})
	// The worker stays paired; a new handoff is a new card.
	again, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if again.WorkerTaskID == away.WorkerTaskID || again.HandoffID == away.HandoffID {
		t.Fatalf("handed off again as %+v", again)
	}
}

func TestTakingBackALostTaskEndsItsAwayState(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Gone")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })
	if err := worker.Store.DeleteTask(ctx, away.WorkerTaskID); err != nil {
		t.Fatal(err)
	}
	worker.Bus.Publish(events.Event{Type: events.EventCardsRefreshed})
	eventually(t, "lost", func() bool { return mainAway(t, main, taskID).View == service.AwayViewLost })

	if _, err := main.Handoffs.TakeBack(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	homeWithoutResults(t, main, taskID, "Ready")
	// The worker has nothing to drop: the Drop is done once it says so.
	eventually(t, "nothing owed", func() bool {
		owes, err := main.Handoffs.Owes(ctx, paired.InstanceID)
		return err == nil && !owes
	})
}

func TestADropOwedToAWorkerThatRefusesTheMainStaysOwed(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	if err := worker.Tokens.RevokeMain(ctx, main.InstanceID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "not accepted", func() bool { return mainAway(t, main, taskID).View == service.AwayViewNotAccepted })
	if _, err := main.Handoffs.TakeBack(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	homeWithoutResults(t, main, taskID, "Ready")
	time.Sleep(100 * time.Millisecond)
	if owes, err := main.Handoffs.Owes(ctx, paired.InstanceID); err != nil || !owes {
		t.Fatalf("owes %v, %v", owes, err)
	}
}

// workerToken says whether the worker still accepts the main's token.
func workerAccepts(t *testing.T, worker *Worker, main *Main) bool {
	t.Helper()
	mains, err := worker.Tokens.ListMains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mains {
		if m.InstanceID == main.InstanceID() {
			return true
		}
	}
	return false
}

// forgotten says whether the main has forgotten the worker: no record,
// hidden or not, and no token.
func forgotten(t *testing.T, main *Main, workerID string) bool {
	t.Helper()
	ctx := context.Background()
	if _, err := main.Store.GetWorker(ctx, workerID); !errors.Is(err, store.ErrNotFound) {
		return false
	}
	secrets, err := main.Secrets.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return len(secrets) == 0
}

func TestRemovingAReachableWorkerRecallsWhatItCanTakesBackTheRestAndLeaves(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	working, workingAway, note := workedOn(t, main, worker, paired.InstanceID, "Doing")
	lostID, lost := awayOn(t, main, worker, paired.InstanceID, "Gone")
	eventually(t, "rows", func() bool { return mainAway(t, main, lostID).View == service.AwayViewIdle })
	if err := worker.Store.DeleteTask(ctx, lost.WorkerTaskID); err != nil {
		t.Fatal(err)
	}
	worker.Bus.Publish(events.Event{Type: events.EventCardsRefreshed})
	eventually(t, "lost", func() bool { return mainAway(t, main, lostID).View == service.AwayViewLost })
	if w := workerRecord(t, main, paired.InstanceID); w.Away != 2 {
		t.Fatalf("the worker holds %d away tasks, want 2", w.Away)
	}

	removed, err := main.Handoffs.RemoveWorker(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Recalled) != 1 || removed.Recalled[0].TaskID != working || len(removed.TakenBack) != 1 || removed.TakenBack[0].TaskID != lostID || !removed.Left {
		t.Fatalf("removed %+v", removed)
	}
	returnedHome(t, main, working, "Doing", note)
	archivedOnWorker(t, worker, main, workingAway)
	homeWithoutResults(t, main, lostID, "Ready")
	if workerAccepts(t, worker, main) {
		t.Fatal("the worker still accepts the main")
	}
	if !forgotten(t, main, paired.InstanceID) {
		t.Fatal("the main did not forget the worker")
	}
	if workers, _ := main.Workers.List(ctx); len(workers) != 0 {
		t.Fatalf("still listed: %+v", workers)
	}
	if _, err := main.Handoffs.RemoveWorker(ctx, paired.InstanceID); !errors.Is(err, service.ErrWorkerNotPaired) {
		t.Fatalf("removing it again: %v", err)
	}
}

func TestRemovingAnUnreachableWorkerKeepsItHiddenToSendDropAndLeave(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })
	unreachable(t, main, worker, paired.InstanceID, taskID)

	removed, err := main.Handoffs.RemoveWorker(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Recalled) != 0 || len(removed.TakenBack) != 1 || removed.Left {
		t.Fatalf("removed %+v", removed)
	}
	homeWithoutResults(t, main, taskID, "Ready")
	// Hidden: no longer listed, and nothing more can be handed to it.
	if workers, _ := main.Workers.List(ctx); len(workers) != 0 {
		t.Fatalf("still listed: %+v", workers)
	}
	other, _ := localTask(t, main, "Another", "", "")
	if _, err := handOff(t, main, paired.InstanceID, other); !errors.Is(err, service.ErrWorkerNotPaired) {
		t.Fatalf("handing off to a removed worker: %v", err)
	}
	if forgotten(t, main, paired.InstanceID) {
		t.Fatal("forgotten before Leave was sent")
	}

	// It comes back within the 30 days: Drop, then Leave, then forgotten.
	worker.Clock.Advance(24 * time.Hour)
	worker.Restart()
	eventually(t, "forgotten", func() bool { return forgotten(t, main, paired.InstanceID) })
	droppedOnWorker(t, worker, main, away)
	if workerAccepts(t, worker, main) {
		t.Fatal("the worker still accepts the main")
	}
}

func TestARemovedWorkerUnreachableFor30DaysIsForgotten(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })
	unreachable(t, main, worker, paired.InstanceID, taskID)
	if _, err := main.Handoffs.RemoveWorker(ctx, paired.InstanceID); err != nil {
		t.Fatal(err)
	}

	worker.Clock.Advance(29 * 24 * time.Hour)
	time.Sleep(100 * time.Millisecond)
	if forgotten(t, main, paired.InstanceID) {
		t.Fatal("forgotten within the 30 days")
	}
	worker.Clock.Advance(2 * 24 * time.Hour)
	eventually(t, "forgotten", func() bool { return forgotten(t, main, paired.InstanceID) })
	if owes, err := main.Handoffs.Owes(ctx, paired.InstanceID); err != nil || owes {
		t.Fatalf("still owes %v, %v", owes, err)
	}

	// Back after that: the main sends it nothing.
	worker.Restart()
	time.Sleep(300 * time.Millisecond)
	if h := workerHandoff(t, worker, main, away.HandoffID); h.ClosedAt != nil {
		t.Fatalf("dropped after the main forgot the worker: %+v", h)
	}
	if !workerAccepts(t, worker, main) {
		t.Fatal("left after the main forgot the worker")
	}
}

func TestUnpairingAMainOnTheWorkerLeavesItsCardsAsOrdinaryCards(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	firstID, first := awayOn(t, main, worker, paired.InstanceID, "First")
	_, second := awayOn(t, main, worker, paired.InstanceID, "Second")
	mains, err := worker.Tokens.ListMains(ctx)
	if err != nil || len(mains) != 1 || mains[0].OpenHandoffs != 2 {
		t.Fatalf("mains %+v, %v", mains, err)
	}

	if resp := worker.Do(http.MethodDelete, "/api/settings/mains/"+main.InstanceID(), worker.WebToken(), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("unpair: %d", resp.StatusCode)
	}
	if workerAccepts(t, worker, main) {
		t.Fatal("the worker still accepts the main")
	}
	for _, a := range []service.AwayTask{first, second} {
		h := workerHandoff(t, worker, main, a.HandoffID)
		if h.ClosedAt == nil {
			t.Fatalf("handoff open: %+v", h)
		}
		task, err := worker.Store.GetTask(ctx, a.WorkerTaskID)
		if err != nil || task.ArchivedAt != nil {
			t.Fatalf("card %+v, %v", task, err)
		}
		if _, err := worker.Store.GetAgentSessionByTaskID(ctx, a.WorkerTaskID); err != nil {
			t.Fatalf("its agent stopped: %v", err)
		}
		notes, _ := worker.Store.ListTaskNotes(ctx, a.WorkerTaskID)
		found := false
		for _, n := range notes {
			found = found || strings.Contains(n.Body, "workstation was unpaired")
		}
		if !found {
			t.Fatalf("no unpair note: %+v", notes)
		}
		// An ordinary card: it can be archived now.
		if err := service.RefuseHandedOff(ctx, worker.Store, a.WorkerTaskID); err != nil {
			t.Fatalf("still handed off: %v", err)
		}
	}
	// The main sees the worker no longer accepts it, and takes nothing back.
	eventually(t, "not accepted", func() bool { return mainAway(t, main, firstID).View == service.AwayViewNotAccepted })
	if resp := worker.Do(http.MethodDelete, "/api/settings/mains/"+main.InstanceID(), worker.WebToken(), ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unpairing again: %d", resp.StatusCode)
	}
}

func TestADropAndALeaveAreSafeToRepeat(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	_, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	client, err := main.Workers.Client(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := client.Call(ctx, http.MethodPost, service.HandoffDropPath(away.HandoffID), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	droppedOnWorker(t, worker, main, away)
	var refusal *service.WorkerRefusal
	if err := client.Call(ctx, http.MethodPost, service.HandoffDropPath("ho-none"), nil, nil); !errors.As(err, &refusal) || refusal.Code != "not_found" {
		t.Fatalf("dropping an unknown handoff: %v", err)
	}
	if err := client.Call(ctx, http.MethodPost, service.HandoffLeavePath, nil, nil); err != nil {
		t.Fatal(err)
	}
	if workerAccepts(t, worker, main) {
		t.Fatal("the worker still accepts the main")
	}
	if err := client.Call(ctx, http.MethodPost, service.HandoffLeavePath, nil, nil); !errors.As(err, &refusal) || refusal.Status != http.StatusUnauthorized {
		t.Fatalf("leaving again: %v", err)
	}
}

func TestTheWebAPITakesBackRemovesAWorkerAndCountsItsAwayTasks(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	web := newMainWeb(t, main)
	first, _ := awayOn(t, main, worker, paired.InstanceID, "First")
	second, _ := awayOn(t, main, worker, paired.InstanceID, "Second")
	eventually(t, "rows", func() bool { return mainAway(t, main, second).View == service.AwayViewIdle })

	// Settings › Workers says how many tasks Remove would bring home.
	var listed struct {
		Workers []service.Worker `json:"workers"`
	}
	if code := web.do(http.MethodGet, "/api/settings/workers", "", &listed); code != http.StatusOK || len(listed.Workers) != 1 || listed.Workers[0].Away != 2 {
		t.Fatalf("workers: %d %+v", code, listed)
	}
	var refused struct {
		Error string `json:"error"`
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+first+"/take-back", "", &refused); code != http.StatusConflict || !strings.Contains(refused.Error, "recall it") {
		t.Fatalf("taking back a reachable worker's task: %d %+v", code, refused)
	}
	unreachable(t, main, worker, paired.InstanceID, first)
	var took struct {
		Returned service.ReturnedTask `json:"returned"`
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+first+"/take-back", "", &took); code != http.StatusOK || !took.Returned.TakenBack || took.Returned.Column != "Ready" {
		t.Fatalf("take back: %d %+v", code, took)
	}
	var removed struct {
		Removed service.RemovedWorker `json:"removed"`
	}
	if code := web.do(http.MethodDelete, "/api/settings/workers/"+paired.InstanceID, "", &removed); code != http.StatusOK || removed.Removed.Worker != "vm-1" || len(removed.Removed.TakenBack) != 1 || removed.Removed.Left {
		t.Fatalf("remove: %d %+v", code, removed)
	}
	homeWithoutResults(t, main, second, "Ready")
	if code := web.do(http.MethodGet, "/api/settings/workers", "", &listed); code != http.StatusOK || len(listed.Workers) != 0 {
		t.Fatalf("workers after remove: %d %+v", code, listed)
	}
	if code := web.do(http.MethodDelete, "/api/settings/workers/"+paired.InstanceID, "", &refused); code != http.StatusNotFound {
		t.Fatalf("removing it again: %d %+v", code, refused)
	}
}
