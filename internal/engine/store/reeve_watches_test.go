package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTheReeveWatchesAgentsItStarted(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.WatchForReeve(ctx, ReeveWatch{TaskID: "T-1", ReeveTaskID: "R-1", Seen: `{"notes":1}`}); err != nil {
		t.Fatal(err)
	}
	// Watching again keeps what was seen, for the session now watching.
	if err := s.WatchForReeve(ctx, ReeveWatch{TaskID: "T-1", ReeveTaskID: "R-2", Seen: `{}`}); err != nil {
		t.Fatal(err)
	}
	watches, err := s.ListReeveWatches(ctx)
	if err != nil || len(watches) != 1 || watches[0].ReeveTaskID != "R-2" || watches[0].Seen != `{"notes":1}` {
		t.Fatalf("watches = %+v, %v", watches, err)
	}
	if err := s.SetReeveWatchSeen(ctx, "T-1", `{"notes":2}`); err != nil {
		t.Fatal(err)
	}
	watches, _ = s.ListReeveWatches(ctx)
	if watches[0].Seen != `{"notes":2}` {
		t.Fatalf("seen = %s", watches[0].Seen)
	}
	if err := s.DeleteReeveWatch(ctx, "T-1"); err != nil {
		t.Fatal(err)
	}
	if watches, _ = s.ListReeveWatches(ctx); len(watches) != 0 {
		t.Fatalf("after delete: %+v", watches)
	}
}

// A task worked since a time when one of its executions was working at any
// moment after it.
func TestATaskWorkedSince(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "worked.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, Execution{ID: "ex-1", Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: "T-1", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "ex-1", ActivityWorking, "", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, "ex-1", "", "", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		since time.Time
		want  bool
	}{{t0, true}, {t0.Add(90 * time.Second), true}, {t0.Add(3 * time.Minute), false}} {
		got, err := s.TaskWorkedSince(ctx, "T-1", tc.since)
		if err != nil || got != tc.want {
			t.Errorf("worked since %s = %v, %v; want %v", tc.since.Format(time.TimeOnly), got, err, tc.want)
		}
	}
}

// A watch keeps the events it was asked for; watching again replaces them.
func TestAWatchKeepsItsEvents(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.WatchForReeve(ctx, ReeveWatch{TaskID: "T-1", ReeveTaskID: "R-1", Events: "waiting,exited"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WatchForReeve(ctx, ReeveWatch{TaskID: "T-1", ReeveTaskID: "R-1", Events: "note"}); err != nil {
		t.Fatal(err)
	}
	watches, _ := s.ListReeveWatches(ctx)
	if len(watches) != 1 || watches[0].Events != "note" {
		t.Fatalf("watches = %+v", watches)
	}
}

// Events wait in a queue, oldest first, until the Reeve takes them; taking
// them removes those up to the last one taken, not any that came since.
func TestWatchEventsWaitInAQueue(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, body := range []string{`{"n":1}`, `{"n":2}`} {
		if err := s.QueueReeveWatchEvent(ctx, "T-1", body); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := s.ListReeveWatchEvents(ctx, 10)
	if err != nil || len(queued) != 2 || queued[0].Body != `{"n":1}` || queued[1].Body != `{"n":2}` || queued[0].CreatedAt == "" {
		t.Fatalf("queued = %+v, %v", queued, err)
	}
	if err := s.QueueReeveWatchEvent(ctx, "T-2", `{"n":3}`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteReeveWatchEventsThrough(ctx, queued[1].ID); err != nil {
		t.Fatal(err)
	}
	left, _ := s.ListReeveWatchEvents(ctx, 10)
	if len(left) != 1 || left[0].Body != `{"n":3}` {
		t.Fatalf("left = %+v", left)
	}
}
