package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSpawnClaudeAccountKeyboardSelection(t *testing.T) {
	m := NewAgentSpawn([]string{"claude-code", "codex"}, "claude-code", "/work", "TASK-1", "Task").WithClaudeAccounts([]SpawnAccount{{ID: "work", Label: "Work subscription"}, {ID: "personal"}})
	// The default-agent policy stays unspecified, including its account.
	_, msg := spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg); got.AgentKind != "" || got.Account != "" {
		t.Fatalf("policy submit = %+v", got)
	}
	// An explicit Claude choice has its own implicit default, not the policy's account.
	m, _ = spawnKey(t, m, keyRight, keyTab)
	if view := m.View(); !strings.Contains(view, "› account") || !strings.Contains(view, "default") {
		t.Fatalf("account row missing: %s", view)
	}
	_, msg = spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg); got.AgentKind != "claude-code" || got.Account != "" {
		t.Fatalf("explicit default = %+v", got)
	}
	m, _ = spawnKey(t, m, runes("l"))
	if !strings.Contains(m.View(), "Work subscription") {
		t.Fatal("selected label missing")
	}
	_, msg = spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg); got.Account != "work" {
		t.Fatalf("account = %q", got.Account)
	}
	m, _ = spawnKey(t, m, keyRight)
	if !strings.Contains(m.View(), "personal") {
		t.Fatal("missing name fallback")
	}
	m, _ = spawnKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	_, msg = spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg); got.Account != "work" {
		t.Fatalf("left selection = %+v", got)
	}
	// Changing tools drops the account and skips the row in tab order.
	m, _ = spawnKey(t, m, keyShiftTab, keyRight, keyTab)
	if strings.Contains(m.View(), "› account") || strings.Contains(m.View(), "Work subscription") {
		t.Fatal("account offered for another tool")
	}
	_, msg = spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg); got.Account != "" {
		t.Fatalf("non-Claude account = %+v", got)
	}
}

func TestSpawnNamedAccountMakesDefaultAgentExplicitClaude(t *testing.T) {
	m := NewAgentSpawn(nil, "claude-code", "/work", "TASK-1", "Task").WithClaudeAccounts([]SpawnAccount{{ID: "work"}})
	_, msg := spawnKey(t, m, keyTab, keyRight, keyEnter)
	got := msg.(AgentSpawnSubmitMsg)
	if got.AgentKind != "claude-code" || got.Account != "work" {
		t.Fatalf("named account submit = %+v", got)
	}
}
