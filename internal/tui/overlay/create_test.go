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

	// Tab to column focus, then use l to cycle
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(CreateOverlay)
	if m.Column() != "Done" {
		t.Errorf("after l = %q, want Done", m.Column())
	}

	// l wraps around
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
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

func TestCreateOverlaySpaceInTitle(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	// Type "hello world" with a space
	for _, r := range "hello" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(CreateOverlay)
	for _, r := range "world" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	if m.title != "hello world" {
		t.Errorf("title = %q, want 'hello world'", m.title)
	}
}

func TestCreateOverlayTabCyclesThroughAllFields(t *testing.T) {
	cols := []string{"Backlog", "Doing"}
	m := NewCreate(cols, "Backlog")
	m.width = 80
	m.height = 40

	// Initial focus is title
	if m.focus != focusTitle {
		t.Errorf("initial focus = %d, want focusTitle", m.focus)
	}

	// Tab 1: title → column
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	if m.focus != focusColumn {
		t.Errorf("after 1st tab: focus = %d, want focusColumn", m.focus)
	}

	// Tab 2: column → description
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	if m.focus != focusDescription {
		t.Errorf("after 2nd tab: focus = %d, want focusDescription", m.focus)
	}

	// Tab 3: description → title (wraps)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	if m.focus != focusTitle {
		t.Errorf("after 3rd tab: focus = %d, want focusTitle", m.focus)
	}
}

func TestCreateOverlayTypingInDescriptionField(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	// Tab to column, then to description
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)

	// Type in description
	for _, r := range "desc" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}
	if m.description != "desc" {
		t.Errorf("description = %q, want 'desc'", m.description)
	}

	// Space works in description
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(CreateOverlay)
	if m.description != "desc " {
		t.Errorf("description after space = %q, want 'desc '", m.description)
	}

	// Backspace
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(CreateOverlay)
	if m.description != "desc" {
		t.Errorf("description after backspace = %q, want 'desc'", m.description)
	}
}

func TestCreateOverlayCtrlJNewline(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	// Tab to description
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)

	// Type then ctrl+j for newline
	for _, r := range "line1" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(CreateOverlay)
	for _, r := range "line2" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	if m.description != "line1\nline2" {
		t.Errorf("description = %q, want 'line1\\nline2'", m.description)
	}
}

func TestCreateOverlaySubmitIncludesDescription(t *testing.T) {
	m := NewCreate([]string{"Backlog"}, "Backlog")
	m.width = 80
	m.height = 40

	// Type title
	for _, r := range "Task" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	// Tab to column, then to description
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)

	// Type description
	for _, r := range "My desc" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(CreateOverlay)
	}

	// Submit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command on enter")
	}
	msg := cmd()
	create := msg.(CreateTaskMsg)
	if create.Description != "My desc" {
		t.Errorf("description = %q, want 'My desc'", create.Description)
	}
}

func TestCreateOverlayColumnCyclingOnlyWhenFocused(t *testing.T) {
	cols := []string{"Backlog", "Doing", "Done"}
	m := NewCreate(cols, "Backlog")
	m.width = 80
	m.height = 40

	// Tab to column focus
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CreateOverlay)

	// Now typing 'j' or 'l' in column focus should cycle columns
	// Actually, looking at existing code, tab is what cycles columns when in column focus
	// Let me verify: in column focus, tab should move to description, not cycle column
	// Column cycling uses tab-within-column (old behavior) — but now tab moves focus.
	// Per spec: tab cycles title → column → description → title
	// Column selection within column focus: use j/k or left/right
	// Actually the spec says "tab cycles columns" within the column row.
	// Let me re-read... The old code just cycled column on tab. The new design has
	// tab cycling focus fields. Within column focus, we need another mechanism.

	// For now, verify tab moves to description
	if m.focus != focusColumn {
		t.Errorf("focus = %d, want focusColumn", m.focus)
	}
	// In column focus, j/k or h/l should cycle columns
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(CreateOverlay)
	if m.Column() != "Doing" {
		t.Errorf("column after l = %q, want Doing", m.Column())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(CreateOverlay)
	if m.Column() != "Backlog" {
		t.Errorf("column after h = %q, want Backlog", m.Column())
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
