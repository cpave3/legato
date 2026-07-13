package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWorktreeOverlaySubmitsEnteredValues(t *testing.T) {
	m := NewWorktree("task-1", "/repo", "task-1")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg, ok := cmd().(WorktreeSubmitMsg)
	if !ok {
		t.Fatalf("message = %T, want WorktreeSubmitMsg", cmd())
	}
	if msg.TaskID != "task-1" || msg.PrimaryDir != "/repo" || msg.Branch != "task-1" || msg.BaseBranch != "main" {
		t.Fatalf("message = %#v", msg)
	}
}

func TestWorktreeOverlayRequiresAllValues(t *testing.T) {
	m := NewWorktree("task-1", "", "task-1")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty primary directory submitted")
	}
}
