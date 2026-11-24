package store

import (
	"context"
	"testing"
	"time"
)

func reeveWaitExecution(t *testing.T, s *Store, taskID string) string {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: taskID, Title: taskID, Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	id := "ex-" + taskID
	if err := s.CreateExecution(ctx, Execution{ID: id, Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: taskID, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return id
}

func openWaits(t *testing.T, s *Store) map[string]OpenReeveWaitTimes {
	t.Helper()
	waits, err := s.OpenReeveWaits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]OpenReeveWaitTimes{}
	for _, w := range waits {
		out[w.ExecutionID] = w
	}
	return out
}

// Telling the Reeve marks only the named executions' open waits it was not
// yet told of; a second question keeps the time it was told, and a wait
// that opens again after it was cleared has not been told.
func TestTellingTheReeveMarksOnlyThoseWaits(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	asked := time.Date(2026, 11, 24, 11, 53, 0, 0, time.UTC)
	a, b := reeveWaitExecution(t, s, "T-A"), reeveWaitExecution(t, s, "T-B")
	for _, ex := range []struct{ task, id string }{{"T-A", a}, {"T-B", b}} {
		if err := s.OpenReeveWait(ctx, ex.task, ex.id, asked); err != nil {
			t.Fatal(err)
		}
	}
	told := asked.Add(20 * time.Minute)
	if err := s.MarkReeveWaitsTold(ctx, []string{a}, told); err != nil {
		t.Fatal(err)
	}
	waits := openWaits(t, s)
	if w := waits[a]; !w.Since.Equal(asked) || w.ToldAt == nil || !w.ToldAt.Equal(told) {
		t.Fatalf("told wait: %+v", w)
	}
	if w := waits[b]; w.ToldAt != nil {
		t.Fatalf("a wait not named was marked told: %+v", w)
	}

	// Told again later: the first time stands.
	if err := s.MarkReeveWaitsTold(ctx, []string{a}, told.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A second question on the open wait keeps it too.
	if err := s.OpenReeveWait(ctx, "T-A", a, told.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if w := openWaits(t, s)[a]; w.ToldAt == nil || !w.ToldAt.Equal(told) {
		t.Fatalf("after a second question: %+v", w)
	}

	// Cleared, then asked again: a new wait, not yet told.
	if _, err := s.ClearReeveWait(ctx, a, ReeveWaitReeveAnswered); err != nil {
		t.Fatal(err)
	}
	again := told.Add(2 * time.Hour)
	if err := s.OpenReeveWait(ctx, "T-A", a, again); err != nil {
		t.Fatal(err)
	}
	if w := openWaits(t, s)[a]; !w.Since.Equal(again) || w.ToldAt != nil {
		t.Fatalf("a reopened wait: %+v", w)
	}

}
