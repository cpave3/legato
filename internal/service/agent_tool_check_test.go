package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestASpawnWithAMissingAgentToolIsRefusedWithTheFix(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "t1")
	mt := newMockTmux()
	var asked string
	missing := DoctorCheck{ID: "codex", Title: "Codex", Result: DoctorMissing, Detail: "codex not found on Legato's PATH", Fix: "Install Codex: npm install -g @openai/codex"}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapters: map[string]AIToolAdapter{"codex": &fakeAdapter{name: "codex"}},
		ToolCheck: func(_ context.Context, tool string) error {
			asked = tool
			return &AgentToolUnavailableError{Check: missing}
		},
	})
	err = svc.SpawnAgent(context.Background(), "t1", 80, 24, AgentSpawnOptions{AgentKind: "codex"})
	var unavailable *AgentToolUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("spawn = %v, want a refusal", err)
	}
	if asked != "codex" {
		t.Errorf("checked %q, want codex", asked)
	}
	if !strings.Contains(err.Error(), "npm install -g @openai/codex") {
		t.Errorf("refusal %q has no fix", err)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.sessions) != 0 {
		t.Fatalf("a tmux session was made: %v", mt.sessions)
	}
}
