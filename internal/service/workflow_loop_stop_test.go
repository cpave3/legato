package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// moveBackAndForth moves a task out of Triage and back in, as an agent
// that moves a card each run would, and captures the move.
func moveBackAndForth(t *testing.T, board BoardService, coordinator *WorkflowTriggerCoordinator, taskID string) {
	t.Helper()
	if err := board.MoveCard(context.Background(), taskID, "Backlog"); err != nil {
		t.Fatal(err)
	}
	moveAndCapture(t, board, coordinator, taskID)
}

func requestsWith(t *testing.T, db *store.Store, status string) []store.WorkflowTriggerRequest {
	t.Helper()
	list, err := db.ListWorkflowTriggerRequests(context.Background(), status)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTheFourthRunInADayForOneTaskIsHeld(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "")
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	coordinator.SetClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		moveBackAndForth(t, board, coordinator, "T-1")
		now = now.Add(time.Hour)
	}
	if n := len(requestsWith(t, db, "started")); n != 3 {
		t.Fatalf("started = %d, want 3", n)
	}
	moveBackAndForth(t, board, coordinator, "T-1")
	held := requestsWith(t, db, "held")
	if len(held) != 1 || !strings.Contains(held[0].Error, "loop") {
		t.Fatalf("held = %+v", held)
	}
	// Another task has its own count.
	moveAndCapture(t, board, coordinator, "T-2")
	if n := len(requestsWith(t, db, "started")); n != 4 {
		t.Fatalf("another task's run: started = %d", n)
	}
	// A day after the first runs, the count has room again.
	now = now.Add(24 * time.Hour)
	moveBackAndForth(t, board, coordinator, "T-1")
	if n := len(requestsWith(t, db, "started")); n != 5 {
		t.Fatalf("after a day: started = %d", n)
	}
}

func TestATriggersOwnLimitBeatsTheDefault(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "    max_runs_per_day: 1\n")
	moveBackAndForth(t, board, coordinator, "T-1")
	moveBackAndForth(t, board, coordinator, "T-1")
	if started, held := len(requestsWith(t, db, "started")), len(requestsWith(t, db, "held")); started != 1 || held != 1 {
		t.Fatalf("started %d held %d", started, held)
	}
}

func TestRunAnywayStartsAHeldRequestAndPauseTriggerPausesIt(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "    max_runs_per_day: 1\n")
	ctx := context.Background()
	moveBackAndForth(t, board, coordinator, "T-1")
	moveBackAndForth(t, board, coordinator, "T-1")
	held := requestsWith(t, db, "held")
	if len(held) != 1 {
		t.Fatalf("held = %d", len(held))
	}
	got, err := coordinator.RunAnyway(ctx, held[0].ID)
	if err != nil || got.Status != "started" || got.RunID == "" {
		t.Fatalf("run anyway = %+v, %v", got, err)
	}
	moveBackAndForth(t, board, coordinator, "T-1")
	held = requestsWith(t, db, "held")
	if len(held) != 1 {
		t.Fatalf("held again = %d", len(held))
	}
	if err := coordinator.PauseTrigger(ctx, held[0].ID); err != nil {
		t.Fatal(err)
	}
	if request, _ := db.GetWorkflowTriggerRequest(ctx, held[0].ID); request.Status != "skipped" {
		t.Fatalf("paused request = %s", request.Status)
	}
	states, _ := coordinator.TriggerStates(ctx)
	if s := states["triage"][0]; s.Enabled || !s.Overridden {
		t.Fatalf("after Pause trigger: %+v", s)
	}
	if _, err := coordinator.RunAnyway(ctx, held[0].ID); err == nil {
		t.Fatal("ran a request that is no longer held")
	}
}

func TestALoopStopReachesAttentionWithItsActions(t *testing.T) {
	db, coordinator, board := triggerSwitchFixture(t, "    max_runs_per_day: 1\n")
	ctx := context.Background()
	moveBackAndForth(t, board, coordinator, "T-1")
	moveBackAndForth(t, board, coordinator, "T-1")
	adapter := NewWorkflowTriggerAttentionAdapter(coordinator, db)
	items, err := adapter.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SignalKind != AttentionSignalWorkflowTriggerLoopStop || items[0].TaskID != "T-1" || !strings.Contains(strings.ToLower(items[0].Reason), "loop") {
		t.Fatalf("items = %+v", items)
	}
	var ids []string
	for _, action := range items[0].Actions {
		ids = append(ids, action.ID)
	}
	if strings.Join(ids, ",") != "run_anyway,pause_trigger" {
		t.Fatalf("actions = %v", ids)
	}
	if err := adapter.Execute(ctx, AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "run_anyway"}); err != nil {
		t.Fatal(err)
	}
	if n := len(requestsWith(t, db, "started")); n != 2 {
		t.Fatalf("started after Run anyway = %d", n)
	}
	_ = workflowengine.DefaultMaxRunsPerDay
}
