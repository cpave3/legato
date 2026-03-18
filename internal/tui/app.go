package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/service"
)

type viewType int

const (
	viewBoard viewType = iota
)

// EventBusMsg wraps an event bus event as a Bubbletea message.
type EventBusMsg struct {
	Event events.Event
}

// App is the root Bubbletea model.
type App struct {
	board     board.Model
	statusBar statusbar.Model
	active    viewType
	width     int
	height    int
	eventBus  *events.Bus
	eventSub  <-chan events.Event
}

// NewApp creates a new root application model.
func NewApp(svc service.BoardService, bus *events.Bus) App {
	app := App{
		board:     board.New(svc),
		statusBar: statusbar.New(),
		active:    viewBoard,
		eventBus:  bus,
	}
	if bus != nil {
		app.eventSub = bus.Subscribe(events.EventSyncStarted)
	}
	return app
}

// Init returns initial commands.
func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.board.Init()}
	if a.eventSub != nil {
		cmds = append(cmds, a.listenEventBus())
	}
	return tea.Batch(cmds...)
}

// listenEventBus returns a command that bridges EventBus events into Bubbletea messages.
func (a App) listenEventBus() tea.Cmd {
	ch := a.eventSub
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return EventBusMsg{Event: event}
	}
}

// Update handles messages.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		default:
			// Delegate to active view
			switch a.active {
			case viewBoard:
				var cmd tea.Cmd
				a.board, cmd = a.board.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Propagate to board (reserve 1 line for status bar)
		boardMsg := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 1}
		var cmd tea.Cmd
		a.board, cmd = a.board.Update(boardMsg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Propagate to status bar
		a.statusBar, _ = a.statusBar.Update(msg)

	case EventBusMsg:
		// Convert event bus events to status bar messages
		switch msg.Event.Type {
		case events.EventSyncStarted:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncStartedMsg{})
		case events.EventSyncCompleted:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncCompletedMsg{})
		case events.EventSyncFailed:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncFailedMsg{})
		}
		// Continue listening
		if a.eventSub != nil {
			cmds = append(cmds, a.listenEventBus())
		}

	case board.DataLoadedMsg:
		// Forward data loaded to board
		var cmd tea.Cmd
		a.board, cmd = a.board.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Forward other messages to board
		var cmd tea.Cmd
		a.board, cmd = a.board.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

// View renders the application.
func (a App) View() string {
	var content string
	switch a.active {
	case viewBoard:
		content = a.board.View()
	}
	statusBar := a.statusBar.View()
	statusBarHeight := lipgloss.Height(statusBar)

	// Pad board content to fill available height
	boardHeight := a.height - statusBarHeight
	if boardHeight > 0 {
		content = lipgloss.NewStyle().Height(boardHeight).Render(content)
	}

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}
