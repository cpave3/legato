package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAgentSpawnOverlayEphemeralDefaults(t *testing.T) {
	m := NewAgentSpawn([]string{"chimera"}, "openai", "/tmp", "", "")

	// Title should start empty
	if m.title != "" {
		t.Errorf("title = %q, want empty", m.title)
	}
	// Focus should be on title for ephemeral
	if m.focus != spawnFocusTitle {
		t.Errorf("focus = %d, want title", m.focus)
	}
	// Heading should say "Ephemeral"
	view := m.View()
	if !strings.Contains(view, "Spawn Ephemeral Agent") {
		t.Error("expected 'Spawn Ephemeral Agent' heading")
	}
	// Default values: adapter options
	if len(m.agentOptions) != 3 {
		t.Fatalf("expected 3 agent options, got %d", len(m.agentOptions))
	}
	if m.agentOptions[0] != "default (openai)" {
		t.Errorf("first option = %q", m.agentOptions[0])
	}
	if m.agentOptions[1] != "shell" {
		t.Errorf("second option = %q", m.agentOptions[1])
	}
	if m.agentOptions[2] != "chimera" {
		t.Errorf("third option = %q", m.agentOptions[2])
	}
}

func TestAgentSpawnOverlayTaskBoundDefaults(t *testing.T) {
	m := NewAgentSpawn([]string{}, "", "/workspace", "REX-1", "Fix login")

	if m.taskID != "REX-1" {
		t.Errorf("taskID = %q, want REX-1", m.taskID)
	}
	if m.title != "Fix login" {
		t.Errorf("title = %q, want 'Fix login'", m.title)
	}
	// Focus should be on agent selector for task-bound
	if m.focus != spawnFocusAgent {
		t.Errorf("focus = %d, want agent", m.focus)
	}
	view := m.View()
	if strings.Contains(view, "Ephemeral") {
		t.Error("task-bound heading should not contain 'Ephemeral'")
	}
	if !strings.Contains(view, "Spawn Agent") {
		t.Error("expected 'Spawn Agent' heading")
	}
}

func TestAgentSpawnOverlayCancel(t *testing.T) {
	m := NewAgentSpawn(nil, "", "/tmp", "", "")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on esc")
	}
	msg := cmd()
	if _, ok := msg.(AgentSpawnCancelledMsg); !ok {
		t.Fatalf("expected AgentSpawnCancelledMsg, got %T", msg)
	}
}

func TestAgentSpawnOverlayEphemeralSubmit(t *testing.T) {
	m := NewAgentSpawn(nil, "", "/tmp", "", "") // empty title
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter")
	}
	msg := cmd()
	submit, ok := msg.(AgentSpawnSubmitMsg)
	if !ok {
		t.Fatalf("expected AgentSpawnSubmitMsg, got %T", msg)
	}
	if submit.TaskID != "" {
		t.Errorf("TaskID = %q, want empty", submit.TaskID)
	}
	if submit.Title != "Ephemeral session" {
		t.Errorf("Title = %q, want 'Ephemeral session'", submit.Title)
	}
	if submit.WorkingDir != "/tmp" {
		t.Errorf("WorkingDir = %q", submit.WorkingDir)
	}
	_ = tm.(AgentSpawnOverlay)
}

func TestAgentSpawnOverlayEphemeralSubmitWithTitle(t *testing.T) {
	m := NewAgentSpawn(nil, "", "/tmp", "", "debug session")
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter")
	}
	msg := cmd()
	submit := msg.(AgentSpawnSubmitMsg)
	if submit.Title != "debug session" {
		t.Errorf("Title = %q, want 'debug session'", submit.Title)
	}
	_ = tm.(AgentSpawnOverlay)
}

func TestAgentSpawnOverlayTaskBoundSubmit(t *testing.T) {
	m := NewAgentSpawn([]string{"chimera"}, "openai", "/workspace", "REX-1", "Fix login")
	// Switch focus to CWD, then back to agent
	tmp, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // another key doesn't matter
	m = tmp.(AgentSpawnOverlay)
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter")
	}
	msg := cmd()
	submit := msg.(AgentSpawnSubmitMsg)
	if submit.TaskID != "REX-1" {
		t.Errorf("TaskID = %q, want REX-1", submit.TaskID)
	}
	if submit.Title != "Fix login" {
		t.Errorf("Title = %q, want 'Fix login'", submit.Title)
	}
	if submit.AgentKind != "" {
		t.Errorf("AgentKind = %q, want empty (default)", submit.AgentKind)
	}
	_ = tm.(AgentSpawnOverlay)
}

func TestAgentSpawnOverlayAgentSelection(t *testing.T) {
	m := NewAgentSpawn([]string{"chimera"}, "openai", "/tmp", "", "")
	// Move focus to agent selector
	tmp, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tmp.(AgentSpawnOverlay)
	if m.focus != spawnFocusAgent {
		t.Fatalf("focus should be on agent after tab")
	}
	// Move right twice to reach chimera (default -> shell -> chimera)
	tmp, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = tmp.(AgentSpawnOverlay)
	tmp, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = tmp.(AgentSpawnOverlay)
	if m.agentIndex != 2 {
		t.Errorf("agentIndex = %d, want 2", m.agentIndex)
	}
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(AgentSpawnOverlay)
	msg := cmd()
	submit := msg.(AgentSpawnSubmitMsg)
	if submit.AgentKind != "chimera" {
		t.Errorf("AgentKind = %q, want chimera", submit.AgentKind)
	}
}
