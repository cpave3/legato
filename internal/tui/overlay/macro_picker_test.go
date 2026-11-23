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

func TestMacroPickerShowsFavouritesAndTheRestBehindShowAll(t *testing.T) {
	var m tea.Model = NewMacroPicker([]macros.Macro{
		{Name: "Plan", Keys: "plan it", Favorite: true},
		{Name: "Ticket to PR", Keys: "ship it"},
		{Name: "Tests", Keys: "run the tests", Favorite: true},
	})
	v := m.View()
	if !strings.Contains(v, "Plan") || !strings.Contains(v, "Tests") || !strings.Contains(v, "Show all (1 more)") {
		t.Fatalf("want the favourites and Show all (1 more), got:\n%s", v)
	}
	if strings.Contains(v, "Ticket to PR") {
		t.Fatalf("a macro that is not a favourite is listed before Show all:\n%s", v)
	}

	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("j"))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("Show all sent something: %T", cmd())
	}
	v = m.View()
	if !strings.Contains(v, "Ticket to PR") || strings.Contains(v, "Show all") {
		t.Fatalf("after Show all, want every macro and no Show all row, got:\n%s", v)
	}
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the first of the rest sent nothing")
	}
	if sel, ok := cmd().(MacroSelectedMsg); !ok || sel.Macro.Name != "Ticket to PR" {
		t.Fatalf("selected %+v, want Ticket to PR", cmd())
	}
}

func TestMacroPickerWithNoStarsListsEveryMacro(t *testing.T) {
	m := NewMacroPicker([]macros.Macro{{Name: "a", Keys: "1"}, {Name: "b", Keys: "2"}})
	v := m.View()
	if !strings.Contains(v, "a") || !strings.Contains(v, "b") || strings.Contains(v, "Show all") {
		t.Fatalf("want every macro and no Show all, got:\n%s", v)
	}
}
