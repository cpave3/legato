package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cpave3/legato/internal/tui/statusbar"
)

// LegatoUpdatedNotice is what the TUI says when a newer Legato asks it to
// exit.
const LegatoUpdatedNotice = "Legato updated: restart"

// LegatoUpdatedMsg tells the TUI that a newer Legato binary is about to
// migrate the database and asked every other Legato process on the data
// folder to exit (spec LEG-559 M7): this one must not go on working on
// tables it does not know. The TUI says so and quits.
type LegatoUpdatedMsg struct{}

// legatoUpdated shows LegatoUpdatedNotice and quits, whatever overlay or
// view is in front.
func (a App) legatoUpdated() (tea.Model, tea.Cmd) {
	a.statusBar, _ = a.statusBar.Update(statusbar.WarningMsg{Text: LegatoUpdatedNotice})
	return a, tea.Quit
}
