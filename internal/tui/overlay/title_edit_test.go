package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTitleEditPrefilledWithCurrentTitle(t *testing.T) {
	m := NewTitleEdit("abc123", "Old title")
	m.width = 80
	m.height = 40

	if m.title != "Old title" {
		t.Errorf("title = %q, want 'Old title'", m.title)
	}
}

func TestTitleEditTyping(t *testing.T) {
	m := NewTitleEdit("abc123", "")
	m.width = 80
	m.height = 40

	for _, r := range "New" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(TitleEditOverlay)
	}
	if m.title != "New" {
		t.Errorf("title = %q, want 'New'", m.title)
	}
}

func TestTitleEditSpaceWorks(t *testing.T) {
	m := NewTitleEdit("abc123", "hello")
	m.width = 80
	m.height = 40

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(TitleEditOverlay)
	if m.title != "hello " {
		t.Errorf("title = %q, want 'hello '", m.title)
	}
}

func TestTitleEditBackspace(t *testing.T) {
	m := NewTitleEdit("abc123", "hello")
	m.width = 80
	m.height = 40

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(TitleEditOverlay)
	if m.title != "hell" {
		t.Errorf("title = %q, want 'hell'", m.title)
	}
}

func TestTitleEditSubmit(t *testing.T) {
	m := NewTitleEdit("abc123", "Updated title")
	m.width = 80
	m.height = 40

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter")
	}
	msg := cmd()
	result, ok := msg.(TitleEditSubmitMsg)
	if !ok {
		t.Fatalf("expected TitleEditSubmitMsg, got %T", msg)
	}
	if result.TaskID != "abc123" {
		t.Errorf("taskID = %q, want abc123", result.TaskID)
	}
	if result.Title != "Updated title" {
		t.Errorf("title = %q, want 'Updated title'", result.Title)
	}
}

func TestTitleEditEmptyTitleNoSubmit(t *testing.T) {
	m := NewTitleEdit("abc123", "")
	m.width = 80
	m.height = 40

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("empty title should not submit")
	}
}

func TestTitleEditCancel(t *testing.T) {
	m := NewTitleEdit("abc123", "something")
	m.width = 80
	m.height = 40

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on esc")
	}
	msg := cmd()
	if _, ok := msg.(TitleEditCancelledMsg); !ok {
		t.Fatalf("expected TitleEditCancelledMsg, got %T", msg)
	}
}
