package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// spinnerInterval is how long one frame of the spinner is shown.
const spinnerInterval = 100 * time.Millisecond

// spinnerTickMsg says it is time to turn the spinner one frame.
type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// spinning reports whether the view in front shows an agent at work. Only
// then is there something to animate, and only then does the app redraw
// between events.
func (a App) spinning() bool {
	if a.overlayType != overlayNone {
		return false
	}
	switch a.active {
	case viewBoard:
		return a.board.HasWorkingAgent()
	case viewAgents:
		return a.agentView.HasWorkingAgent()
	default:
		return false
	}
}

// handleSpinnerTick turns the spinner one frame and asks for the next tick
// while there is still something to animate.
func (a App) handleSpinnerTick() (tea.Model, tea.Cmd) {
	a.spinnerRunning = false
	if a.spinning() {
		a.spinFrame++
		a.board.SetSpinner(a.spinFrame)
		a.agentView.SetSpinner(a.spinFrame)
	}
	return a.keepSpinning(nil)
}

// keepSpinning adds a tick to cmd when the spinner should turn and no tick
// is on its way. One tick at a time keeps the spinner at one speed however
// many events arrive.
func (a App) keepSpinning(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if a.spinnerRunning || !a.spinning() {
		return a, cmd
	}
	a.spinnerRunning = true
	return a, tea.Batch(cmd, spinnerTickCmd())
}
