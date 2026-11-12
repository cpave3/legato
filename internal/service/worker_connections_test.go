package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestReconnectsBackOffFromASecondDoublingToAMinute(t *testing.T) {
	var got []time.Duration
	for attempt := range 9 {
		got = append(got, WorkerBackoff(attempt, time.Second, time.Minute))
	}
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("waits %v", got)
		}
	}
	// The default jitter spreads a wait over its second half.
	jitter := NewWorkerConnections(nil, WorkerConnectionsOptions{}).opts.Jitter
	for range 100 {
		if d := jitter(time.Minute); d < 30*time.Second || d > time.Minute {
			t.Fatalf("jittered %v", d)
		}
	}
}

func TestTheMainShowsItsOwnViewOfTheWorkerFirst(t *testing.T) {
	cases := []struct {
		away AwayTask
		want string
	}{
		{AwayTask{State: "pending"}, AwayViewStarting},
		{AwayTask{State: "in_progress", Activity: HandoffActivityWorking}, AwayViewWorking},
		{AwayTask{State: "in_progress", Activity: HandoffActivityWaiting}, AwayViewNeedsYou},
		{AwayTask{State: "in_progress", Activity: HandoffActivityIdle}, AwayViewIdle},
		{AwayTask{State: "in_progress", Activity: HandoffActivityEnded}, AwayViewEnded},
		{AwayTask{State: "in_progress", Activity: HandoffActivityInterrupted}, AwayViewInterrupted},
		{AwayTask{State: "done", Activity: HandoffActivityIdle}, AwayViewDone},
		{AwayTask{State: "failed", Failure: "no clone"}, AwayViewFailed},
		{AwayTask{State: "returning"}, AwayViewComingBack},
		{AwayTask{State: "done", WorkerUnreachableSince: "t"}, AwayViewUnreachable},
		{AwayTask{State: "done", WorkerUnreachableSince: "t", WorkerRefusedAt: "t"}, AwayViewNotAccepted},
		{AwayTask{State: "done", WorkerRefusedAt: "t", LostAt: "t"}, AwayViewLost},
	}
	for _, c := range cases {
		if got := awayView(c.away); got != c.want {
			t.Errorf("%+v: %s, want %s", c.away, got, c.want)
		}
	}
}

// A reach check marks a worker unreachable from the drop it saw only if the
// connection is still down when it writes: a pull in between heard the
// worker, and marking it then would undo that from the old outage's time.
func TestAReachCheckDoesNotMarkAWorkerAPullHeardSinceItLooked(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.SaveWorker(ctx, store.Worker{InstanceID: "srv-1", Name: "vm-1", Address: "https://vm-1:3080", PairedAt: "2026-11-12T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	c := NewWorkerConnections(db, WorkerConnectionsOptions{Now: func() time.Time { return now }})
	wc := &workerConn{droppedAt: now.Add(-time.Minute)}
	c.conns["srv-1"] = wc

	// CheckReach saw it down; then a pull reconnected it.
	wc.droppedAt, wc.connected = time.Time{}, true
	c.markUnreachable(ctx, "srv-1", wc, now)
	if w, err := db.GetWorker(ctx, "srv-1"); err != nil || w.UnreachableSince != nil {
		t.Fatalf("marked after the pull: %+v, %v", w, err)
	}

	// Still down when it writes: marked, from when it dropped.
	wc.droppedAt, wc.connected = now.Add(-time.Minute), false
	c.markUnreachable(ctx, "srv-1", wc, now)
	if w, err := db.GetWorker(ctx, "srv-1"); err != nil || w.UnreachableSince == nil || *w.UnreachableSince != "2026-11-12T08:59:00Z" {
		t.Fatalf("not marked from its drop: %+v, %v", w, err)
	}
}
