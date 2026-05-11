package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// multiKeyAdapter is a test double that returns two interrupt keys.
type multiKeyAdapter struct {
	fakeAdapter
}

func (a *multiKeyAdapter) InterruptKeys() []string { return []string{"Escape", "Enter"} }

// plainAdapter implements the minimum AIToolAdapter interface but does NOT
// implement InterruptAdapter. Used to verify graceful degradation.
type plainAdapter struct{ name string }

func (a *plainAdapter) Name() string                                 { return a.name }
func (a *plainAdapter) InstallHooks(string) error                    { return nil }
func (a *plainAdapter) UninstallHooks(string) error                  { return nil }
func (a *plainAdapter) EnvVars(taskID, socket string) map[string]string { return nil }

func TestMessageUrgentSendsInterruptKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	adpt := &fakeAdapter{name: "fake", rolePrompts: map[string]string{"worker": "do work"}}
	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter:  adpt,
		Adapters: map[string]AIToolAdapter{"fake": adpt},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{MaxConcurrentAgents: 4, DefaultAgent: "fake"}, t.TempDir())

	seedParentTask(t, st, "parent-msg")
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID: "st-msg-01", ParentTaskID: "parent-msg", Title: "sub", Role: "worker",
		Status: "in_progress", AgentKind: "fake",
	}); err != nil {
		t.Fatal(err)
	}

	sessionName := "legato-42"
	mt.sessions[sessionName] = true
	ctx := context.Background()
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "st-msg-01", TmuxSession: sessionName, Command: "fake", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	// Non-urgent message should send only the text via SendKeysLine.
	if err := sw.Message(ctx, "st-msg-01", "hello", false); err != nil {
		t.Fatalf("Message(non-urgent): %v", err)
	}
	if keys := mt.sentKeysFor(sessionName); len(keys) != 0 {
		t.Errorf("non-urgent: sentKeys = %v, want none", keys)
	}

	// Urgent message should send Escape first, then text.
	if err := sw.Message(ctx, "st-msg-01", "hello", true); err != nil {
		t.Fatalf("Message(urgent): %v", err)
	}
	if keys := mt.sentKeysFor(sessionName); len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("urgent: sentKeys = %v, want [Escape]", keys)
	}
}

func TestMessageParentUrgentSendsInterruptKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	adpt := &fakeAdapter{name: "fake", rolePrompts: map[string]string{"conductor": "be conductor"}}
	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter:  adpt,
		Adapters: map[string]AIToolAdapter{"fake": adpt},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{MaxConcurrentAgents: 4, DefaultAgent: "fake", ConductorAgent: "fake"}, t.TempDir())

	seedParentTask(t, st, "parent-conductor")
	if err := sw.StartSwarm(context.Background(), "parent-conductor", t.TempDir()); err != nil {
		t.Fatalf("StartSwarm: %v", err)
	}

	session, err := st.GetAgentSessionByTaskID(context.Background(), "parent-conductor")
	if err != nil {
		t.Fatalf("GetAgentSessionByTaskID: %v", err)
	}

	if err := sw.MessageParent(context.Background(), "parent-conductor", "urgent", true); err != nil {
		t.Fatalf("MessageParent(urgent): %v", err)
	}
	if keys := mt.sentKeysFor(session.TmuxSession); len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("sentKeys = %v, want [Escape]", keys)
	}
}

func TestBroadcastUrgentSendsInterruptPerTarget(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	adpt := &fakeAdapter{name: "fake", rolePrompts: map[string]string{"worker": "do work"}}
	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter:  adpt,
		Adapters: map[string]AIToolAdapter{"fake": adpt},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{MaxConcurrentAgents: 4, DefaultAgent: "fake"}, t.TempDir())

	seedParentTask(t, st, "parent-bc")
	for _, id := range []string{"st-bc-01", "st-bc-02"} {
		if err := st.CreateSubtask(context.Background(), store.Subtask{
			ID: id, ParentTaskID: "parent-bc", Title: "sub", Role: "worker",
			Status: "in_progress", AgentKind: "fake",
		}); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	for _, p := range []struct{ id, sess string }{
		{"st-bc-01", "legato-s1"},
		{"st-bc-02", "legato-s2"},
	} {
		mt.sessions[p.sess] = true
		if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: p.id, TmuxSession: p.sess, Command: "cmd", Status: "running"}); err != nil {
			t.Fatal(err)
		}
	}

	// Urgent broadcast should send interrupts to both targets.
	if _, err := sw.Broadcast(ctx, "parent-bc", "hello", true); err != nil {
		t.Fatalf("Broadcast(urgent): %v", err)
	}
	for _, sess := range []string{"legato-s1", "legato-s2"} {
		if keys := mt.sentKeysFor(sess); len(keys) != 1 || keys[0] != "Escape" {
			t.Errorf("%s: sentKeys = %v, want [Escape]", sess, keys)
		}
	}

	// Non-urgent broadcast should send zero interrupt keys.
	mt = newMockTmux() // fresh mock to clear state
	mt.sessions["legato-s1"] = true
	mt.sessions["legato-s2"] = true
	if _, err := sw.Broadcast(ctx, "parent-bc", "hello", false); err != nil {
		t.Fatalf("Broadcast(non-urgent): %v", err)
	}
	for _, sess := range []string{"legato-s1", "legato-s2"} {
		if keys := mt.sentKeysFor(sess); len(keys) != 0 {
			t.Errorf("%s: non-urgent sentKeys = %v, want none", sess, keys)
		}
	}
}

func TestMessageUrgentFallsBackWhenAdapterNotImplemented(t *testing.T) {
	// Uses the default service path (no adapter) so InterruptAdapter is not
	// available. The urgent flag should silently degrade to normal delivery.
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-fb")
	seedSubtask(t, st, "st-fb-01", "parent-fb", "in_progress")

	mt.sessions["legato-99"] = true
	if _, err := st.InsertAgentSessionReturningID(context.Background(), store.AgentSession{TaskID: "st-fb-01", TmuxSession: "legato-99", Command: "cmd", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	if err := sw.Message(context.Background(), "st-fb-01", "hello", true); err != nil {
		t.Fatalf("Message(urgent, no adapter): %v", err)
	}
	if keys := mt.sentKeysFor("legato-99"); len(keys) != 0 {
		t.Errorf("sentKeys = %v, want none (adapter does not implement InterruptAdapter)", keys)
	}
}

func TestMessageUrgentSendsMultipleInterruptKeys(t *testing.T) {
	// Verifies that when an adapter returns multiple interrupt keys, each is
	// sent in order with a gap between them.
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()
	adpt := &multiKeyAdapter{fakeAdapter{name: "multi", rolePrompts: map[string]string{"worker": "do work"}}}
	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter:  adpt,
		Adapters: map[string]AIToolAdapter{"multi": adpt},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{MaxConcurrentAgents: 4, DefaultAgent: "multi"}, t.TempDir())

	seedParentTask(t, st, "parent-multi")
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID: "st-multi-01", ParentTaskID: "parent-multi", Title: "sub", Role: "worker",
		Status: "in_progress", AgentKind: "multi",
	}); err != nil {
		t.Fatal(err)
	}

	sessionName := "legato-multi"
	mt.sessions[sessionName] = true
	ctx := context.Background()
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "st-multi-01", TmuxSession: sessionName, Command: "fake", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	if err := sw.Message(ctx, "st-multi-01", "hello", true); err != nil {
		t.Fatalf("Message(urgent): %v", err)
	}
	keys := mt.sentKeysFor(sessionName)
	if len(keys) != 2 || keys[0] != "Escape" || keys[1] != "Enter" {
		t.Errorf("sentKeys = %v, want [Escape Enter]", keys)
	}
}

func TestBroadcastUrgentMixedAdapters(t *testing.T) {
	// One worker uses an adapter that implements InterruptAdapter; another
	// uses a plain adapter that does not. Only the capable worker should
	// receive interrupt keys; both should receive the message.
	dbPath := filepath.Join(t.TempDir(), "swarm.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mt := newMockTmux()

	interruptAdpt := &fakeAdapter{name: "interrupt", rolePrompts: map[string]string{"worker": "w"}}
	plainAdpt := &plainAdapter{name: "plain"} // does NOT implement InterruptAdapter

	agents := NewAgentService(st, mt, t.TempDir(), AgentServiceOptions{
		Adapter: interruptAdpt,
		Adapters: map[string]AIToolAdapter{
			"interrupt": interruptAdpt,
			"plain":     plainAdpt,
		},
	})
	bus := events.New()
	sw := NewSwarmService(st, agents, bus, SwarmConfig{MaxConcurrentAgents: 4, DefaultAgent: "interrupt"}, t.TempDir())

	seedParentTask(t, st, "parent-mixed")
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID: "st-mix-01", ParentTaskID: "parent-mixed", Title: "sub", Role: "worker",
		Status: "in_progress", AgentKind: "interrupt",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID: "st-mix-02", ParentTaskID: "parent-mixed", Title: "sub", Role: "worker",
		Status: "in_progress", AgentKind: "plain",
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	mt.sessions["legato-capable"] = true
	mt.sessions["legato-plain"] = true
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "st-mix-01", TmuxSession: "legato-capable", Command: "cmd", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "st-mix-02", TmuxSession: "legato-plain", Command: "cmd", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	count, err := sw.Broadcast(ctx, "parent-mixed", "hello", true)
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if keys := mt.sentKeysFor("legato-capable"); len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("capable: sentKeys = %v, want [Escape]", keys)
	}
	if keys := mt.sentKeysFor("legato-plain"); len(keys) != 0 {
		t.Errorf("plain: sentKeys = %v, want none (adapter does not implement InterruptAdapter)", keys)
	}
}
