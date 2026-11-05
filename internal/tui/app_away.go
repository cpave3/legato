package tui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// refuseAway says, in the status bar, that a task cannot be changed here
// because it is away on a worker, and reports whether it is. The TUI has
// no handoff actions: the person returns or recalls the task from the web
// UI. It asks the board, which knows the away tasks it shows, so a change
// is refused before its overlay opens.
func (a App) refuseAway(taskID string) (App, tea.Cmd, bool) {
	away := a.board.AwayTask(taskID)
	if away == nil {
		return a, nil, false
	}
	a, cmd := a.showRefusal(board.AwayRefusal(taskID, board.AwayWorker(away)))
	return a, cmd, true
}

// showRefusal puts a refusal where the person is looking: the detail view
// has a bar of its own, the other views the status bar.
func (a App) showRefusal(text string) (App, tea.Cmd) {
	if a.active == viewDetail {
		model, _ := a.detail.Update(detail.FeedbackMsg{Text: text})
		a.detail = model.(detail.Model)
		return a, nil
	}
	var cmd tea.Cmd
	a.statusBar, cmd = a.statusBar.Update(statusbar.ErrorMsg{Text: text})
	return a, cmd
}

// awayErrorText words the service's refusal of a change to an away task as
// the TUI's own refusal, and on a worker its refusal to delete or archive
// a card a main handed to it; false for any other error.
func awayErrorText(err error) (string, bool) {
	var away *service.TaskAwayError
	if errors.As(err, &away) {
		return board.AwayRefusal(away.TaskID, away.Worker), true
	}
	var handedOff *service.TaskHandedOffError
	if errors.As(err, &handedOff) {
		return handedOff.Error(), true
	}
	return "", false
}

// keptDoneText says why archiving the done cards kept some: each is away on
// a worker, or a card a main handed to this worker, and goes home first.
func keptDoneText(kept int) string {
	cards := "cards"
	if kept == 1 {
		cards = "card"
	}
	return fmt.Sprintf("%d done %s kept: away on a worker, or handed here by a main; return or recall it from its main first", kept, cards)
}

// showAwayError shows the service's refusal of a change to an away task,
// for a task the board did not yet know was away, and reports whether err
// was one.
func (a App) showAwayError(err error) (App, tea.Cmd, bool) {
	text, ok := awayErrorText(err)
	if !ok {
		return a, nil, false
	}
	a, cmd := a.showRefusal(text)
	return a, cmd, true
}
