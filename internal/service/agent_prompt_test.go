package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func promptService(t *testing.T) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter:           &fakeAdapter{name: "fake", launchFn: func(map[string]string, string, string, string) string { return "fake-tool" }},
		BriefKickoffDelay: time.Millisecond,
	})
	return svc, s, mt
}

func waitForLines(t *testing.T, mt *mockTmux, session string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lines := mt.sentLinesFor(session); len(lines) >= n {
			return lines
		}
		time.Sleep(10 * time.Millisecond)
	}
	return mt.sentLinesFor(session)
}

func TestAPromptIsTheAgentsFirstMessage(t *testing.T) {
	svc, s, mt := promptService(t)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Prompt: "Fix the failing sync test."}); err != nil {
		t.Fatal(err)
	}
	lines := waitForLines(t, mt, "legato-task-1", 2)
	if len(lines) != 2 || lines[0] != "fake-tool" || lines[1] != "Fix the failing sync test." {
		t.Fatalf("lines = %q", lines)
	}
}

func TestABlankPromptSendsNothing(t *testing.T) {
	svc, s, mt := promptService(t)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Prompt: "  \n "}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if lines := mt.sentLinesFor("legato-task-1"); len(lines) != 1 {
		t.Fatalf("lines = %q", lines)
	}
}
