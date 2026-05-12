package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/macros"
)

func TestMacroPickerEmptyShowsPlaceholder(t *testing.T) {
	m := NewMacroPicker(nil)
	v := m.View()
	if !strings.Contains(v, "(no macros configured)") {
		t.Fatalf("expected placeholder in view, got:\n%s", v)
	}
}

func TestMacroPickerNavigateAndSelect(t *testing.T) {
	m := NewMacroPicker([]macros.Macro{
		{Name: "run tests", Keys: "task test\n"},
		{Name: "git diff", Keys: "! git diff\n"},
	})

	// Down → cursor 1
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = m2.(MacroPickerOverlay)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	// Up → cursor 0
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = m2.(MacroPickerOverlay)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}

	// Enter → MacroSelectedMsg
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg := cmd()
	sel, ok := msg.(MacroSelectedMsg)
	if !ok {
		t.Fatalf("expected MacroSelectedMsg, got %T", msg)
	}
	if sel.Macro.Name != "run tests" || sel.Macro.Keys != "task test\n" {
		t.Errorf("selected macro = %+v", sel.Macro)
	}
}

func TestMacroPickerCancel(t *testing.T) {
	m := NewMacroPicker([]macros.Macro{
		{Name: "x", Keys: "y\n"},
	})
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	_ = m2
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(MacroCancelledMsg); !ok {
		t.Fatalf("expected MacroCancelledMsg, got %T", msg)
	}
}


