package board

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/tui/hotkey"
)

func TestContextActionEmitsSelectedCard(t *testing.T) {
	model := newTestModel()
	_, cmd := model.HandleAction(hotkey.ActionBoardContext)
	if cmd == nil {
		t.Fatal("context action emitted no command")
	}
	msg, ok := cmd().(OpenContextMsg)
	if !ok || msg.CardKey != "REX-1" {
		t.Fatalf("message = %#v", msg)
	}
}

func TestContextKeyIsNoOpWithoutSelectedCard(t *testing.T) {
	model := newTestModel()
	model.cursorCol = 2 // Review is empty.
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd != nil {
		t.Fatalf("empty-column context command = %#v", cmd())
	}
}
