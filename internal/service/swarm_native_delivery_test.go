package service

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/store"
)

// bridgeSwarmParticipant gives a swarm participant a running session bound
// to a bridge, as a Claude Code worker or conductor has once its mod loads.
func bridgeSwarmParticipant(t *testing.T, st *store.Store, mt *mockTmux, taskID string) <-chan map[string]any {
	t.Helper()
	ctx := context.Background()
	session := "legato-" + taskID
	mt.sessions[session] = true
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{
		TaskID: taskID, TmuxSession: session, Command: "claude", AgentKind: "claude-code", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	commands := &lineWriter{lines: make(chan map[string]any, 32)}
	bridge := claudebridge.New(claudebridge.Options{TaskID: taskID, Commands: commands})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	if err := st.UpdateChimeraDiscovery(ctx, taskID, taskID, server.URL); err != nil {
		t.Fatal(err)
	}
	return commands.lines
}

func nextBridgeCommand(t *testing.T, commands <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case command := <-commands:
		return command
	default:
		t.Fatal("no command reached the bridge")
		return nil
	}
}

func seedSwarmWorker(t *testing.T, st *store.Store, parentID, subtaskID string) {
	t.Helper()
	seedParentTask(t, st, parentID)
	if err := st.CreateSubtask(context.Background(), store.Subtask{
		ID: subtaskID, ParentTaskID: parentID, Title: "API", Role: "worker", Status: "in_progress", AgentKind: "claude-code",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSwarmMessageReachesABridgedWorkerWithoutTheTerminal(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedSwarmWorker(t, st, "parent-1", "st-worker-01")
	commands := bridgeSwarmParticipant(t, st, mt, "st-worker-01")

	if err := sw.Message(context.Background(), "st-worker-01", "rebase onto main", false); err != nil {
		t.Fatal(err)
	}

	if command := nextBridgeCommand(t, commands); command["type"] != "submit" || command["text"] != "rebase onto main" {
		t.Fatalf("command = %v", command)
	}
	if lines := mt.sentLinesFor("legato-st-worker-01"); len(lines) != 0 {
		t.Fatalf("message was also typed into the terminal: %v", lines)
	}
}

func TestUrgentSwarmMessageInterruptsABridgedWorkerDirectly(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedSwarmWorker(t, st, "parent-1", "st-worker-01")
	commands := bridgeSwarmParticipant(t, st, mt, "st-worker-01")

	if err := sw.Message(context.Background(), "st-worker-01", "stop: wrong branch", true); err != nil {
		t.Fatal(err)
	}

	if command := nextBridgeCommand(t, commands); command["type"] != "interrupt" {
		t.Fatalf("first command = %v, want the turn interrupted", command)
	}
	if command := nextBridgeCommand(t, commands); command["text"] != "stop: wrong branch" {
		t.Fatalf("second command = %v", command)
	}
	if keys := mt.sentKeysFor("legato-st-worker-01"); len(keys) != 0 {
		t.Fatalf("interrupt keys were also sent to the terminal: %v", keys)
	}
}

func TestSwarmMessageToABridgedConductorSkipsTheTerminal(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	commands := bridgeSwarmParticipant(t, st, mt, "parent-1")

	if err := sw.MessageParent(context.Background(), "parent-1", "please summarise progress", false); err != nil {
		t.Fatal(err)
	}

	if command := nextBridgeCommand(t, commands); command["text"] != "please summarise progress" {
		t.Fatalf("command = %v", command)
	}
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 0 {
		t.Fatalf("message was also typed into the terminal: %v", lines)
	}
}

func TestSwarmBroadcastCountsBridgedAndTerminalWorkersAlike(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedSwarmWorker(t, st, "parent-1", "st-worker-01")
	commands := bridgeSwarmParticipant(t, st, mt, "st-worker-01")
	ctx := context.Background()
	if err := st.CreateSubtask(ctx, store.Subtask{
		ID: "st-worker-02", ParentTaskID: "parent-1", Title: "Web", Role: "worker", Status: "in_progress", AgentKind: "codex",
	}); err != nil {
		t.Fatal(err)
	}
	mt.sessions["legato-st-worker-02"] = true
	if _, err := st.InsertAgentSessionReturningID(ctx, store.AgentSession{
		TaskID: "st-worker-02", TmuxSession: "legato-st-worker-02", Command: "codex", AgentKind: "codex", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	delivered, err := sw.Broadcast(ctx, "parent-1", "main moved; rebase", false)
	if err != nil {
		t.Fatal(err)
	}

	if delivered != 2 {
		t.Fatalf("delivered = %d, want both workers", delivered)
	}
	if command := nextBridgeCommand(t, commands); command["text"] != "main moved; rebase" {
		t.Fatalf("bridged worker command = %v", command)
	}
	if lines := mt.sentLinesFor("legato-st-worker-02"); len(lines) != 1 || lines[0] != "main moved; rebase" {
		t.Fatalf("terminal worker lines = %v", lines)
	}
}

func TestWorkerEventsReachABridgedConductorDirectly(t *testing.T) {
	sw, _, st, mt := newTestSwarmService(t)
	seedSwarmWorker(t, st, "parent-1", "st-worker-01")
	mt.sessions["legato-st-worker-01"] = true
	if _, err := st.InsertAgentSessionReturningID(context.Background(), store.AgentSession{
		TaskID: "st-worker-01", TmuxSession: "legato-st-worker-01", Command: "claude", AgentKind: "claude-code", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	commands := bridgeSwarmParticipant(t, st, mt, "parent-1")

	if err := sw.Question(context.Background(), "st-worker-01", "which auth scheme?"); err != nil {
		t.Fatal(err)
	}

	command := nextBridgeCommand(t, commands)
	text, _ := command["text"].(string)
	if !strings.Contains(text, "new swarm event") || !strings.Contains(text, "legato swarm inbox parent-1") {
		t.Fatalf("command = %v, want the inbox pointer", command)
	}
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 0 {
		t.Fatalf("event pointer was also typed into the terminal: %v", lines)
	}
}

func TestPromotingABridgedSessionToConductorSubmitsItsKickoffDirectly(t *testing.T) {
	adapter := &fakeAdapter{name: "fake", rolePrompts: map[string]string{"conductor": "be a conductor"}}
	sw, agentSvc, st, mt := newTestSwarmServiceWithAdapter(t, adapter, SwarmConfig{
		MaxConcurrentAgents: 4, Worker: SwarmParticipantConfig{Agent: "fake"},
	})
	ctx := context.Background()
	seedParentTask(t, st, "adhoc-1")
	if err := agentSvc.SpawnAgent(ctx, "adhoc-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	commands := bindBridge(t, st, "adhoc-1")
	typedBefore := len(mt.sentLinesFor("legato-adhoc-1"))

	if err := sw.CreateAdhocSwarm(ctx, "adhoc-1", "split this work across agents", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	command := awaitBridgeCommand(t, commands)
	text, _ := command["text"].(string)
	if !strings.Contains(text, "You are now the Legato swarm conductor") {
		t.Fatalf("command = %v, want the conductor kickoff", command)
	}
	if typed := mt.sentLinesFor("legato-adhoc-1"); len(typed) != typedBefore {
		t.Fatalf("the kickoff was also typed into the terminal: %v", typed[typedBefore:])
	}
}
