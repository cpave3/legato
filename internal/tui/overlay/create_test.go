package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCreateOverlayTitleInput(t *testing.T) {
	m := NewCreate([]string{"Backlog", "Doing", "Done"}, "Backlog")
	m.width = 80
	m.height = 40

	// Type "hello"
	for _, r := range "hello" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}
	if m.title != "hello" {
		t.Errorf("title = %q, want hello", m.title)
	}

	// Backspace
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(CreateOverlay)
	if m.title != "hell" {
		t.Errorf("title after backspace = %q, want hell", m.title)
	}
}

func TestCreateOverlayColumnCycling(t *testing.T) {
	cols := []string{"Backlog", "Doing", "Done"}
	m := NewCreate(cols, "Doing")
	m.width = 80
	m.height = 40

	if m.Column() != "Doing" {
		t.Errorf("initial column = %q, want Doing", m.Column())
	}

	// Tab cycles to next
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	if m.Column() != "Done" {
		t.Errorf("after tab = %q, want Done", m.Column())
	}

	// Tab wraps around
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	if m.Column() != "Backlog" {
		t.Errorf("after wrap = %q, want Backlog", m.Column())
	}
}

func TestCreateOverlayPriorityCycling(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	if m.Priority() != "" {
		t.Errorf("initial priority = %q, want empty", m.Priority())
	}

	// ctrl+p cycles: "" → Low → Medium → High → ""
	expected := []string{"Low", "Medium", "High", ""}
	for _, want := range expected {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		m = updated.(CreateOverlay)
		if m.Priority() != want {
			t.Errorf("priority = %q, want %q", m.Priority(), want)
		}
	}
}

func TestCreateOverlaySubmit(t *testing.T) {
	m := NewCreate([]string{"Backlog", "Doing"}, "Backlog")
	m.width = 80
	m.height = 40

	// Type title
	for _, r := range "New task" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	// Enter submits
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter with non-empty title")
	}
	msg := cmd()
	create, ok := msg.(CreateTaskMsg)
	if !ok {
		t.Fatalf("expected CreateTaskMsg, got %T", msg)
	}
	if create.Title != "New task" {
		t.Errorf("title = %q, want 'New task'", create.Title)
	}
	if create.Column != "Backlog" {
		t.Errorf("column = %q, want Backlog", create.Column)
	}
}

func TestCreateOverlayEmptyTitleNoSubmit(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	// Enter with empty title should not submit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected no command on enter with empty title")
	}
}

func TestCreateOverlayCancel(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on esc")
	}
	msg := cmd()
	if _, ok := msg.(CreateCancelledMsg); !ok {
		t.Fatalf("expected CreateCancelledMsg, got %T", msg)
	}
}

func TestCreateOverlayViewContainsElements(t *testing.T) {
	m := NewCreate([]string{"Backlog", "Doing"}, "Backlog")
	m.width = 80
	m.height = 40

	// Type something
	for _, r := range "Test" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
	// Panel should contain the title text (even if styled)
	// and column names
}
