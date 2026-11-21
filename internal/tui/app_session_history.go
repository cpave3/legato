package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// SessionHistory is the session history the TUI lists and restores from:
// the service the API uses.
type SessionHistory interface {
	List(ctx context.Context, q service.SessionQuery) (service.SessionPage, error)
	Restore(ctx context.Context, id int64, target string) (string, error)
}

// SetSessionHistory enables the history list of the agents view (H).
func (a *App) SetSessionHistory(h SessionHistory) {
	a.sessionHistory = h
}

// sessionHistoryLoadedMsg is a page of the history for the overlay, for
// the query it was asked for.
type sessionHistoryLoadedMsg struct {
	query   string
	entries []service.SessionEntry
	err     error
}

const tuiHistoryPage = 100

func (a App) loadSessionHistory(query string) tea.Cmd {
	history := a.sessionHistory
	return func() tea.Msg {
		page, err := history.List(context.Background(), service.SessionQuery{Q: query, Limit: tuiHistoryPage})
		return sessionHistoryLoadedMsg{query: query, entries: page.Sessions, err: err}
	}
}

func (a App) openSessionHistoryOverlay() (tea.Model, tea.Cmd) {
	if a.sessionHistory == nil {
		return a, nil
	}
	model := overlay.NewSessionHistory()
	sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlaySessionHistory
	a.sessionHistoryQuery = ""
	return a, a.loadSessionHistory("")
}

// updateSessionHistory handles the history overlay's messages; handled is
// false for any other message.
func (a App) updateSessionHistory(msg tea.Msg) (model tea.Model, cmd tea.Cmd, handled bool) {
	switch msg := msg.(type) {
	case sessionHistoryLoadedMsg:
		current, ok := a.activeOverlay.(overlay.SessionHistoryOverlay)
		// A page for a query since replaced is dropped.
		if !ok || a.overlayType != overlaySessionHistory || msg.query != a.sessionHistoryQuery {
			return a, nil, true
		}
		a.activeOverlay = current.SetEntries(msg.entries, msg.err)
		return a, nil, true
	case overlay.SessionHistorySearchMsg:
		a.sessionHistoryQuery = msg.Query
		return a, a.loadSessionHistory(msg.Query), true
	case overlay.SessionHistoryRefusedMsg:
		return a, func() tea.Msg { return statusbar.ErrorMsg{Text: "Cannot restore: " + msg.Reason} }, true
	case overlay.SessionHistoryCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil, true
	case overlay.SessionRestoreMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		history := a.sessionHistory
		return a, func() tea.Msg {
			taskID, err := history.Restore(context.Background(), msg.ID, msg.Target)
			var refused *service.RestoreRefusal
			switch {
			case errors.As(err, &refused):
				return statusbar.ErrorMsg{Text: "Cannot restore: " + refused.Reason}
			case err != nil:
				return statusbar.ErrorMsg{Text: "Restore failed: " + err.Error()}
			}
			return statusbar.InfoMsg{Text: "Restored the conversation in " + taskID}
		}, true
	}
	return a, nil, false
}
