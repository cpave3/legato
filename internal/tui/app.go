package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/clipboard"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

type viewType int

const (
	viewBoard viewType = iota
	viewDetail
	viewAgents
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayMove
	overlaySearch
	overlayHelp
)

// EventBusMsg wraps an event bus event as a Bubbletea message.
type EventBusMsg struct {
	Event events.Event
}

// ClipboardWarningMsg signals that clipboard is unavailable.
type ClipboardWarningMsg struct{}

// SearchResultsMsg carries search results back to the app.
type SearchResultsMsg struct {
	Results []service.Card
}

// App is the root Bubbletea model.
type App struct {
	svc       service.BoardService
	agentSvc  service.AgentService
	board     board.Model
	detail    detail.Model
	agentView agents.Model
	statusBar statusbar.Model
	clip          *clipboard.Clipboard
	activeOverlay tea.Model
	overlayType   overlayKind
	active        viewType
	width     int
	height    int
	eventBus  *events.Bus
	eventSub  <-chan events.Event
}

// NewApp creates a new root application model.
func NewApp(svc service.BoardService, agentSvc service.AgentService, bus *events.Bus) App {
	clip := clipboard.New()
	app := App{
		svc:       svc,
		agentSvc:  agentSvc,
		board:     board.New(svc),
		agentView: agents.New(),
		statusBar: statusbar.New(),
		clip:      clip,
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
	// Clipboard availability check
	if a.clip == nil || !a.clip.Available() {
		a.statusBar, _ = a.statusBar.Update(statusbar.WarningMsg{
			Text: "clipboard unavailable -- install xclip or wl-copy",
		})
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
		// ? opens help from any context (replaces active overlay if any)
		if msg.String() == "?" {
			return a.openHelpOverlay()
		}

		// Overlay intercepts all keys when active
		if a.overlayType != overlayNone {
			var cmd tea.Cmd
			a.activeOverlay, cmd = a.activeOverlay.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			if a.active == viewBoard {
				return a, tea.Quit
			}
			// In detail or agent view, q goes back to board
			if a.active == viewAgents {
				a.agentView.StopPolling()
			}
			a.active = viewBoard
			return a, nil
		case "/":
			if a.active == viewBoard {
				return a.openSearchOverlay()
			}
			return a.delegateKey(msg)
		default:
			return a.delegateKey(msg)
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

		// Propagate to detail if active
		if a.active == viewDetail {
			var detailModel tea.Model
			detailModel, cmd = a.detail.Update(tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 1})
			a.detail = detailModel.(detail.Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		// Propagate to agent view
		a.agentView.SetSize(msg.Width, msg.Height-1)

		// Propagate to status bar
		a.statusBar, _ = a.statusBar.Update(msg)

	case board.OpenDetailMsg:
		a.active = viewDetail
		// Load card synchronously — GetCard hits local SQLite, not remote API
		card, err := a.svc.GetCard(context.Background(), msg.CardKey)
		if err != nil {
			a.detail = detail.NewLoading(msg.CardKey, a.svc, a.clip)
		} else {
			a.detail = detail.New(card, a.svc, a.clip)
		}
		// Send window size
		detailModel, cmd := a.detail.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height - 1})
		a.detail = detailModel.(detail.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case detail.BackToBoard:
		a.active = viewBoard
		return a, nil

	case agents.ReturnToBoardMsg:
		a.active = viewBoard
		a.agentView.StopPolling()
		return a, nil

	case agents.SpawnAgentMsg:
		return a.handleSpawnAgent(msg)

	case agents.KillAgentMsg:
		return a.handleKillAgent(msg)

	case agents.AttachSessionMsg:
		return a.handleAttachSession(msg)

	case agents.CaptureOutputMsg:
		a.agentView, _ = a.agentView.Update(msg)
		return a, nil

	case agents.AgentsRefreshedMsg:
		a.agentView, _ = a.agentView.Update(msg)
		return a, nil

	case agentTickMsg:
		if a.active != viewAgents || a.agentSvc == nil {
			return a, nil
		}
		return a.handleAgentTick()

	case detail.OpenMoveOverlay:
		return a.openMoveOverlay(msg.TicketID)

	case board.OpenMoveMsg:
		return a.openMoveOverlay(msg.CardKey)

	case overlay.MoveSelectedMsg:
		return a.handleMoveSelected(msg)

	case overlay.MoveCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.SearchSelectedMsg:
		return a.handleSearchSelected(msg)

	case overlay.SearchCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.SearchQueryChangedMsg:
		return a.handleSearchQuery(msg)

	case overlay.HelpClosedMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case SearchResultsMsg:
		if a.overlayType == overlaySearch {
			so := a.activeOverlay.(overlay.SearchOverlay)
			so = so.SetResults(msg.Results)
			a.activeOverlay = so
		}
		return a, nil

	case detail.CardLoadedMsg:
		if a.active == viewDetail {
			var detailModel tea.Model
			var cmd tea.Cmd
			detailModel, cmd = a.detail.Update(msg)
			a.detail = detailModel.(detail.Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case EventBusMsg:
		// Convert event bus events to status bar messages
		switch msg.Event.Type {
		case events.EventSyncStarted:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncStartedMsg{})
		case events.EventSyncCompleted:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncCompletedMsg{})
		case events.EventSyncFailed:
			a.statusBar, _ = a.statusBar.Update(statusbar.SyncFailedMsg{})
		case events.EventSyncError, events.EventAuthFailed, events.EventRateLimited, events.EventTransitionFailed:
			if p, ok := msg.Event.Payload.(events.ErrorPayload); ok {
				a.statusBar, _ = a.statusBar.Update(statusbar.ErrorMsg{Text: p.Message})
			}
		}
		// Continue listening
		if a.eventSub != nil {
			cmds = append(cmds, a.listenEventBus())
		}

	case statusbar.ErrorMsg:
		a.statusBar, _ = a.statusBar.Update(msg)
		return a, nil

	case board.DataLoadedMsg:
		// Forward data loaded to board
		var cmd tea.Cmd
		a.board, cmd = a.board.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Forward other messages to active view
		switch a.active {
		case viewBoard:
			var cmd tea.Cmd
			a.board, cmd = a.board.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case viewDetail:
			var detailModel tea.Model
			var cmd tea.Cmd
			detailModel, cmd = a.detail.Update(msg)
			a.detail = detailModel.(detail.Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) openMoveOverlay(ticketID string) (tea.Model, tea.Cmd) {
	// Get columns from the board
	columns, err := a.svc.ListColumns(context.Background())
	if err != nil {
		return a, nil
	}
	colNames := make([]string, len(columns))
	for i, c := range columns {
		colNames[i] = c.Name
	}
	// Get current column for the ticket
	card, _ := a.svc.GetCard(context.Background(), ticketID)
	currentCol := ""
	if card != nil {
		currentCol = card.Status
	}

	moveModel := overlay.NewMove(ticketID, colNames, currentCol)
	// Send size
	sized, _ := moveModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayMove
	return a, nil
}

func (a App) handleMoveSelected(msg overlay.MoveSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	err := a.svc.MoveCard(context.Background(), msg.TicketID, msg.TargetColumn)
	if err != nil {
		return a, nil
	}
	// Refresh board data
	cmd := a.board.Init()
	// If in detail view, refresh the card
	if a.active == viewDetail {
		card, err := a.svc.GetCard(context.Background(), msg.TicketID)
		if err == nil {
			a.detail.SetCard(card)
		}
	}
	return a, cmd
}

func (a App) openHelpOverlay() (tea.Model, tea.Cmd) {
	a.activeOverlay = overlay.NewHelp(a.width, a.height)
	a.overlayType = overlayHelp
	return a, nil
}

func (a App) openSearchOverlay() (tea.Model, tea.Cmd) {
	a.activeOverlay = overlay.NewSearch(a.width, a.height)
	a.overlayType = overlaySearch
	return a, nil
}

func (a App) handleSearchQuery(msg overlay.SearchQueryChangedMsg) (tea.Model, tea.Cmd) {
	svc := a.svc
	query := msg.Query
	cmd := func() tea.Msg {
		results, err := svc.SearchCards(context.Background(), query)
		if err != nil {
			return SearchResultsMsg{}
		}
		return SearchResultsMsg{Results: results}
	}
	return a, cmd
}

func (a App) handleSearchSelected(msg overlay.SearchSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	// Navigate board cursor to the selected card
	a.board.NavigateTo(msg.CardID)
	return a, nil
}

// agentTickMsg is an internal tick for agent capture polling.
type agentTickMsg struct{}

func agentTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return agentTickMsg{}
	})
}

func (a App) switchToAgentView() (tea.Model, tea.Cmd) {
	a.active = viewAgents
	a.agentView.StartPolling()

	var cmds []tea.Cmd
	if a.agentSvc != nil {
		svc := a.agentSvc
		cmds = append(cmds, func() tea.Msg {
			_ = svc.ReconcileSessions(context.Background())
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList}
		})
	}
	cmds = append(cmds, agentTickCmd())
	return a, tea.Batch(cmds...)
}

func (a App) handleAgentTick() (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	selected := a.agentView.SelectedAgent()
	if selected == nil || selected.Status != "running" {
		return a, agentTickCmd()
	}
	svc := a.agentSvc
	ticketID := selected.TicketID
	return a, tea.Batch(
		func() tea.Msg {
			output, _ := svc.CaptureOutput(context.Background(), ticketID)
			return agents.CaptureOutputMsg{Output: output}
		},
		agentTickCmd(),
	)
}

func (a App) handleSpawnAgent(msg agents.SpawnAgentMsg) (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	ticketID := msg.TicketID
	// If no ticket ID, use selected board card
	if ticketID == "" {
		if card := a.board.SelectedCard(); card != nil {
			ticketID = card.Key
		}
	}
	if ticketID == "" {
		return a, nil
	}
	svc := a.agentSvc
	return a, func() tea.Msg {
		if err := svc.SpawnAgent(context.Background(), ticketID); err != nil {
			return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
		}
		agentList, _ := svc.ListAgents(context.Background())
		return agents.AgentsRefreshedMsg{Agents: agentList}
	}
}

func (a App) handleKillAgent(msg agents.KillAgentMsg) (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	svc := a.agentSvc
	ticketID := msg.TicketID
	return a, func() tea.Msg {
		_ = svc.KillAgent(context.Background(), ticketID)
		agentList, _ := svc.ListAgents(context.Background())
		return agents.AgentsRefreshedMsg{Agents: agentList}
	}
}

func (a App) handleAttachSession(msg agents.AttachSessionMsg) (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	// Find the ticket ID for the session
	selected := a.agentView.SelectedAgent()
	if selected == nil {
		return a, nil
	}
	svc := a.agentSvc
	ticketID := selected.TicketID
	cmd, err := svc.AttachCmd(context.Background(), ticketID)
	if err != nil {
		return a, nil
	}
	return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
		// After detach, refresh agent list
		agentList, _ := svc.ListAgents(context.Background())
		return agents.AgentsRefreshedMsg{Agents: agentList}
	})
}

func (a App) handleBoardSpawnAgent() (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	card := a.board.SelectedCard()
	if card == nil {
		return a, nil
	}

	ticketID := card.Key
	svc := a.agentSvc

	// Check if agent already exists for this card
	_, err := svc.CaptureOutput(context.Background(), ticketID)
	if err == nil {
		// Agent already running — switch to agent view with it selected
		a.active = viewAgents
		a.agentView.StartPolling()
		a.agentView.SelectByTicketID(ticketID)
		return a, tea.Batch(
			func() tea.Msg {
				_ = svc.ReconcileSessions(context.Background())
				agentList, _ := svc.ListAgents(context.Background())
				return agents.AgentsRefreshedMsg{Agents: agentList, SelectTicket: ticketID}
			},
			agentTickCmd(),
		)
	}

	// Spawn and switch to agent view
	a.active = viewAgents
	a.agentView.StartPolling()
	return a, tea.Batch(
		func() tea.Msg {
			if err := svc.SpawnAgent(context.Background(), ticketID); err != nil {
				return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
			}
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList, SelectTicket: ticketID}
		},
		agentTickCmd(),
	)
}

func (a App) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch a.active {
	case viewBoard:
		// 'A' switches to agent view
		if msg.String() == "A" {
			return a.switchToAgentView()
		}
		// 'a' spawns agent on selected card
		if msg.String() == "a" {
			return a.handleBoardSpawnAgent()
		}
		// 't' opens terminal for selected card (spawns if needed)
		if msg.String() == "t" {
			return a.handleBoardSpawnAgent()
		}
		var cmd tea.Cmd
		a.board, cmd = a.board.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case viewDetail:
		var detailModel tea.Model
		var cmd tea.Cmd
		detailModel, cmd = a.detail.Update(msg)
		a.detail = detailModel.(detail.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	case viewAgents:
		var cmd tea.Cmd
		a.agentView, cmd = a.agentView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return a, tea.Batch(cmds...)
}

// View renders the application.
func (a App) View() string {
	if a.overlayType != overlayNone && a.activeOverlay != nil {
		return a.activeOverlay.View()
	}

	switch a.active {
	case viewDetail:
		return a.detail.View()
	case viewAgents:
		statusBar := a.statusBar.View()
		statusBarHeight := lipgloss.Height(statusBar)
		// Set agent view height to fill space above status bar
		a.agentView.SetSize(a.width, a.height-statusBarHeight)
		content := a.agentView.View()
		return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
	default:
		content := a.board.View()
		statusBar := a.statusBar.View()
		statusBarHeight := lipgloss.Height(statusBar)

		// Pad board content to fill available height
		boardHeight := a.height - statusBarHeight
		if boardHeight > 0 {
			content = lipgloss.NewStyle().Height(boardHeight).Render(content)
		}

		return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
	}
}
