package tui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// openReviewLoop opens the review loop that holds a card's pull request in
// the web UI, which shows it (the TUI has no run page); with the web UI off,
// the status bar says where it is.
func (a App) openReviewLoop(loop *service.FoundReviewLoop) (tea.Model, tea.Cmd) {
	if loop == nil {
		return a, nil
	}
	if a.webURL == "" {
		a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: fmt.Sprintf("Review loop %s: %s in the web UI", loop.LoopID, loop.Link)})
		return a, nil
	}
	if err := a.openURL(a.webURL + loop.Link); err != nil {
		a.statusBar, _ = a.statusBar.Update(statusbar.ErrorMsg{Text: "Opening the review loop: " + err.Error()})
		return a, nil
	}
	a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: "Opened review loop " + loop.LoopID})
	return a, nil
}

// withWebLink adds to a refused start the web UI's address of what it
// links to, such as the loop that holds the pull request.
func (a App) withWebLink(err error) error {
	var refused *service.WorkflowStepError
	if a.webURL == "" || !errors.As(err, &refused) || refused.Link == "" {
		return err
	}
	return fmt.Errorf("%w\nOpen %s%s", err, a.webURL, refused.Link)
}
