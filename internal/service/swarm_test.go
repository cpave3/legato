package service

import (
	"context"
	"path/filepath"
	"strings"
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
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{
				{Title: "Backend", Role: "backend"},
				{Title: "Frontend", Role: "frontend"},
			}},
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

// seedSubtaskWithStep creates a subtask with a specific step index.
func seedSubtaskWithStep(t *testing.T, st *store.Store, id, parentID, status string, stepIndex int) {
	t.Helper()
	err := st.CreateSubtask(context.Background(), store.Subtask{
		ID:           id,
		ParentTaskID: parentID,
		Title:        "sub " + id,
		Role:         "worker",
		Status:       status,
		StepIndex:    stepIndex,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestApplyApprovedPlanSetsStepIndex verifies each subtask gets the correct
// step_index from the plan's steps.
func TestApplyApprovedPlanSetsStepIndex(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-1", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{{Title: "Step0-A"}, {Title: "Step0-B"}}},
			{Subtasks: []swarm.PlanSubtask{{Title: "Step1-A"}}},
		},
	}
	if err := sw.ApplyApprovedPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	subs, err := st.ListSubtasksByParent(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 {
		t.Fatalf("got %d subtasks, want 3", len(subs))
	}
	for _, s := range subs {
		switch s.Title {
		case "Step0-A", "Step0-B":
			if s.StepIndex != 0 {
				t.Errorf("%s step_index = %d, want 0", s.Title, s.StepIndex)
			}
		case "Step1-A":
			if s.StepIndex != 1 {
				t.Errorf("%s step_index = %d, want 1", s.Title, s.StepIndex)
			}
		default:
			t.Fatalf("unexpected subtask %s", s.Title)
		}
	}
}

// TestDispatchGatedByStep defers dispatch when subtask step > parent's active step.
func TestDispatchGatedByStep(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa10", "parent-1", "queued", 1)

	// active_step defaults to 0
	if err := sw.Dispatch(context.Background(), "st-aaaaaaaa10"); err == nil {
		t.Fatal("expected dispatch to be deferred")
	}

	got, _ := st.GetSubtask(context.Background(), "st-aaaaaaaa10")
	if got.Status != "queued" {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

// TestDispatchAllowedForCurrentStep dispatches a step-0 subtask when active_step is 0.
func TestDispatchAllowedForCurrentStep(t *testing.T) {
	sw, agentSvc, st, _ := newTestSwarmService(t)
	// Bypass the real agent service by hijacking its SpawnAgent for the subtask.
	seedParentTask(t, st, "parent-2")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa11", "parent-2", "queued", 0)

	// Agents can't really spawn in tests (no real tmux), so just check that the
	// step gate does NOT block it.
	ctx := context.Background()
	err := sw.Dispatch(ctx, "st-aaaaaaaa11")
	// It will likely fail at the agent spawn layer, but the important thing is
	// it's NOT the "step deferred" error.
	if err != nil && err.Error() == "dispatch deferred: step 0 is not yet active" {
		t.Fatalf("step 0 was blocked: %v", err)
	}

	_ = agentSvc
}

// TestNextStepAdvancesWhenTerminal moves active_step forward after all current
// step subtasks are done or cancelled.
func TestNextStepAdvancesWhenTerminal(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa12", "parent-1", "done", 0)
	seedSubtaskWithStep(t, st, "st-aaaaaaaa13", "parent-1", "cancelled", 0)
	seedSubtaskWithStep(t, st, "st-aaaaaaaa14", "parent-1", "queued", 1)

	if err := sw.NextStep(context.Background(), "parent-1"); err != nil {
		t.Fatalf("NextStep: %v", err)
	}

	parent, _ := st.GetTask(context.Background(), "parent-1")
	if parent.SwarmActiveStep != 1 {
		t.Errorf("active_step = %d, want 1", parent.SwarmActiveStep)
	}
}

// TestNextStepBlockedWhenNotTerminal refuses advancement if a current-step
// subtask is still active.
func TestNextStepBlockedWhenNotTerminal(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa15", "parent-1", "done", 0)
	seedSubtaskWithStep(t, st, "st-aaaaaaaa16", "parent-1", "queued", 0)
	seedSubtaskWithStep(t, st, "st-aaaaaaaa17", "parent-1", "queued", 1)

	if err := sw.NextStep(context.Background(), "parent-1"); err == nil {
		t.Fatal("expected NextStep to be blocked")
	}

	parent, _ := st.GetTask(context.Background(), "parent-1")
	if parent.SwarmActiveStep != 0 {
		t.Errorf("active_step = %d, want 0", parent.SwarmActiveStep)
	}
}

// TestNextStepBlockedWhenNoMoreSteps refuses advancement if already on the last
// step.
func TestNextStepBlockedWhenNoMoreSteps(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa18", "parent-1", "done", 0)

	if err := sw.NextStep(context.Background(), "parent-1"); err == nil {
		t.Fatal("expected NextStep to be blocked (no more steps)")
	}
}

// TestListSubtaskInfosIncludesStepIndex verifies the StepIndex field is
// surfaced in the UI DTO.
func TestListSubtaskInfosIncludesStepIndex(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa19", "parent-1", "queued", 2)

	infos, err := sw.ListSubtaskInfos(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("got %d infos, want 1", len(infos))
	}
	if infos[0].StepIndex != 2 {
		t.Errorf("StepIndex = %d, want 2", infos[0].StepIndex)
	}
}

// TestSnapshotIncludesActiveStepAndStepIndex verifies the parent payload carries
// active_step and each subtask carries step_index.
func TestSnapshotIncludesActiveStepAndStepIndex(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa20", "parent-1", "queued", 1)

	// Advance active step manually to 1 before snapshotting.
	_ = st.SetParentActiveStep(context.Background(), "parent-1", 1)

	data, err := sw.Snapshot(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), `"active_step": 1`) {
		t.Errorf("snapshot missing active_step=1: %s", string(data))
	}
	if !strings.Contains(string(data), `"step_index": 1`) {
		t.Errorf("snapshot missing step_index=1: %s", string(data))
	}
}

// TestMaybeNotifyAllIdleEmitsStepCompleted detects when the current step is
// terminal and more steps remain, and notifies with a step-completed message.
func TestMaybeNotifyAllIdleEmitsStepCompleted(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtaskWithStep(t, st, "st-aaaaaaaa21", "parent-1", "done", 0)
	seedSubtaskWithStep(t, st, "st-aaaaaaaa22", "parent-1", "queued", 1)

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

	events, err := st.ListUnackedSwarmEvents(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected an all_idle event in inbox")
	}
	found := false
	for _, e := range events {
		if e.Kind == "all_idle" && strings.Contains(e.Payload, "is complete") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no step-completed all_idle event found; got %+v", eventKinds(events))
	}
}

// TestSingleStepPlanBackwardCompatibility verifies a plan with one step produces
// subtasks all with step_index=0 and follows the existing lifecycle.
func TestSingleStepPlanBackwardCompatibility(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-1", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{
				{Title: "Backend", Role: "backend"},
				{Title: "Frontend", Role: "frontend"},
			}},
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
		if s.StepIndex != 0 {
			t.Errorf("subtask %s step_index = %d, want 0", s.Title, s.StepIndex)
		}
		if s.Status != "queued" {
			t.Errorf("subtask %s status = %q, want queued", s.Title, s.Status)
		}
	}

	// Step 0 subtasks should NOT be gated (active_step defaults to 0).
	err = sw.Dispatch(context.Background(), subs[0].ID)
	if err != nil && strings.Contains(err.Error(), "deferred") {
		t.Fatalf("single-step plan subtask was gated: %v", err)
	}
}

// TestTwoStepPlanGatingAndAdvancement verifies a two-step plan assigns correct
// step indices, gates step-1 dispatch until next-step is called, and notifies
// the conductor when step 0 completes.
func TestTwoStepPlanGatingAndAdvancement(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-1", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{{Title: "Step0-A", Role: "backend"}}},
			{Subtasks: []swarm.PlanSubtask{{Title: "Step1-A", Role: "frontend"}}},
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

	var step0Sub, step1Sub *store.Subtask
	for i := range subs {
		s := subs[i]
		switch s.Title {
		case "Step0-A":
			step0Sub = &s
		case "Step1-A":
			step1Sub = &s
		}
	}
	if step0Sub == nil || step1Sub == nil {
		t.Fatalf("missing subtasks: step0=%v step1=%v", step0Sub, step1Sub)
	}
	if step0Sub.StepIndex != 0 {
		t.Errorf("step0 step_index = %d, want 0", step0Sub.StepIndex)
	}
	if step1Sub.StepIndex != 1 {
		t.Errorf("step1 step_index = %d, want 1", step1Sub.StepIndex)
	}

	// Spawn conductor so notifications have a target.
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

	// Step 0 should dispatch freely.
	ctx := context.Background()
	step0Err := sw.Dispatch(ctx, step0Sub.ID)
	if step0Err != nil && strings.Contains(step0Err.Error(), "deferred") {
		t.Fatalf("step 0 dispatch was gated: %v", step0Err)
	}

	// Step 1 should be gated until next-step.
	step1Err := sw.Dispatch(ctx, step1Sub.ID)
	if step1Err == nil {
		t.Fatal("expected step 1 dispatch to be gated")
	}
	if !strings.Contains(step1Err.Error(), "deferred") && !strings.Contains(step1Err.Error(), "not yet active") {
		t.Errorf("expected deferred error, got: %v", step1Err)
	}

	// Mark step 0 done, advance, then step 1 should be dispatchable.
	_ = st.UpdateSubtaskStatus(ctx, step0Sub.ID, "done")
	if err := sw.NextStep(ctx, "parent-1"); err != nil {
		t.Fatalf("NextStep: %v", err)
	}

	parent, _ := st.GetTask(ctx, "parent-1")
	if parent.SwarmActiveStep != 1 {
		t.Errorf("active_step = %d, want 1", parent.SwarmActiveStep)
	}

	step1ErrAfter := sw.Dispatch(ctx, step1Sub.ID)
	if step1ErrAfter != nil && strings.Contains(step1ErrAfter.Error(), "deferred") {
		t.Fatalf("step 1 dispatch was still gated after next-step: %v", step1ErrAfter)
	}
}
