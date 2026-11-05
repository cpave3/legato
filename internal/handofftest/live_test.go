package handofftest

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

// awayOn hands a new local task to the worker and waits for its agent to
// start. It returns the main's task ID and the away task.
func awayOn(t *testing.T, main *Main, worker *Worker, workerID, title string) (string, service.AwayTask) {
	t.Helper()
	taskID, _ := localTask(t, main, title, "Details.", "")
	away, err := handOff(t, main, workerID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	return taskID, away
}

// mainAway is the main's away task, as the board shows it.
func mainAway(t *testing.T, main *Main, taskID string) service.AwayTask {
	t.Helper()
	away, err := main.Handoffs.Away(context.Background(), taskID)
	if err != nil || away == nil {
		t.Fatalf("away %s: %+v, %v", taskID, away, err)
	}
	return *away
}

// workerRecord is the main's record of a worker.
func workerRecord(t *testing.T, main *Main, workerID string) service.Worker {
	t.Helper()
	workers, err := main.Workers.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range workers {
		if w.InstanceID == workerID {
			return w
		}
	}
	t.Fatalf("no worker %s", workerID)
	return service.Worker{}
}

// pushBranches makes a worker's clone say origin has these branches, as git
// does after a push, by writing the remote-tracking refs.
func pushBranches(t *testing.T, clonePath string, branches ...string) {
	t.Helper()
	for _, b := range branches {
		for _, ref := range []string{"refs/heads/" + b, "refs/remotes/origin/" + b} {
			path := filepath.Join(clonePath, ".git", filepath.FromSlash(ref))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Repeat("a", 40)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPullListsEachUnfinishedHandoffOfTheCallingMain(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix the flaky login test")
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "waiting", ""); err != nil {
		t.Fatal(err)
	}
	clones := worker.Repos.Clones()
	if len(clones) != 1 {
		t.Fatalf("clones %+v", clones)
	}
	branch := "legato/" + strings.ToLower(taskID)
	clonePath := filepath.Join(worker.Dir, "jobs", clones[0].ExecutionID, "widget")
	pushBranches(t, clonePath, branch)

	client, err := main.Workers.Client(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	var pull service.HandoffPull
	if err := client.Call(ctx, http.MethodGet, service.HandoffPullPath, nil, &pull); err != nil {
		t.Fatal(err)
	}
	if pull.WorkerInstanceID != worker.InstanceID() || len(pull.Handoffs) != 1 {
		t.Fatalf("pull %+v", pull)
	}
	row := pull.Handoffs[0]
	want := service.HandoffSummary{HandoffID: away.HandoffID, WorkerTaskID: away.WorkerTaskID, State: store.HandoffInProgress,
		Activity: service.HandoffActivityWaiting, Column: "Doing", Branches: []string{branch}, Title: "Fix the flaky login test",
		URL: "/board?highlight_task=" + away.WorkerTaskID}
	if !reflect.DeepEqual(row, want) {
		t.Fatalf("row\n got %+v\nwant %+v", row, want)
	}

	// Another main paired with the same worker hears nothing of it.
	other := NewMain(t, MainOptions{Name: "laptop", Clock: worker.Clock})
	otherPaired := Pair(t, other, worker)
	otherClient, err := other.Workers.Client(ctx, otherPaired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	var none service.HandoffPull
	if err := otherClient.Call(ctx, http.MethodGet, service.HandoffPullPath, nil, &none); err != nil || len(none.Handoffs) != 0 {
		t.Fatalf("the other main's pull: %+v, %v", none, err)
	}
	// The worker's full web token is no main.
	if resp := worker.Do(http.MethodGet, service.HandoffPullPath, worker.WebToken(), ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pull with the web token: %d", resp.StatusCode)
	}
}

func TestPullListsAStacksPushedBranchesInStackOrder(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "A big change")
	base := "legato/" + strings.ToLower(taskID)
	clone := filepath.Join(worker.Dir, "jobs", worker.Repos.Clones()[0].ExecutionID, "widget")
	// A Staccato stack of three on main; the top one is not pushed yet.
	sha := strings.Repeat("b", 40)
	graph := `{"version":7,"root":"main","branches":{
		"` + base + `-api":{"parent":"main","base_sha":"` + sha + `"},
		"` + base + `-ui":{"parent":"` + base + `-api","base_sha":"` + sha + `"},
		"` + base + `-docs":{"parent":"` + base + `-ui","base_sha":"` + sha + `"}}}`
	if err := os.MkdirAll(filepath.Join(clone, ".git", "stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "stack", "graph.json"), []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}
	pushBranches(t, clone, base+"-ui", base+"-api")

	pull, err := worker.Handoffs.Pull(ctx, main.InstanceID())
	if err != nil || len(pull.Handoffs) != 1 {
		t.Fatalf("pull %+v, %v", pull, err)
	}
	if got, want := pull.Handoffs[0].Branches, []string{base + "-api", base + "-ui"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("branches %v, want %v", got, want)
	}
}

// streamMessages runs a worker's Events stream for main and collects what
// it sends.
type streamMessages struct {
	mu   sync.Mutex
	msgs []service.HandoffStreamMessage
}

func (s *streamMessages) send(m service.HandoffStreamMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
	return nil
}

func (s *streamMessages) rows() []service.HandoffSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []service.HandoffSummary
	for _, m := range s.msgs {
		if m.Type == service.HandoffStreamRow {
			out = append(out, *m.Row)
		}
	}
	return out
}

func TestTheEventsStreamSendsAPullThenEachChangeWithActivityDebounced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	_, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")

	got := &streamMessages{}
	done := make(chan error, 1)
	go func() { done <- worker.Handoffs.Stream(ctx, main.InstanceID(), got.send) }()
	eventually(t, "the pull", func() bool {
		got.mu.Lock()
		defer got.mu.Unlock()
		return len(got.msgs) == 1
	})
	first := got.msgs[0]
	if first.Type != service.HandoffStreamPull || first.WorkerInstanceID != paired.InstanceID || len(first.Handoffs) != 1 ||
		first.Handoffs[0].Activity != service.HandoffActivityIdle {
		t.Fatalf("first message %+v", first)
	}

	// The first change of activity after the pull goes at once.
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first activity row", func() bool { return len(got.rows()) == 1 })
	if rows := got.rows(); rows[0].Activity != service.HandoffActivityWorking {
		t.Fatalf("first row %+v", rows[0])
	}
	// Then it flips three times in a moment: nothing goes within the
	// second, then the last state.
	for _, a := range []string{"waiting", "working", "waiting"} {
		if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, a, ""); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if rows := got.rows(); len(rows) != 1 {
		t.Fatalf("rows within the second: %+v", rows)
	}
	waitUp(t, 2*time.Second, "the debounced row", func() bool { return len(got.rows()) == 2 })
	if rows := got.rows(); rows[1].Activity != service.HandoffActivityWaiting {
		t.Fatalf("debounced row %+v", rows[1])
	}

	// A change of state goes at once, whatever the activity did.
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the done row", func() bool {
		rows := got.rows()
		last := rows[len(rows)-1]
		return last.State == store.HandoffDone && last.Column == "Done"
	})
	for _, m := range got.msgs {
		if m.WorkerInstanceID != paired.InstanceID {
			t.Fatalf("a message without the worker's instance ID: %+v", m)
		}
	}
	cancel()
	<-done
}

// waitUp waits up to d for ok.
func waitUp(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheEventsRouteTakesOnlyAMainsHandoffToken(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	argus, err := worker.Tokens.Create(ctx, "argus", []string{service.ScopeOwnerRead})
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"the web token": worker.WebToken(), "an Argus token": argus, "no token": ""} {
		header := http.Header{}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		_, resp, err := websocket.Dial(ctx, "wss://"+strings.TrimPrefix(worker.URL, "https://")+service.HandoffEventsPath,
			&websocket.DialOptions{HTTPClient: worker.Client(), HTTPHeader: header})
		if err == nil || resp == nil || (resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized) {
			t.Fatalf("%s: %v, %+v", name, err, resp)
		}
	}
}

// liveMain is a main with its worker connections started.
func liveMain(t *testing.T, worker *Worker, edit ...func(*service.WorkerConnectionsOptions)) *Main {
	t.Helper()
	main := NewMain(t, MainOptions{Clock: worker.Clock, Connections: func(o *service.WorkerConnectionsOptions) {
		for _, e := range edit {
			e(o)
		}
	}})
	return main
}

func TestTheMainHearsAnAwayTaskChangeAsItHappens(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	changed := main.Bus.Subscribe(events.EventCardUpdated)
	main.StartConnections()

	// Nothing is away: no connection.
	time.Sleep(50 * time.Millisecond)
	if main.Connections.Connected(paired.InstanceID) {
		t.Fatal("connected to a worker that holds no away task")
	}
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "the connection", func() bool { return main.Connections.Connected(paired.InstanceID) })

	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "working on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewWorking })
	got := mainAway(t, main, taskID)
	if got.State != store.HandoffInProgress || got.Activity != service.HandoffActivityWorking || got.WorkerColumn != "Doing" || got.WorkerTitle != "Fix it" {
		t.Fatalf("away %+v", got)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("no board event for the row")
	}
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "waiting", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "needs you on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewNeedsYou })
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })

	// Both sides know when the main last pulled.
	if w := workerRecord(t, main, paired.InstanceID); w.LastPullAt == nil || w.UnreachableSince != nil {
		t.Fatalf("worker record %+v", w)
	}
	mains, err := worker.Tokens.ListMains(ctx)
	if err != nil || len(mains) != 1 || mains[0].LastPullAt == nil || mains[0].OpenHandoffs != 1 {
		t.Fatalf("mains on the worker: %+v, %v", mains, err)
	}

	// Once nothing is away there, the connection closes.
	if err := main.Store.DeleteAwayTask(ctx, away.HandoffID); err != nil {
		t.Fatal(err)
	}
	main.Connections.Kick()
	eventually(t, "the connection closing", func() bool { return !main.Connections.Connected(paired.InstanceID) })
}

func TestAWorkerDownIsMarkedUnreachableOnlyAfterTheGrace(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "the connection", func() bool { return main.Connections.Connected(paired.InstanceID) })
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })

	dropped := worker.Clock.Now()
	worker.Stop()
	eventually(t, "the drop", func() bool { return !main.Connections.Connected(paired.InstanceID) })
	ctx := context.Background()
	worker.Clock.Advance(29 * time.Second)
	main.Connections.CheckReach(ctx)
	if w := workerRecord(t, main, paired.InstanceID); w.UnreachableSince != nil {
		t.Fatalf("marked unreachable within the grace: %+v", w)
	}
	if got := mainAway(t, main, taskID); got.View != service.AwayViewIdle {
		t.Fatalf("view within the grace: %+v", got)
	}
	worker.Clock.Advance(2 * time.Second)
	main.Connections.CheckReach(ctx)
	w := workerRecord(t, main, paired.InstanceID)
	if w.UnreachableSince == nil || *w.UnreachableSince != dropped.UTC().Format(time.RFC3339) {
		t.Fatalf("unreachable since %v, want the drop at %s", w.UnreachableSince, dropped)
	}
	got := mainAway(t, main, taskID)
	if got.View != service.AwayViewUnreachable || got.State != store.HandoffInProgress || got.WorkerUnreachableSince == "" {
		t.Fatalf("away while unreachable %+v", got)
	}

	// Back up: it reconnects by itself, pulls, and the mark goes.
	worker.Restart()
	eventually(t, "the reconnection", func() bool { return workerRecord(t, main, paired.InstanceID).UnreachableSince == nil })
	if got := mainAway(t, main, taskID); got.View != service.AwayViewIdle {
		t.Fatalf("away after the reconnection %+v", got)
	}
}

func TestAHandoffCallWithNoAnswerMarksTheWorkerAtOnce(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	firstID, _ := awayOn(t, main, worker, paired.InstanceID, "First")
	worker.Stop()
	secondID, _ := localTask(t, main, "Second", "", "")
	if _, err := handOff(t, main, paired.InstanceID, secondID); err == nil {
		t.Fatal("handed off to a stopped worker")
	}
	w := workerRecord(t, main, paired.InstanceID)
	if w.UnreachableSince == nil || *w.UnreachableSince != worker.Clock.Now().UTC().Format(time.RFC3339) {
		t.Fatalf("worker record %+v", w)
	}
	if got := mainAway(t, main, firstID); got.View != service.AwayViewUnreachable {
		t.Fatalf("first task %+v", got)
	}
}

func TestAWorkerThatUnpairedTheMainNoLongerAcceptsIt(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "working", func() bool { return mainAway(t, main, taskID).View == service.AwayViewWorking })

	if err := worker.Tokens.RevokeMain(ctx, main.InstanceID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the refusal", func() bool { return mainAway(t, main, taskID).View == service.AwayViewNotAccepted })
	got := mainAway(t, main, taskID)
	// The last summary stays, and the main takes nothing back.
	if got.State != store.HandoffInProgress || got.Activity != service.HandoffActivityWorking || got.WorkerRefusedAt == "" {
		t.Fatalf("away %+v", got)
	}
	if w := workerRecord(t, main, paired.InstanceID); w.RefusedAt == nil || w.UnreachableSince != nil {
		t.Fatalf("worker record %+v", w)
	}
	if err := service.RefuseAway(ctx, main.Store, taskID); err == nil {
		t.Fatal("the task is back on the main")
	}
}

func TestAHandoffTheWorkerNoLongerHasIsLostOnWorker(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	keptID, _ := awayOn(t, main, worker, paired.InstanceID, "Kept")
	goneID, gone := awayOn(t, main, worker, paired.InstanceID, "Gone while connected")
	eventually(t, "the rows", func() bool { return mainAway(t, main, goneID).View == service.AwayViewIdle })

	// Deleted on the worker while the main listens: lost at once.
	if err := worker.Store.DeleteTask(ctx, gone.WorkerTaskID); err != nil {
		t.Fatal(err)
	}
	worker.Bus.Publish(events.Event{Type: events.EventCardsRefreshed})
	eventually(t, "lost", func() bool { return mainAway(t, main, goneID).View == service.AwayViewLost })

	// Deleted while the worker is down: lost at the next pull.
	worker.Stop()
	db, err := store.New(worker.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	keptAway := mainAway(t, main, keptID)
	if err := db.DeleteTask(ctx, keptAway.WorkerTaskID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	worker.Restart()
	eventually(t, "lost at the pull", func() bool { return mainAway(t, main, keptID).View == service.AwayViewLost })
	got := mainAway(t, main, keptID)
	if got.Lost == "" {
		t.Fatalf("lost without a reason: %+v", got)
	}
	// Lost tasks stay away: only the person takes them back.
	for _, id := range []string{keptID, goneID} {
		if err := service.RefuseAway(ctx, main.Store, id); err == nil {
			t.Fatalf("%s is back on the main", id)
		}
	}
}

func TestAWorkerAnsweringAsAnotherInstanceLosesItsAwayTasks(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })

	worker.Stop()
	db, err := store.New(worker.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetInstanceValue(ctx, "server_id", "srv-0000000000000000"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	worker.Restart()
	eventually(t, "lost", func() bool { return mainAway(t, main, taskID).View == service.AwayViewLost })
	if got := mainAway(t, main, taskID); !strings.Contains(got.Lost, "srv-0000000000000000") {
		t.Fatalf("lost reason %q", got.Lost)
	}
}

func TestAWorkerThatStopsAnsweringPingsIsDroppedAndReconnected(t *testing.T) {
	var dials atomic.Int32
	deaf := true
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != service.HandoffEventsPath {
				next.ServeHTTP(w, r)
				return
			}
			dials.Add(1)
			if !deaf {
				next.ServeHTTP(w, r)
				return
			}
			// Accepts, then never reads: pings go unanswered.
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			<-r.Context().Done()
			conn.CloseNow()
		})
	}})
	main := liveMain(t, worker, func(o *service.WorkerConnectionsOptions) { o.PingEvery = 30 * time.Millisecond })
	paired := Pair(t, main, worker)
	awayOn(t, main, worker, paired.InstanceID, "Fix it")
	main.StartConnections()
	waitUp(t, 3*time.Second, "a second connection", func() bool { return dials.Load() >= 2 })
}

func TestAManualSyncReconnectsAndPullsEveryWorker(t *testing.T) {
	ctx := context.Background()
	var dials atomic.Int32
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == service.HandoffEventsPath {
				dials.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	}})
	idle := StartWorker(t, WorkerOptions{Name: "vm-2", Clock: worker.Clock})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	idlePaired := Pair(t, main, idle)
	main.StartConnections()
	awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "the connection", func() bool { return main.Connections.Connected(paired.InstanceID) })
	before := dials.Load()

	worker.Clock.Advance(time.Minute)
	if err := main.Connections.SyncWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != before+1 {
		t.Fatalf("dials %d, want %d", dials.Load(), before+1)
	}
	now := worker.Clock.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{paired.InstanceID, idlePaired.InstanceID} {
		if w := workerRecord(t, main, id); w.LastPullAt == nil || *w.LastPullAt != now {
			t.Fatalf("worker %s last pulled %v, want %s", w.Name, w.LastPullAt, now)
		}
	}
	// A worker without away tasks keeps no connection after the pull.
	if main.Connections.Connected(idlePaired.InstanceID) {
		t.Fatal("connected to a worker with nothing away")
	}
	// A sync that cannot reach a worker says so.
	idle.Stop()
	if err := main.Connections.SyncWorkers(ctx); err == nil || !strings.Contains(err.Error(), "vm-2") {
		t.Fatalf("sync with vm-2 down: %v", err)
	}
}
