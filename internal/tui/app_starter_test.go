package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

func TestTheBoardOpensOnTheStarterTaskWithTheHint(t *testing.T) {
	app := newTestAppWithBoardService(&fakeBoardService{imported: []service.Card{{ID: "START1", Title: "Try Legato: explain this repository", Status: "Backlog"}}})
	app.SetStarterTask("START1")
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	var hint string
	for _, msg := range messagesOf(app.board.Init()) {
		var cmd tea.Cmd
		app, cmd = updateApp(app, msg)
		for _, m := range messagesOf(cmd) {
			if info, ok := m.(statusbar.InfoMsg); ok {
				hint = info.Text
			}
		}
	}
	if card := app.board.SelectedCard(); card == nil || card.Key != "START1" {
		t.Fatalf("selected %+v, want the starter task", card)
	}
	if !strings.Contains(hint, "press a") {
		t.Fatalf("hint = %q", hint)
	}
}
