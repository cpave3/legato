package service

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/store"
)

// nativeLaunchAdapter is an adapter whose sessions bind a native endpoint on
// their own shortly after launch, as Claude Code sessions do once the Legato
// mod has started their bridge.
type nativeLaunchAdapter struct {
	fakeAdapter
}

func (a *nativeLaunchAdapter) BindsNativeEndpoint() bool { return true }

func newNativeLaunchService(t *testing.T, workDir string) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	adapter := &nativeLaunchAdapter{fakeAdapter{
		name:        "claude-code",
		rolePrompts: map[string]string{"conductor": "you are the conductor"},
		launchFn:    func(map[string]string, string, string, string) string { return "claude" },
	}}
	svc := NewAgentService(s, mt, workDir, AgentServiceOptions{Adapter: adapter, BriefKickoffDelay: time.Millisecond})
	return svc, s, mt
}

// bindBridge stands in for the session's bridge coming up: the moment the
// agent's native endpoint is bound.
func bindBridge(t *testing.T, s *store.Store, taskID string) <-chan map[string]any {
	t.Helper()
	commands := &lineWriter{lines: make(chan map[string]any, 32)}
	bridge := claudebridge.New(claudebridge.Options{TaskID: taskID, Commands: commands})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	if err := s.UpdateChimeraDiscovery(context.Background(), taskID, taskID, server.URL); err != nil {
		t.Fatal(err)
	}
	return commands.lines
}

func awaitBridgeCommand(t *testing.T, commands <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case command := <-commands:
		return command
	case <-time.After(3 * time.Second):
		t.Fatal("no command reached the bridge")
		return nil
	}
}

func TestLaunchKickoffWaitsForTheSessionsBridgeAndIsSubmittedDirectly(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	// Claude Code is still starting. Typing the kickoff now is how it used to
	// be lost: the text lands in the prompt box and the Enter goes nowhere.
	time.Sleep(60 * time.Millisecond)
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 1 || lines[0] != "claude" {
		t.Fatalf("lines typed while the session was starting = %v, want only the launch command", lines)
	}

	commands := bindBridge(t, s, "parent-1")

	if command := awaitBridgeCommand(t, commands); command["type"] != "submit" || command["text"] != briefKickoffMessage {
		t.Fatalf("command = %v, want the kickoff submitted as a prompt", command)
	}
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 1 {
		t.Fatalf("the kickoff was also typed into the terminal: %v", lines)
	}
}

func TestLaunchKickoffIsTypedWhenNoBridgeAppears(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	// A session that never loads the mod (an older Claude Code, a failed
	// load) still gets its kickoff the way it always did.
	svc.(*agentService).nativeBindTimeout = 40 * time.Millisecond
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(mt.sentLinesFor("legato-parent-1")) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("kickoff never typed; lines = %v", mt.sentLinesFor("legato-parent-1"))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := mt.sentLinesFor("legato-parent-1")[1]; got != briefKickoffMessage {
		t.Fatalf("typed line = %q, want the kickoff", got)
	}
}

func TestPeerNoticeToANewClaudeSessionWaitsForItsBridge(t *testing.T) {
	repo := initTestRepo(t)
	svc, s, mt := newNativeLaunchService(t, repo)
	createTask(t, s, "first")
	createTask(t, s, "second")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "first", 80, 24); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnAgent(ctx, "second", 80, 24); err != nil {
		t.Fatal(err)
	}

	time.Sleep(60 * time.Millisecond)
	mt.mu.Lock()
	typed := append([]string(nil), mt.sentMultiline["legato-second"]...)
	mt.mu.Unlock()
	if len(typed) != 0 {
		t.Fatalf("the notice was typed into a session still starting: %v", typed)
	}

	commands := bindBridge(t, s, "second")

	command := awaitBridgeCommand(t, commands)
	text, _ := command["text"].(string)
	if command["type"] != "submit" || !strings.Contains(text, "[legato coordination]") || !strings.Contains(text, "first") {
		t.Fatalf("command = %v, want the coordination notice submitted as a prompt", command)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if typed := mt.sentMultiline["legato-second"]; len(typed) != 0 {
		t.Fatalf("the notice was also typed into the terminal: %v", typed)
	}
}
