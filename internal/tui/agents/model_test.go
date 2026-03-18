package agents

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func testAgents() []service.AgentSession {
	return []service.AgentSession{
		{ID: 1, TicketID: "REX-1238", TmuxSession: "legato-REX-1238", Command: "shell", Status: "running", StartedAt: time.Now().Add(-5 * time.Minute)},
		{ID: 2, TicketID: "REX-1239", TmuxSession: "legato-REX-1239", Command: "shell", Status: "running", StartedAt: time.Now().Add(-12 * time.Minute)},
	}
}

func newTestModel() Model {
	m := New()
	m.SetAgents(testAgents())
	m.SetSize(120, 40)
	return m
}

func TestNavigationJK(t *testing.T) {
	m := newTestModel()

	if m.selected != 0 {
		t.Fatalf("initial selected = %d, want 0", m.selected)
	}

	// j moves down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Errorf("after j: selected = %d, want 1", m.selected)
	}

	// j at bottom stays
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Errorf("after j at bottom: selected = %d, want 1", m.selected)
	}

	// k moves up
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.selected != 0 {
		t.Errorf("after k: selected = %d, want 0", m.selected)
	}

	// k at top stays
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.selected != 0 {
		t.Errorf("after k at top: selected = %d, want 0", m.selected)
	}
}

func TestSpawnKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected command from 's' key")
	}
	msg := cmd()
	if _, ok := msg.(SpawnAgentMsg); !ok {
		t.Errorf("expected SpawnAgentMsg, got %T", msg)
	}
}

func TestKillKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd == nil {
		t.Fatal("expected command from 'X' key")
	}
	msg := cmd()
	kill, ok := msg.(KillAgentMsg)
	if !ok {
		t.Fatalf("expected KillAgentMsg, got %T", msg)
	}
	if kill.TicketID != "REX-1238" {
		t.Errorf("TicketID = %q, want %q", kill.TicketID, "REX-1238")
	}
}

func TestKillNoAgentNoCmd(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd != nil {
		t.Error("expected nil command when no agents")
	}
}

func TestEnterAttachKeybinding(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter key")
	}
	msg := cmd()
	attach, ok := msg.(AttachSessionMsg)
	if !ok {
		t.Fatalf("expected AttachSessionMsg, got %T", msg)
	}
	if attach.TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", attach.TmuxSession, "legato-REX-1238")
	}
}

func TestEscReturnsToBoard(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command from esc key")
	}
	msg := cmd()
	if _, ok := msg.(ReturnToBoardMsg); !ok {
		t.Errorf("expected ReturnToBoardMsg, got %T", msg)
	}
}

func TestCaptureOutputMsgUpdatesContent(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(CaptureOutputMsg{Output: "hello world\n$ "})
	if m.termContent != "hello world\n$ " {
		t.Errorf("termContent = %q, want %q", m.termContent, "hello world\n$ ")
	}
}

func TestAgentsRefreshedMsgUpdatesAgents(t *testing.T) {
	m := newTestModel()
	m.selected = 1 // select second agent

	// Refresh with only one agent
	m, _ = m.Update(AgentsRefreshedMsg{
		Agents: []service.AgentSession{
			{ID: 1, TicketID: "REX-1238", TmuxSession: "legato-REX-1238", Command: "shell", Status: "running", StartedAt: time.Now()},
		},
	})
	if len(m.agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(m.agents))
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 (clamped)", m.selected)
	}
}

func TestViewContainsElements(t *testing.T) {
	m := newTestModel()
	view := m.View()

	if view == "" {
		t.Fatal("expected non-empty view")
	}

	// Header should contain selected agent's ticket ID
	if !containsStr(view, "REX-1238") {
		t.Error("view should contain selected agent REX-1238")
	}

	// Should contain keybinding hints
	if !containsStr(view, "spawn") {
		t.Error("view should contain keybinding help")
	}

	// Should contain agent counter when multiple agents
	if !containsStr(view, "[1/2]") {
		t.Error("view should contain agent counter [1/2]")
	}
}

func TestViewEmptyState(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	view := m.View()

	if !containsStr(view, "No agents") {
		t.Error("empty state should show 'No agents' message")
	}
}

func TestSelectedAgentReturnsCorrectAgent(t *testing.T) {
	m := newTestModel()

	a := m.SelectedAgent()
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	if a.TicketID != "REX-1238" {
		t.Errorf("TicketID = %q, want %q", a.TicketID, "REX-1238")
	}

	m.selected = 1
	a = m.SelectedAgent()
	if a.TicketID != "REX-1239" {
		t.Errorf("TicketID = %q, want %q", a.TicketID, "REX-1239")
	}
}

func TestSelectedAgentNilWhenEmpty(t *testing.T) {
	m := New()
	if a := m.SelectedAgent(); a != nil {
		t.Error("expected nil agent when list is empty")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
