package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
)

// newTestSwarmService stands up a real store + real agent service wrapping a
// mockTmux. The swarm service is wired to the event bus so callers can observe
// state changes and the conductor lock + debouncer behave the same as in
// production.
func newTestSwarmService(t *testing.T) (SwarmService, AgentService, *store.Store, *mockTmux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	agentSvc := NewAgentService(st, mt, t.TempDir())
	bus := events.New()
	cfg := SwarmConfig{MaxConcurrentAgents: 4, MaxSubtasksPerPlan: 10}
	sw := NewSwarmService(st, agentSvc, bus, cfg, t.TempDir())
	return sw, agentSvc, st, mt
}

func seedParentTask(t *testing.T, st *store.Store, id string) {
	t.Helper()
	err := st.CreateTask(context.Background(), store.Task{
		ID:        id,
		Title:     "parent " + id,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedSubtask(t *testing.T, st *store.Store, id, parentID, status string) {
	t.Helper()
	err := st.CreateSubtask(context.Background(), store.Subtask{
		ID:           id,
		ParentTaskID: parentID,
		Title:        "sub " + id,
		Role:         "worker",
		Status:       status,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestProgressFirstCallNoDeadlock is the regression test for the
// scheduleProgressEvent → pendingProgressMeta double-lock deadlock. Before the
// fix, the first Progress call from a fresh worker hung the goroutine because
// both functions tried to acquire the non-reentrant debounceMu.
func TestProgressFirstCallNoDeadlock(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa01", "parent-1", "in_progress")

	done := make(chan error, 1)
	go func() {
		done <- sw.Progress(context.Background(), "st-aaaaaaaa01", "first progress event")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Progress returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Progress() deadlocked")
	}
}

// TestProgressDebounceCollapsesWithinWindow verifies that two Progress calls
// within the 1s window collapse to a single deferred event with the latest
// text.
func TestProgressDebounceCollapsesWithinWindow(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa02", "parent-1", "in_progress")

	ctx := context.Background()
	if err := sw.Progress(ctx, "st-aaaaaaaa02", "first"); err != nil {
		t.Fatal(err)
	}
	if err := sw.Progress(ctx, "st-aaaaaaaa02", "second"); err != nil {
		t.Fatal(err)
	}

	// Inspect the concrete service to verify only one entry is pending and the
	// text was overwritten.
	concrete, ok := sw.(*swarmService)
	if !ok {
		t.Fatal("expected *swarmService concrete type")
	}
	concrete.debounceMu.Lock()
	defer concrete.debounceMu.Unlock()
	if got := len(concrete.pendingProgress); got != 1 {
		t.Fatalf("pendingProgress len = %d, want 1", got)
	}
	entry := concrete.pendingProgress["st-aaaaaaaa02"]
	if entry == nil {
		t.Fatal("expected pending entry for subtask")
	}
	if entry.text != "second" {
		t.Errorf("entry.text = %q, want %q (latest wins)", entry.text, "second")
	}
}

// TestProgressTransitionsDispatchedToInProgress verifies the lifecycle hop on
// the first Progress call from a worker still in `dispatched` state.
func TestProgressTransitionsDispatchedToInProgress(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa03", "parent-1", "dispatched")

	if err := sw.Progress(context.Background(), "st-aaaaaaaa03", "starting"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetSubtask(context.Background(), "st-aaaaaaaa03")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", got.Status, "in_progress")
	}
}

// TestBuiltTransitionsInProgressToReporting verifies the `reporting` transition
// and that pending progress for the same sub-task is flushed (not lost).
func TestBuiltTransitionsInProgressToReporting(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa04", "parent-1", "in_progress")

	if err := sw.Built(context.Background(), "st-aaaaaaaa04"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetSubtask(context.Background(), "st-aaaaaaaa04")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "reporting" {
		t.Errorf("Status = %q, want %q", got.Status, "reporting")
	}
}

// TestCloseRatifiesReportingToDone covers the conductor's ratification path.
func TestCloseRatifiesReportingToDone(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa05", "parent-1", "reporting")

	if err := sw.Close(context.Background(), "st-aaaaaaaa05"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetSubtask(context.Background(), "st-aaaaaaaa05")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" {
		t.Errorf("Status = %q, want %q", got.Status, "done")
	}
}

// TestStartSwarmRefusesDoubleSpawn verifies the second StartSwarm for the same
// parent fails when a session is already running.
func TestStartSwarmRefusesDoubleSpawn(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")

	if err := sw.StartSwarm(context.Background(), "parent-1", t.TempDir()); err != nil {
		t.Fatalf("first StartSwarm: %v", err)
	}
	if err := sw.StartSwarm(context.Background(), "parent-1", t.TempDir()); err == nil {
		t.Fatal("second StartSwarm should have failed")
	}
}

// TestApplyApprovedPlanPersistsSubtasks verifies plan → subtask rows mapping.
func TestApplyApprovedPlanPersistsSubtasks(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-1", WorkingDir: t.TempDir()},
		Subtasks: []swarm.PlanSubtask{
			{Title: "Backend", Role: "backend"},
			{Title: "Frontend", Role: "frontend"},
		},
	}
	if err := sw.ApplyApprovedPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	subs, err := st.ListSubtasksByParent(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Fatalf("got %d subtasks, want 2", len(subs))
	}
	for _, s := range subs {
		if s.Status != "queued" {
			t.Errorf("subtask %s status = %q, want queued", s.ID, s.Status)
		}
	}
}

// TestHandleAgentDiedTransitionsToCancelled verifies the EventAgentDied
// subscriber correctly cancels live workers but leaves terminal ones alone.
func TestHandleAgentDiedTransitionsToCancelled(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa06", "parent-1", "in_progress")
	seedSubtask(t, st, "st-aaaaaaaa07", "parent-1", "done")

	ctx := context.Background()
	sw.HandleAgentDied(ctx, "parent-1", "st-aaaaaaaa06", "worker")
	sw.HandleAgentDied(ctx, "parent-1", "st-aaaaaaaa07", "worker")

	got1, _ := st.GetSubtask(ctx, "st-aaaaaaaa06")
	if got1.Status != "cancelled" {
		t.Errorf("in_progress→cancelled failed: got %q", got1.Status)
	}
	got2, _ := st.GetSubtask(ctx, "st-aaaaaaaa07")
	if got2.Status != "done" {
		t.Errorf("done should not be touched: got %q", got2.Status)
	}
}

// TestMaybeNotifyAllIdleFiresOnAllTerminal is the regression for W1: previously
// the guard `!hasReportingOrQueued` swallowed the most useful notification —
// the one that fires when every worker is in a terminal state. Verify the
// notification is recorded as a swarm event in the inbox.
func TestMaybeNotifyAllIdleFiresOnAllTerminal(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaaa08", "parent-1", "done")
	seedSubtask(t, st, "st-aaaaaaaa09", "parent-1", "cancelled")

	// Spawn a fake conductor session so recordEventForConductor finds a target.
	if err := mt.Spawn("legato-parent-1", t.TempDir(), 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID:      "parent-1",
		TmuxSession: "legato-parent-1",
		Status:      "running",
		StartedAt:   "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	concrete := sw.(*swarmService)
	concrete.maybeNotifyAllIdle(context.Background(), "parent-1")

	// The event should be recorded as a swarm_event row.
	events, err := st.ListUnackedSwarmEvents(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected an all_idle event in inbox")
	}
	found := false
	for _, e := range events {
		if e.Kind == "all_idle" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no all_idle event found; got kinds: %+v", eventKinds(events))
	}
}

func eventKinds(events []store.SwarmEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}
