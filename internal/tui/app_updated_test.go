package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A newer Legato asked this one to exit before it migrates the database
// (spec LEG-559 M7): the TUI says so and quits, whatever is in front.
func TestTheTUISaysLegatoUpdatedAndQuitsWhenANewerLegatoAsks(t *testing.T) {
	app := newTestApp()
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	app = model.(App)
	model, _ = app.openHelpOverlay()
	app = model.(App)

	model, cmd := app.Update(LegatoUpdatedMsg{})
	app = model.(App)
	if view := app.statusBar.View(); !strings.Contains(view, "Legato updated: restart") {
		t.Fatalf("status = %q, want it to say Legato updated: restart", view)
	}
	if cmd == nil {
		t.Fatal("no command: the TUI did not quit")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatalf("command gave %T, want tea.QuitMsg", cmd())
	}
}
