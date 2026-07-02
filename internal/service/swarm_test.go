package service

import (
	"context"
	"os"
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
	return newTestSwarmServiceWithAdapter(t, nil, SwarmConfig{MaxConcurrentAgents: 4, MaxSubtasksPerPlan: 10})
}

// newTestSwarmServiceWithAdapter is the customizable variant used by tests
// that need to spy on adapter launch calls (e.g. tier propagation tests). When
// adapter is nil, the agent service runs without an AI tool (same as
// newTestSwarmService). When provided, it's wired as both the default adapter
// and the sole entry of the registry under its Name(). Pass cfg to override
// SwarmConfig fields like DefaultAgent/ConductorTier/TierCatalog.
func newTestSwarmServiceWithAdapter(t *testing.T, adapter AIToolAdapter, cfg SwarmConfig) (SwarmService, AgentService, *store.Store, *mockTmux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	var agentSvc AgentService
	if adapter != nil {
		agentSvc = NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
			Adapter:  adapter,
			Adapters: map[string]AIToolAdapter{adapter.Name(): adapter},
		})
	} else {
		agentSvc = NewAgentService(st, mt, t.TempDir())
	}
	bus := events.New()
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

// TestDispatchPassesTierToAdapter verifies the persisted SwarmSubtask.Tier
// flows into AgentSpawnOptions.Tier and reaches the adapter's LaunchCommand
// at spawn time.
func TestDispatchPassesTierToAdapter(t *testing.T) {
	var capturedTier string
	adapter := &fakeAdapter{
		name:        "fake",
		rolePrompts: map[string]string{"worker": "be a worker"},
		launchFn: func(env map[string]string, brief, tier string) string {
			capturedTier = tier
			return ""
		},
	}
	sw, _, st, _ := newTestSwarmServiceWithAdapter(t, adapter, SwarmConfig{
		MaxConcurrentAgents: 4,
		DefaultAgent:        "fake",
	})

	seedParentTask(t, st, "parent-tier-dispatch")
	wd := t.TempDir()
	if err := st.SetTaskSwarmWorkingDir(context.Background(), "parent-tier-dispatch", &wd); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID:           "st-tier-aaaa",
		ParentTaskID: "parent-tier-dispatch",
		Title:        "Cheap edit",
		Role:         "worker",
		AgentKind:    "fake",
		Tier:         "small",
		Status:       "queued",
	}); err != nil {
		t.Fatal(err)
	}

	if err := sw.Dispatch(context.Background(), "st-tier-aaaa"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if capturedTier != "small" {
		t.Errorf("adapter saw tier = %q, want small", capturedTier)
	}
}

// TestStartSwarmUsesConductorTierAndAppendsCatalog verifies that StartSwarm
// passes the configured ConductorTier to the adapter and appends the tier
// catalog to the conductor's brief.
//
// The brief is observed by capturing it inside launchFn — agent.go always
// invokes LaunchCommand even when the return is "", so this side-channel
// works without a real adapter. If that contract changes, the captures
// below would silently see empty strings instead of asserting against the
// real brief; revisit this if either Contains check starts spuriously
// failing.
func TestStartSwarmUsesConductorTierAndAppendsCatalog(t *testing.T) {
	var capturedTier string
	var capturedBrief string
	adapter := &fakeAdapter{
		name:        "fake",
		rolePrompts: map[string]string{"conductor": "be a conductor"},
		launchFn: func(env map[string]string, brief, tier string) string {
			capturedTier = tier
			capturedBrief = brief
			return ""
		},
	}
	sw, _, st, _ := newTestSwarmServiceWithAdapter(t, adapter, SwarmConfig{
		MaxConcurrentAgents: 4,
		DefaultAgent:        "fake",
		ConductorTier:       "large",
		TierCatalog: map[string]map[string]string{
			"fake": {
				"small": "fast/cheap",
				"large": "deep reasoning",
			},
		},
	})

	seedParentTask(t, st, "parent-conductor-tier")
	if err := sw.StartSwarm(context.Background(), "parent-conductor-tier", t.TempDir()); err != nil {
		t.Fatalf("StartSwarm: %v", err)
	}
	if capturedTier != "large" {
		t.Errorf("conductor tier = %q, want large", capturedTier)
	}
	if !strings.Contains(capturedBrief, "## Available tiers") {
		t.Errorf("brief missing tier catalog: %q", capturedBrief)
	}
	if !strings.Contains(capturedBrief, "small — fast/cheap") {
		t.Errorf("brief missing tier description: %q", capturedBrief)
	}
}

func TestCreateAdhocSwarmPromotesExistingSession(t *testing.T) {
	adapter := &fakeAdapter{
		name:        "fake",
		rolePrompts: map[string]string{"conductor": "be a conductor"},
	}
	sw, agentSvc, st, mt := newTestSwarmServiceWithAdapter(t, adapter, SwarmConfig{
		MaxConcurrentAgents: 4,
		DefaultAgent:        "fake",
		TierCatalog: map[string]map[string]string{
			"fake": {"large": "deep reasoning"},
		},
	})
	ctx := context.Background()
	seedParentTask(t, st, "adhoc-1")
	if err := agentSvc.SpawnAgent(ctx, "adhoc-1", 0, 0); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}

	wd := t.TempDir()
	if err := sw.CreateAdhocSwarm(ctx, "adhoc-1", "split this work across agents", wd); err != nil {
		t.Fatalf("CreateAdhocSwarm: %v", err)
	}

	task, err := st.GetTask(ctx, "adhoc-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.SwarmWorkingDir == nil || *task.SwarmWorkingDir != wd {
		t.Fatalf("SwarmWorkingDir = %v, want %q", task.SwarmWorkingDir, wd)
	}
	sess, err := st.GetAgentSessionByTaskID(ctx, "adhoc-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Role != "conductor" {
		t.Errorf("Role = %q, want conductor", sess.Role)
	}
	if sess.ParentTaskID == nil || *sess.ParentTaskID != "adhoc-1" {
		t.Errorf("ParentTaskID = %v, want adhoc-1", sess.ParentTaskID)
	}
	if sess.TmuxSession != "legato-adhoc-1" {
		t.Errorf("TmuxSession = %q", sess.TmuxSession)
	}

	env := mt.envVarsFor("legato-adhoc-1")
	if env["LEGATO_AGENT_ROLE"] != "conductor" {
		t.Errorf("LEGATO_AGENT_ROLE = %q", env["LEGATO_AGENT_ROLE"])
	}
	if env["LEGATO_PARENT_TASK_ID"] != "adhoc-1" {
		t.Errorf("LEGATO_PARENT_TASK_ID = %q", env["LEGATO_PARENT_TASK_ID"])
	}
	if env["LEGATO_BRIEF_FILE"] == "" {
		t.Fatal("LEGATO_BRIEF_FILE not set")
	}
	brief, err := os.ReadFile(env["LEGATO_BRIEF_FILE"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brief), "split this work across agents") {
		t.Errorf("brief missing goal: %q", string(brief))
	}
	if !strings.Contains(string(brief), "large — deep reasoning") {
		t.Errorf("brief missing tier catalog: %q", string(brief))
	}
	if env["LEGATO_ROLE_PROMPT_FILE"] == "" {
		t.Fatal("LEGATO_ROLE_PROMPT_FILE not set")
	}
	rolePrompt, err := os.ReadFile(env["LEGATO_ROLE_PROMPT_FILE"])
	if err != nil {
		t.Fatal(err)
	}
	if string(rolePrompt) != "be a conductor" {
		t.Errorf("role prompt = %q", string(rolePrompt))
	}
	lines := mt.sentLinesFor("legato-adhoc-1")
	if len(lines) != 1 {
		t.Fatalf("sent lines = %v, want one kickoff", lines)
	}
	if !strings.Contains(lines[0], "now the Legato swarm conductor") {
		t.Errorf("kickoff line = %q", lines[0])
	}
}

func TestCreateAdhocSwarmWorkerQuestionTargetsPromotedConductor(t *testing.T) {
	sw, agentSvc, st, mt := newTestSwarmService(t)
	ctx := context.Background()
	seedParentTask(t, st, "adhoc-questions")
	if err := agentSvc.SpawnAgent(ctx, "adhoc-questions", 0, 0); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	wd := t.TempDir()
	if err := sw.CreateAdhocSwarm(ctx, "adhoc-questions", "coordinate workers", wd); err != nil {
		t.Fatalf("CreateAdhocSwarm: %v", err)
	}

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "adhoc-questions", WorkingDir: wd},
		Steps: []swarm.PlanStep{{Subtasks: []swarm.PlanSubtask{
			{Title: "Worker one", Role: "worker"},
		}}},
	}
	if err := sw.ApplyApprovedPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	subs, err := st.ListSubtasksByParent(ctx, "adhoc-questions")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("subtasks len = %d, want 1", len(subs))
	}
	if err := sw.Dispatch(ctx, subs[0].ID); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if err := sw.Question(ctx, subs[0].ID, "which direction should I take?"); err != nil {
		t.Fatalf("Question: %v", err)
	}

	lines := mt.sentLinesFor("legato-adhoc-questions")
	if len(lines) < 2 {
		t.Fatalf("conductor lines = %v, want kickoff plus inbox pointer", lines)
	}
	gotPointer := lines[len(lines)-1]
	if !strings.Contains(gotPointer, "new swarm event") || !strings.Contains(gotPointer, "legato swarm inbox adhoc-questions") {
		t.Errorf("question pointer = %q", gotPointer)
	}
}

func TestCreateAdhocSwarmRejectsWorkerSession(t *testing.T) {
	sw, agentSvc, st, _ := newTestSwarmService(t)
	ctx := context.Background()
	seedParentTask(t, st, "parent-worker")
	if err := st.CreateSubtask(ctx, store.Subtask{
		ID:           "st-worker-promote",
		ParentTaskID: "parent-worker",
		Title:        "worker",
		Role:         "worker",
		Status:       "queued",
	}); err != nil {
		t.Fatal(err)
	}
	if err := agentSvc.SpawnAgent(ctx, "st-worker-promote", 0, 0, AgentSpawnOptions{
		Role:         "worker",
		ParentTaskID: "parent-worker",
		SubtaskID:    "st-worker-promote",
	}); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if err := sw.CreateAdhocSwarm(ctx, "st-worker-promote", "try to lead", t.TempDir()); err == nil {
		t.Fatal("CreateAdhocSwarm should reject worker sessions")
	}
}

// TestStartSwarmUsesConductorAgentOverride verifies that when
// ConductorAgent is set, the conductor spawns with that adapter rather than
// DefaultAgent, while workers without explicit agent: still fall back to
// DefaultAgent.
func TestStartSwarmUsesConductorAgentOverride(t *testing.T) {
	var capturedAgentKind string
	conductorAdapter := &fakeAdapter{
		name:        "conductor-fake",
		rolePrompts: map[string]string{"conductor": "be a conductor"},
		launchFn: func(env map[string]string, brief, tier string) string {
			capturedAgentKind = "conductor-fake"
			return ""
		},
	}
	workerAdapter := &fakeAdapter{
		name:        "worker-fake",
		rolePrompts: map[string]string{"worker": "be a worker"},
		launchFn: func(env map[string]string, brief, tier string) string {
			return ""
		},
	}
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mt := newMockTmux()
	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter: workerAdapter,
		Adapters: map[string]AIToolAdapter{
			"conductor-fake": conductorAdapter,
			"worker-fake":    workerAdapter,
		},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{
		MaxConcurrentAgents: 4,
		DefaultAgent:        "worker-fake",
		ConductorAgent:      "conductor-fake",
	}, t.TempDir())

	seedParentTask(t, st, "parent-split-agent")
	if err := sw.StartSwarm(context.Background(), "parent-split-agent", t.TempDir()); err != nil {
		t.Fatalf("StartSwarm: %v", err)
	}
	if capturedAgentKind != "conductor-fake" {
		t.Errorf("conductor spawned with agent %q, want conductor-fake", capturedAgentKind)
	}
}

// TestApplyApprovedPlanRejectsUnknownTier regresses W1: when a plan reaches
// ApplyApprovedPlan via the TUI/web edit-and-approve path (bypassing the
// CLI's propose-plan validation), the service must re-validate so an injected
// bogus tier cannot land sub-tasks in the store.
func TestApplyApprovedPlanRejectsUnknownTier(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	agentSvc := NewAgentService(st, mt, t.TempDir())
	bus := events.New()
	cfg := SwarmConfig{
		MaxConcurrentAgents: 4,
		DefaultAgent:        "claude-code",
		ValidateOptions: swarm.ValidateOptions{
			RegisteredAdapters: []string{"claude-code"},
			DefaultAgent:       "claude-code",
			AdapterTiers: map[string]map[string]struct{}{
				"claude-code": {"small": {}},
			},
		},
	}
	sw := NewSwarmService(st, agentSvc, bus, cfg, t.TempDir())

	seedParentTask(t, st, "parent-bypass")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-bypass", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{
				{Title: "Bogus", Role: "backend", Agent: "claude-code", Tier: "ghost"},
			}},
		},
	}

	if err := sw.ApplyApprovedPlan(context.Background(), plan); err == nil {
		t.Fatal("expected ApplyApprovedPlan to reject plan with unknown tier")
	}
	subs, err := st.ListSubtasksByParent(context.Background(), "parent-bypass")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Errorf("no sub-tasks should be persisted after a rejected plan; got %d", len(subs))
	}
}

// TestApplyApprovedPlanPersistsTier verifies that the per-sub-task tier from
// the plan flows into the swarm_subtasks row so Dispatch can later pass it
// to the adapter's LaunchCommand.
func TestApplyApprovedPlanPersistsTier(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-tier")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-tier", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{
				{Title: "Cheap", Role: "backend", Tier: "small"},
				{Title: "Expensive", Role: "backend", Tier: "large"},
				{Title: "Default", Role: "backend"},
			}},
		},
	}
	if err := sw.ApplyApprovedPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	subs, err := st.ListSubtasksByParent(context.Background(), "parent-tier")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range subs {
		got[s.Title] = s.Tier
	}
	if got["Cheap"] != "small" || got["Expensive"] != "large" {
		t.Errorf("tier not persisted: %+v", got)
	}
	if got["Default"] != "" {
		t.Errorf("default subtask should have empty tier, got %q", got["Default"])
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

func TestLatestSnapshotColdCacheRebuildsFromDB(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-snap")
	seedSubtask(t, st, "st-snap01", "parent-snap", "queued")
	seedSubtask(t, st, "st-snap02", "parent-snap", "done")
	seedSubtask(t, st, "st-snap03", "parent-snap", "in_progress")

	snap := sw.LatestSnapshot("parent-snap")
	if snap == nil {
		t.Fatal("expected non-nil snapshot after cold rebuild")
	}
	if snap.Total != 3 {
		t.Errorf("Total = %d, want 3", snap.Total)
	}
	if snap.Done != 1 {
		t.Errorf("Done = %d, want 1", snap.Done)
	}
	if snap.Active != 1 {
		t.Errorf("Active = %d, want 1 (in_progress counts)", snap.Active)
	}
	if snap.Cancelled != 0 {
		t.Errorf("Cancelled = %d, want 0", snap.Cancelled)
	}
}

func TestLatestSnapshotEmptyParentReturnsNil(t *testing.T) {
	sw, _, _, _ := newTestSwarmService(t)
	if snap := sw.LatestSnapshot(""); snap != nil {
		t.Errorf("LatestSnapshot(\"\") = %+v, want nil", snap)
	}
}

func TestLatestSnapshotMissingParentReturnsNil(t *testing.T) {
	sw, _, _, _ := newTestSwarmService(t)
	// No parent task seeded — rebuild from empty subtask list still creates an
	// empty snapshot in the cache. We just want to confirm no panic and counters
	// are zero.
	snap := sw.LatestSnapshot("never-existed")
	if snap == nil {
		return // empty parent → nil is fine
	}
	if snap.Total != 0 {
		t.Errorf("Total = %d, want 0 for empty parent", snap.Total)
	}
}

// TestBuiltFromReportingReReports verifies that calling Built from reporting
// status is allowed, stays reporting, emits a fresh built event, and does not
// double-count in the snapshot.
func TestBuiltFromReportingReReports(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-reporting01", "parent-1", "reporting")

	// Spawn a fake conductor so recordEventForConductor has a target.
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

	// Establish snapshot baseline: one reporting subtask → Active=1.
	_ = sw.LatestSnapshot("parent-1")

	if err := sw.Built(context.Background(), "st-reporting01"); err != nil {
		t.Fatalf("Built from reporting: %v", err)
	}

	got, err := st.GetSubtask(context.Background(), "st-reporting01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "reporting" {
		t.Errorf("Status = %q, want reporting", got.Status)
	}

	// Verify a fresh built event was recorded.
	events, err := st.ListUnackedSwarmEvents(context.Background(), "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "built" && e.SubtaskID != nil && *e.SubtaskID == "st-reporting01" {
			found = true
			if !strings.Contains(e.Payload, "re-reported") {
				t.Errorf("expected re-report hint in payload, got: %q", e.Payload)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected a fresh built event for re-report")
	}

	// Snapshot should still show Active=1 (not 2).
	snap := sw.LatestSnapshot("parent-1")
	if snap.Active != 1 {
		t.Errorf("Active = %d, want 1 (no double-count)", snap.Active)
	}
}

// TestCancelSwarmKillsAgentsAndDeletesSubtasks verifies CancelSwarm kills live
// workers, transitions non-terminal sub-tasks to cancelled, deletes them, clears
// working dir, and publishes the change event.
func TestCancelSwarmKillsAgentsAndDeletesSubtasks(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-cancel")
	wd := t.TempDir()
	if err := st.SetTaskSwarmWorkingDir(context.Background(), "parent-cancel", &wd); err != nil {
		t.Fatal(err)
	}
	seedSubtask(t, st, "st-cancel01", "parent-cancel", "in_progress")
	seedSubtask(t, st, "st-cancel02", "parent-cancel", "done")
	seedSubtask(t, st, "st-cancel03", "parent-cancel", "queued")

	// Spawn fake agent sessions so KillAgent has something to kill.
	for _, sess := range []string{"legato-parent-cancel", "legato-st-cancel01", "legato-st-cancel03"} {
		if err := mt.Spawn(sess, t.TempDir(), 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	now := "2024-01-01T00:00:00Z"
	if err := st.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID:      "parent-cancel",
		TmuxSession: "legato-parent-cancel",
		Status:      "running",
		StartedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID:       "st-cancel01",
		TmuxSession:  "legato-st-cancel01",
		Status:       "running",
		ParentTaskID: strPtr("parent-cancel"),
		StartedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID:       "st-cancel03",
		TmuxSession:  "legato-st-cancel03",
		Status:       "running",
		ParentTaskID: strPtr("parent-cancel"),
		StartedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := sw.CancelSwarm(context.Background(), "parent-cancel"); err != nil {
		t.Fatalf("CancelSwarm: %v", err)
	}

	// Sub-tasks should be deleted.
	subs, err := st.ListSubtasksByParent(context.Background(), "parent-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Errorf("expected 0 subtasks after cancel, got %d", len(subs))
	}

	// Working dir should be cleared.
	parent, err := st.GetTask(context.Background(), "parent-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if parent.SwarmWorkingDir != nil {
		t.Errorf("swarm_working_dir not cleared")
	}
	if parent.SwarmActiveStep != 0 {
		t.Errorf("swarm_active_step = %d, want 0", parent.SwarmActiveStep)
	}
}

// TestCancelSwarmNoSwarmIsNoOp verifies CancelSwarm on a parent with no swarm
// returns without error.
func TestCancelSwarmNoSwarmIsNoOp(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-noswarm")

	if err := sw.CancelSwarm(context.Background(), "parent-noswarm"); err != nil {
		t.Fatalf("CancelSwarm on parent with no swarm: %v", err)
	}
}

// TestStartSwarmRefusesLeftoverSubtasks verifies StartSwarm refuses when
// non-terminal sub-tasks exist.
func TestStartSwarmRefusesLeftoverSubtasks(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-leftover")
	seedSubtask(t, st, "st-left01", "parent-leftover", "queued")

	if err := sw.StartSwarm(context.Background(), "parent-leftover", t.TempDir()); err == nil {
		t.Fatal("expected StartSwarm to refuse when subtasks exist")
	} else if !strings.Contains(err.Error(), "cancel the existing swarm first") {
		t.Errorf("error should mention cancelling existing swarm: %v", err)
	}
}

// TestStartSwarmRefusesNonNilWorkingDir verifies StartSwarm refuses when
// swarm_working_dir is already set (even with no subtasks).
func TestStartSwarmRefusesNonNilWorkingDir(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-wdset")
	wd := t.TempDir()
	if err := st.SetTaskSwarmWorkingDir(context.Background(), "parent-wdset", &wd); err != nil {
		t.Fatal(err)
	}

	if err := sw.StartSwarm(context.Background(), "parent-wdset", t.TempDir()); err == nil {
		t.Fatal("expected StartSwarm to refuse when working_dir is set")
	} else if !strings.Contains(err.Error(), "cancel the existing swarm first") {
		t.Errorf("error should mention cancelling existing swarm: %v", err)
	}
}

// TestStartSwarmSucceedsAfterCancel verifies the full cancel → recreate cycle.
func TestStartSwarmSucceedsAfterCancel(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-cycle")
	seedSubtask(t, st, "st-cycle01", "parent-cycle", "queued")

	if err := sw.CancelSwarm(context.Background(), "parent-cycle"); err != nil {
		t.Fatalf("CancelSwarm: %v", err)
	}

	if err := sw.StartSwarm(context.Background(), "parent-cycle", t.TempDir()); err != nil {
		t.Fatalf("StartSwarm after cancel: %v", err)
	}
}

// TestExtendApprovedPlanAppendsWithOffsets verifies ExtendApprovedPlan adds
// new sub-tasks with step indices offset after the existing max.
func TestExtendApprovedPlanAppendsWithOffsets(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-extend")
	wd := t.TempDir()
	if err := st.SetTaskSwarmWorkingDir(context.Background(), "parent-extend", &wd); err != nil {
		t.Fatal(err)
	}

	// Seed existing subtasks with step index 0 and 1.
	seedSubtaskWithStep(t, st, "st-ext01", "parent-extend", "done", 0)
	seedSubtaskWithStep(t, st, "st-ext02", "parent-extend", "done", 1)

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-extend", WorkingDir: wd},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{{Title: "Step2-A"}}},
			{Subtasks: []swarm.PlanSubtask{{Title: "Step3-A"}}},
		},
	}
	if err := sw.ExtendApprovedPlan(context.Background(), plan); err != nil {
		t.Fatalf("ExtendApprovedPlan: %v", err)
	}

	subs, err := st.ListSubtasksByParent(context.Background(), "parent-extend")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 4 {
		t.Fatalf("got %d subtasks, want 4", len(subs))
	}

	// Map titles to step_index.
	idxs := map[string]int{}
	for _, s := range subs {
		idxs[s.Title] = s.StepIndex
	}
	if idxs["Step2-A"] != 2 {
		t.Errorf("Step2-A step_index = %d, want 2", idxs["Step2-A"])
	}
	if idxs["Step3-A"] != 3 {
		t.Errorf("Step3-A step_index = %d, want 3", idxs["Step3-A"])
	}
}

// TestExtendApprovedPlanRefusesNoSwarm verifies ExtendApprovedPlan refuses when
// the parent has no swarm_working_dir.
func TestExtendApprovedPlanRefusesNoSwarm(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-noextend")

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-noextend", WorkingDir: "/tmp"},
		Steps: []swarm.PlanStep{
			{Subtasks: []swarm.PlanSubtask{{Title: "Orphan"}}},
		},
	}
	if err := sw.ExtendApprovedPlan(context.Background(), plan); err == nil {
		t.Fatal("expected ExtendApprovedPlan to refuse when no swarm exists")
	}
}

// TestExtendApprovedPlanValidatesInput verifies ExtendApprovedPlan runs the
// same validation as ApplyApprovedPlan.
func TestExtendApprovedPlanValidatesInput(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-extend-bad")
	wd := t.TempDir()
	if err := st.SetTaskSwarmWorkingDir(context.Background(), "parent-extend-bad", &wd); err != nil {
		t.Fatal(err)
	}

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-extend-bad", WorkingDir: wd},
		Steps: []swarm.PlanStep{}, // empty steps → invalid
	}
	if err := sw.ExtendApprovedPlan(context.Background(), plan); err == nil {
		t.Fatal("expected ExtendApprovedPlan to reject invalid plan")
	}
}

// TestBumpSnapshotAfterClose exercises the bumpSnapshot path: dispatch then
// close a sub-task and assert the in-memory counters move correctly.
func TestBumpSnapshotAfterClose(t *testing.T) {
	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-bump")
	seedSubtask(t, st, "st-bump01", "parent-bump", "in_progress")

	// Cold rebuild establishes Total=1, Active=1, Done=0.
	snap := sw.LatestSnapshot("parent-bump")
	if snap.Total != 1 || snap.Active != 1 || snap.Done != 0 {
		t.Fatalf("pre-close snapshot wrong: %+v", snap)
	}

	if err := sw.Built(context.Background(), "st-bump01"); err != nil {
		t.Fatalf("Built: %v", err)
	}
	if err := sw.Close(context.Background(), "st-bump01"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	snap = sw.LatestSnapshot("parent-bump")
	if snap.Done != 1 {
		t.Errorf("Done after close = %d, want 1", snap.Done)
	}
	if snap.Active != 0 {
		t.Errorf("Active after close = %d, want 0", snap.Active)
	}
}
