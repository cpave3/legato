package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// newSlotFixture is an agent service with Claude Code, Codex and Chimera over
// a mock tmux and a real store.
func newSlotFixture(t *testing.T) restartFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := store.New(filepath.Join(t.TempDir(), "slots.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	bus := &recordingPublisher{}
	claude := hooks.NewClaudeCodeAdapter("legato")
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: claude,
		Adapters: map[string]AIToolAdapter{
			"claude-code": claude, "codex": hooks.NewCodexAdapter("legato"), "chimera": hooks.NewChimeraAdapter("legato"),
		},
		DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond, EventBus: bus,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	return restartFixture{svc: svc, store: s, tmux: mt, bus: bus}
}

func TestAgentsInTwoSlotsOfOneTaskKeepTheirOwnSessions(t *testing.T) {
	f := newSlotFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-s")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "codex", WorkingDir: cwd, Slot: "reviewer"}); err != nil {
		t.Fatalf("starting the reviewer beside the author: %v", err)
	}

	if !f.tmux.sessionAlive("legato-task-s") || !f.tmux.sessionAlive("legato-task-s_reviewer") {
		t.Fatalf("tmux sessions alive: main %v, reviewer %v", f.tmux.sessionAlive("legato-task-s"), f.tmux.sessionAlive("legato-task-s_reviewer"))
	}
	if env := f.tmux.envVarsFor("legato-task-s_reviewer"); env["LEGATO_SLOT"] != "reviewer" {
		t.Fatalf("reviewer's LEGATO_SLOT = %q", env["LEGATO_SLOT"])
	}
	agents, err := f.svc.ListAgents(ctx)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents = %+v, %v; want two", agents, err)
	}
	slots := map[string]AgentSession{}
	for _, agent := range agents {
		slots[agent.Slot] = agent
	}
	if slots["main"].AgentKind != "claude-code" || slots["reviewer"].AgentKind != "codex" || slots["main"].ExecutionID == slots["reviewer"].ExecutionID {
		t.Fatalf("agents by slot = %+v", slots)
	}

	// A second reviewer is refused while one runs.
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "codex", WorkingDir: cwd, Slot: "reviewer"}); err == nil {
		t.Fatal("a second agent started in the reviewer slot")
	}

	// The task's agent, killed without a slot, is the main slot's.
	if err := f.svc.KillAgent(ctx, "task-s"); err != nil {
		t.Fatal(err)
	}
	if f.tmux.sessionAlive("legato-task-s") || !f.tmux.sessionAlive("legato-task-s_reviewer") {
		t.Fatal("killing the task's agent did not stop the main slot only")
	}

	// A task-level end stops every slot.
	if err := f.svc.(TaskAgentsStopper).StopTaskAgents(ctx, "task-s"); err != nil {
		t.Fatal(err)
	}
	if f.tmux.sessionAlive("legato-task-s_reviewer") {
		t.Fatal("the reviewer still runs after the task's agents were stopped")
	}
	if agents, _ := f.svc.ListAgents(ctx); len(agents) != 0 {
		t.Fatalf("agents after stopping the task's agents = %+v", agents)
	}
}

func TestSpawnRefusesASlotNameTmuxCannotKeep(t *testing.T) {
	f := newSlotFixture(t)
	createTask(t, f.store, "task-s")
	for _, slot := range []string{"Reviewer", "re.view", "a b", "x:y"} {
		if err := f.svc.SpawnAgent(context.Background(), "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", Slot: slot}); err == nil {
			t.Fatalf("slot %q was accepted", slot)
		}
	}
}

// The bug slots exist to prevent: a reviewer's conversation must never
// replace the author's, so resuming the main slot finds the author's.
func TestResumingTheMainSlotAfterAReviewerPicksUpTheAuthorsConversation(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		t.Run(kind, func(t *testing.T) {
			f := newSlotFixture(t)
			ctx := context.Background()
			createTask(t, f.store, "task-s")
			cwd := t.TempDir()
			if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: kind, WorkingDir: cwd}); err != nil {
				t.Fatal(err)
			}
			saveConversation(t, f.store, "task-s", store.SlotMain, kind, "author-conversation", cwd)
			if err := f.svc.KillAgent(ctx, "task-s"); err != nil {
				t.Fatal(err)
			}
			if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: kind, WorkingDir: cwd, Slot: "reviewer"}); err != nil {
				t.Fatal(err)
			}
			saveConversation(t, f.store, "task-s", "reviewer", kind, "reviewer-conversation", cwd)
			if err := f.svc.(TaskAgentsStopper).StopTaskAgents(ctx, "task-s"); err != nil {
				t.Fatal(err)
			}

			if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: kind, SessionPolicy: "resume"}); err != nil {
				t.Fatal(err)
			}

			launch := f.lastLaunch(t, "task-s")
			if !strings.Contains(launch, "author-conversation") || strings.Contains(launch, "reviewer-conversation") {
				t.Fatalf("main slot's resume launch = %q, want the author's conversation", launch)
			}
		})
	}
	t.Run("chimera", func(t *testing.T) {
		f := newSlotFixture(t)
		ctx := context.Background()
		createTask(t, f.store, "task-s")
		if err := f.store.SetTaskChimeraSessionID(ctx, "task-s", "author-chimera"); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "chimera", Slot: "reviewer"}); err != nil {
			t.Fatal(err)
		}
		env := f.tmux.envVarsFor("legato-task-s_reviewer")
		if env["LEGATO_CHIMERA_SESSION_NAME"] != "task-s_reviewer" || env["LEGATO_CHIMERA_SESSION_ID"] == "task-s" {
			t.Fatalf("reviewer's Chimera session env = %v; it must not name the author's session", env)
		}
		if err := f.store.SetChimeraResumeInSlot(ctx, "task-s", "reviewer", "reviewer-chimera"); err != nil {
			t.Fatal(err)
		}

		saved, err := f.svc.GetTaskResumeSession(ctx, "task-s", "chimera")
		if err != nil || saved == nil || saved.ID != "author-chimera" {
			t.Fatalf("main slot's Chimera session = %+v, %v; want author-chimera", saved, err)
		}
	})
}

// saveConversation records the conversation the running agent of a slot
// reported, as its bridge does.
func saveConversation(t *testing.T, s *store.Store, taskID, slot, kind, conversation, cwd string) {
	t.Helper()
	ctx := context.Background()
	session, err := s.GetAgentSessionInSlot(ctx, taskID, slot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, taskID, session.ID, session.ExecutionID, kind, conversation, cwd, ""); err != nil {
		t.Fatal(err)
	}
}

func TestKillingAnAgentInANamedSlotLeavesTheMainSlotRunning(t *testing.T) {
	f := newSlotFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-s")
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", Slot: "reviewer"}); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.(SlotAgents).KillAgentInSlot(ctx, "task-s", "reviewer"); err != nil {
		t.Fatal(err)
	}

	if !f.tmux.sessionAlive("legato-task-s") || f.tmux.sessionAlive("legato-task-s_reviewer") {
		t.Fatal("killing the reviewer did not leave the author alone")
	}
	if _, err := f.store.GetAgentSessionInSlot(ctx, "task-s", "reviewer"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reviewer slot after kill: %v", err)
	}
	// Listeners that act on the task's agent ending hear nothing of it.
	if len(f.bus.calls) != 0 || len(f.bus.slotDeaths) != 1 || f.bus.slotDeaths[0] != "task-s/reviewer" {
		t.Fatalf("died events = %+v, slot deaths = %v", f.bus.calls, f.bus.slotDeaths)
	}
}

type recordingStopper struct{ stopped []string }

func (r *recordingStopper) StopTaskAgents(_ context.Context, taskID string) error {
	r.stopped = append(r.stopped, taskID)
	return nil
}

func TestDeletingOrArchivingATaskStopsEveryAgentSlot(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	ctx := context.Background()
	seedColumns(t, s)
	for _, id := range []string{"del", "arch", "bulk"} {
		if err := s.CreateTask(ctx, store.Task{ID: id, Title: id, Status: "Done"}); err != nil {
			t.Fatal(err)
		}
	}
	stopper := &recordingStopper{}
	svc.(TaskAgentsHolder).SetTaskAgents(stopper)

	if err := svc.DeleteTask(ctx, "del"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveTask(ctx, "arch"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ArchiveDoneCards(ctx); err != nil {
		t.Fatal(err)
	}

	if strings.Join(stopper.stopped, ",") != "del,arch,bulk" {
		t.Fatalf("tasks whose agents were stopped = %v", stopper.stopped)
	}
}

func TestAReadOnlyReviewerIsListedAsAPeerButNotMessaged(t *testing.T) {
	f := newSlotFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-s")
	createTask(t, f.store, "other")
	repo := initTestRepo(t)
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: repo}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-s", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: repo, Slot: "reviewer", ReadOnlyCheckout: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "other", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: repo}); err != nil {
		t.Fatal(err)
	}
	author, _ := f.store.GetAgentSessionByTaskID(ctx, "task-s")

	peers, err := f.svc.ListPeers(ctx, "task-s", author.ExecutionID, "")
	if err != nil {
		t.Fatal(err)
	}
	var reviewer *AgentPeer
	for i := range peers {
		if peers[i].Slot == "reviewer" {
			reviewer = &peers[i]
		}
	}
	if len(peers) != 2 || reviewer == nil || !reviewer.ReadOnly || reviewer.TaskID != "task-s" {
		t.Fatalf("the author's peers = %+v; want the other task and its own read-only reviewer", peers)
	}

	result, err := f.svc.BroadcastToPeers(ctx, "other", "", "", "heads up")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Delivered, ",") != "task-s" {
		t.Fatalf("delivered = %v; want the author only", result.Delivered)
	}
	for _, line := range f.tmux.sentLinesFor("legato-task-s_reviewer") {
		if strings.Contains(line, "heads up") {
			t.Fatal("the read-only reviewer was messaged")
		}
	}
}
