package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/clipboard"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/report"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/tui/theme"
)

type viewType int

const (
	viewBoard viewType = iota
	viewDetail
	viewAgents
	viewReport
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayMove
	overlaySearch
	overlayHelp
	overlayCreate
	overlayDelete
	overlayImport
	overlayTitleEdit
	overlayWorkspace
	overlayMoveWorkspace
	overlayArchive
	overlayLinkPR
	overlayOpenURL
	overlayEphemeralSpawn
)

// EventBusMsg wraps an event bus event as a Bubbletea message.
type EventBusMsg struct {
	Event events.Event
	ch    <-chan events.Event // source channel, for re-subscribing
}

// ClipboardWarningMsg signals that clipboard is unavailable.
type ClipboardWarningMsg struct{}

// SearchResultsMsg carries search results back to the app.
type SearchResultsMsg struct {
	Results []service.Card
}

// ImportRemoteResultsMsg carries remote search results back to the app.
type ImportRemoteResultsMsg struct {
	Results []service.RemoteSearchResult
	Err     string
}

// boardRefreshMsg triggers a board data reload.
type boardRefreshMsg struct{}

// manualRefreshDoneMsg triggers a board reload and clears the sync indicator.
type manualRefreshDoneMsg struct{}

// App is the root Bubbletea model.
type App struct {
	svc       service.BoardService
	syncSvc   service.SyncService
	agentSvc  service.AgentService
	board      board.Model
	detail     detail.Model
	agentView  agents.Model
	reportView report.Model
	statusBar  statusbar.Model
	clip          *clipboard.Clipboard
	editor        string
	activeOverlay tea.Model
	overlayType   overlayKind
	active        viewType
	width         int
	height        int
	pendingNav    string // card ID to navigate to after next board data load
	prSvc         service.PRTrackingService
	eventBus      *events.Bus
	eventSubs     []<-chan events.Event // sync lifecycle events (started/completed/failed/errors)
	cardUpdateSub <-chan events.Event
	prSub         <-chan events.Event
	tmux          service.TmuxManager
	webServer     *server.Server
	webServerStop func()
	webServerPort string
}

// NewApp creates a new root application model.
func NewApp(svc service.BoardService, syncSvc service.SyncService, agentSvc service.AgentService, prSvc service.PRTrackingService, reportSvc service.ReportService, icons theme.Icons, bus *events.Bus, editor string, workspaces []service.Workspace, tmux service.TmuxManager) App {
	clip := clipboard.New()
	b := board.New(svc, icons)
	b.SetWorkspaces(workspaces)
	app := App{
		svc:        svc,
		syncSvc:    syncSvc,
		agentSvc:   agentSvc,
		prSvc:      prSvc,
		board:      b,
		agentView:  agents.New(icons),
		reportView: report.New(reportSvc),
		statusBar:  statusbar.New(),
		clip:       clip,
		editor:        editor,
		active:        viewBoard,
		eventBus:      bus,
		tmux:          tmux,
		webServerPort: "3080",
	}
	if bus != nil {
		app.eventSubs = []<-chan events.Event{
			bus.Subscribe(events.EventSyncStarted),
			bus.Subscribe(events.EventSyncCompleted),
			bus.Subscribe(events.EventSyncFailed),
			bus.Subscribe(events.EventSyncError),
			bus.Subscribe(events.EventAuthFailed),
			bus.Subscribe(events.EventRateLimited),
			bus.Subscribe(events.EventTransitionFailed),
		}
		app.cardUpdateSub = bus.Subscribe(events.EventCardUpdated)
		app.prSub = bus.Subscribe(events.EventPRStatusUpdated)
	}
	return app
}

// Init returns initial commands.
func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.board.Init()}
	for _, sub := range a.eventSubs {
		cmds = append(cmds, a.listenEventBusCh(sub))
	}
	if a.cardUpdateSub != nil {
		cmds = append(cmds, a.listenCardUpdates())
	}
	if a.prSub != nil {
		cmds = append(cmds, a.listenPRUpdates())
	}
	// Clipboard availability check
	if a.clip == nil || !a.clip.Available() {
		a.statusBar, _ = a.statusBar.Update(statusbar.WarningMsg{
			Text: "clipboard unavailable -- install xclip or wl-copy",
		})
	}
	return tea.Batch(cmds...)
}

// listenEventBusCh returns a command that bridges a single EventBus channel into Bubbletea messages.
func (a App) listenEventBusCh(ch <-chan events.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return EventBusMsg{Event: event, ch: ch}
	}
}

// cardUpdateMsg triggers a board data reload when agent state or card data changes via IPC.
type cardUpdateMsg struct{}

// listenCardUpdates returns a command that listens for EventCardUpdated and triggers a refresh.
func (a App) listenCardUpdates() tea.Cmd {
	ch := a.cardUpdateSub
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return cardUpdateMsg{}
	}
}

// prUpdateMsg triggers a board data reload when PR status changes.
type prUpdateMsg struct{}

// listenPRUpdates returns a command that listens for EventPRStatusUpdated.
func (a App) listenPRUpdates() tea.Cmd {
	ch := a.prSub
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return prUpdateMsg{}
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
		case "r":
			if a.active == viewBoard {
				return a.manualRefresh()
			}
			return a.delegateKey(msg)
		case "n":
			if a.active == viewBoard {
				return a.openCreateOverlay()
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

		// Propagate to report view
		a.reportView.SetSize(msg.Width, msg.Height-1)

		// Propagate to status bar
		a.statusBar, _ = a.statusBar.Update(msg)

	case board.OpenDetailMsg:
		a.active = viewDetail
		// Load card synchronously — GetCard hits local SQLite, not remote API
		card, err := a.svc.GetCard(context.Background(), msg.CardKey)
		if err != nil {
			a.detail = detail.NewLoading(msg.CardKey, a.svc, a.clip, a.editor)
		} else {
			a.detail = detail.New(card, a.svc, a.clip, a.editor)
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

	case report.ReturnToBoardMsg:
		a.active = viewBoard
		return a, nil

	case report.CopyReportMsg:
		if a.clip != nil && a.clip.Available() {
			if err := a.clip.Copy(msg.Markdown); err == nil {
				a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: "Report copied to clipboard"})
			}
		}
		return a, nil

	case report.ReportLoadedMsg:
		a.reportView, _ = a.reportView.Update(msg)
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
		return a.openMoveOverlay(msg.TaskID)

	case board.OpenMoveMsg:
		return a.openMoveOverlay(msg.CardKey)

	case overlay.MoveSelectedMsg:
		return a.handleMoveSelected(msg)

	case overlay.MoveCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.OpenMoveWorkspaceMsg:
		return a.openMoveWorkspaceOverlay(msg.TaskID)

	case overlay.WorkspaceAssignedMsg:
		return a.handleWorkspaceAssigned(msg)

	case overlay.WorkspaceAssignCancelledMsg:
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

	case overlay.CreateTaskMsg:
		return a.handleCreateTask(msg)

	case overlay.CreateCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case board.OpenDeleteMsg:
		return a.openDeleteOverlay(msg.CardKey)

	case detail.OpenDeleteOverlay:
		return a.openDeleteOverlay(msg.TaskID)

	case overlay.DeleteConfirmedMsg:
		return a.handleDeleteConfirmed(msg)

	case overlay.DeleteCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.ArchiveConfirmedMsg:
		return a.handleArchiveConfirmed()

	case overlay.ArchiveCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case board.OpenWorkspaceMsg:
		return a.openWorkspaceOverlay()

	case overlay.WorkspaceSelectedMsg:
		return a.handleWorkspaceSelected(msg)

	case overlay.WorkspaceCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case board.OpenImportMsg:
		return a.openImportOverlay()

	case board.OpenLinkPRMsg:
		return a.openLinkPROverlay(msg.CardKey)

	case overlay.ImportQueryChangedMsg:
		return a.handleImportQuery(msg)

	case overlay.ImportSelectedMsg:
		return a.handleImportSelected(msg)

	case overlay.ImportCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.LinkPRFetchMsg:
		return a.handleLinkPRFetch(msg)

	case overlay.LinkPRResultMsg:
		if a.overlayType == overlayLinkPR {
			lo := a.activeOverlay.(overlay.LinkPROverlay)
			if msg.Err != "" {
				lo = lo.SetError(msg.Err)
			} else {
				lo = lo.SetResult(msg.Status)
			}
			a.activeOverlay = lo
		}
		return a, nil

	case overlay.LinkPRConfirmedMsg:
		return a.handleLinkPRConfirmed(msg)

	case overlay.LinkPRCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case detail.OpenURLPickerMsg:
		urlOverlay := overlay.NewOpenURL(msg.ProviderURL, msg.PRURL, a.width, a.height)
		a.activeOverlay = urlOverlay
		a.overlayType = overlayOpenURL
		return a, nil

	case overlay.OpenURLSelectedMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		if err := clipboard.OpenURL(msg.URL); err != nil {
			return a, func() tea.Msg {
				return statusbar.ErrorMsg{Text: fmt.Sprintf("Open failed: %v", err)}
			}
		}
		return a, nil

	case overlay.OpenURLCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case detail.OpenTitleEditOverlay:
		return a.openTitleEditOverlay(msg.TaskID, msg.Title)

	case overlay.TitleEditSubmitMsg:
		return a.handleTitleEditSubmit(msg)

	case overlay.TitleEditCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case agents.OpenEphemeralSpawnMsg:
		return a.openEphemeralSpawnOverlay()

	case overlay.EphemeralSpawnSubmitMsg:
		return a.handleEphemeralSpawnSubmit(msg)

	case overlay.EphemeralSpawnCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case ImportRemoteResultsMsg:
		if a.overlayType == overlayImport {
			io := a.activeOverlay.(overlay.ImportOverlay)
			if msg.Err != "" {
				io = io.SetError(msg.Err)
			} else {
				io = io.SetResults(msg.Results)
			}
			a.activeOverlay = io
		}
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
		// Continue listening on the same channel
		if msg.ch != nil {
			cmds = append(cmds, a.listenEventBusCh(msg.ch))
		}

	case cardUpdateMsg:
		// IPC triggered a card/agent state change — reload board data.
		cmds = append(cmds, a.board.Init())
		// Also refresh agent list if in agent view
		if a.active == viewAgents && a.agentSvc != nil {
			svc := a.agentSvc
			cmds = append(cmds, func() tea.Msg {
				agentList, _ := svc.ListAgents(context.Background())
				return agents.AgentsRefreshedMsg{Agents: agentList}
			})
		}
		if a.cardUpdateSub != nil {
			cmds = append(cmds, a.listenCardUpdates())
		}
		return a, tea.Batch(cmds...)

	case prUpdateMsg:
		// PR status changed — reload board data.
		cmds = append(cmds, a.board.Init())
		if a.prSub != nil {
			cmds = append(cmds, a.listenPRUpdates())
		}
		return a, tea.Batch(cmds...)

	case boardRefreshMsg:
		cmd := a.board.Init()
		return a, cmd

	case manualRefreshDoneMsg:
		a.statusBar, _ = a.statusBar.Update(statusbar.SyncCompletedMsg{})
		cmd := a.board.Init()
		return a, cmd

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
		// Enrich cards with active agent indicators and activity states
		if a.agentSvc != nil {
			agentList, err := a.agentSvc.ListAgents(context.Background())
			if err == nil {
				active := make(map[string]bool, len(agentList))
				states := make(map[string]string, len(agentList))
				for _, ag := range agentList {
					if ag.Status == "running" {
						active[ag.TaskID] = true
						if ag.Activity != "" {
							states[ag.TaskID] = ag.Activity
						}
					}
				}
				a.board.SetActiveAgents(active)
				a.board.SetAgentStates(states)

				// Populate duration data for agent view
				agentTaskIDs := make([]string, 0, len(agentList))
				for _, ag := range agentList {
					agentTaskIDs = append(agentTaskIDs, ag.TaskID)
				}
				if len(agentTaskIDs) > 0 {
					agentDurations, dErr := a.agentSvc.GetTaskDurations(context.Background(), agentTaskIDs)
					if dErr == nil && len(agentDurations) > 0 {
						avDurations := make(map[string]agents.DurationData, len(agentDurations))
						for id, d := range agentDurations {
							avDurations[id] = agents.DurationData{
								Working: d.Working,
								Waiting: d.Waiting,
							}
						}
						a.agentView.SetDurations(avDurations)
					}
				}
			}
			// Populate duration data for all visible cards
			if taskIDs := a.board.TaskIDs(); len(taskIDs) > 0 {
				durations, err := a.agentSvc.GetTaskDurations(context.Background(), taskIDs)
				if err == nil && len(durations) > 0 {
					boardDurations := make(map[string]board.DurationData, len(durations))
					for id, d := range durations {
						boardDurations[id] = board.DurationData{
							Working: d.Working,
							Waiting: d.Waiting,
						}
					}
					a.board.SetDurations(boardDurations)
				}
			}
		}
		// Enrich cards with PR state indicators
		if a.prSvc != nil {
			if taskIDs := a.board.TaskIDs(); len(taskIDs) > 0 {
				prStates := make(map[string]board.PRStateData)
				for _, id := range taskIDs {
					meta, err := a.prSvc.GetPRStatus(context.Background(), id)
					if err == nil && meta != nil && meta.PRNumber > 0 {
						prStates[id] = board.PRStateData{
							CheckStatus:    meta.CheckStatus,
							ReviewDecision: meta.ReviewDecision,
							CommentCount:   meta.CommentCount,
							IsDraft:        meta.IsDraft,
							PRNumber:       meta.PRNumber,
						}
					}
				}
				if len(prStates) > 0 {
					a.board.SetPRStates(prStates)
				}
			}
		}
		// Apply pending navigation (e.g. after task creation)
		if a.pendingNav != "" {
			a.board.NavigateTo(a.pendingNav)
			a.pendingNav = ""
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

func (a App) openMoveOverlay(taskID string) (tea.Model, tea.Cmd) {
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
	card, _ := a.svc.GetCard(context.Background(), taskID)
	currentCol := ""
	if card != nil {
		currentCol = card.Status
	}

	moveModel := overlay.NewMove(taskID, colNames, currentCol)
	// Send size
	sized, _ := moveModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayMove
	return a, nil
}

func (a App) handleMoveSelected(msg overlay.MoveSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	err := a.svc.MoveCard(context.Background(), msg.TaskID, msg.TargetColumn)
	if err != nil {
		return a, nil
	}
	// Refresh board data
	cmd := a.board.Init()
	// If in detail view, refresh the card
	if a.active == viewDetail {
		card, err := a.svc.GetCard(context.Background(), msg.TaskID)
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

func (a App) openCreateOverlay() (tea.Model, tea.Cmd) {
	cols, err := a.svc.ListColumns(context.Background())
	if err != nil || len(cols) == 0 {
		return a, nil
	}
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}
	// Default to the column the cursor is in
	currentCol := colNames[0]
	if selected := a.board.SelectedCard(); selected != nil {
		for _, col := range colNames {
			cards, _ := a.svc.ListCards(context.Background(), col)
			for _, c := range cards {
				if c.ID == selected.Key {
					currentCol = col
				}
			}
		}
	}
	// Determine active workspace ID for pre-filling
	var activeWorkspaceID *int
	wsView := a.board.WorkspaceView()
	if wsView.Kind == store.ViewWorkspace {
		activeWorkspaceID = &wsView.WorkspaceID
	}
	createModel := overlay.NewCreateWithWorkspaces(colNames, currentCol, a.board.Workspaces(), activeWorkspaceID)
	sized, _ := createModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayCreate
	return a, nil
}

func (a App) openDeleteOverlay(taskID string) (tea.Model, tea.Cmd) {
	card, err := a.svc.GetCard(context.Background(), taskID)
	if err != nil {
		return a, nil
	}
	isRemote := card.Provider != ""
	deleteModel := overlay.NewDelete(taskID, card.Title, isRemote)
	sized, _ := deleteModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayDelete
	return a, nil
}

func (a App) handleDeleteConfirmed(msg overlay.DeleteConfirmedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	_ = a.svc.DeleteTask(context.Background(), msg.TaskID)
	// If we were in detail view, return to board
	if a.active == viewDetail {
		a.active = viewBoard
	}
	// Refresh board
	cmd := a.board.Init()
	return a, cmd
}

func (a App) openArchiveOverlay() (tea.Model, tea.Cmd) {
	count, err := a.svc.CountDoneCards(context.Background())
	if err != nil || count == 0 {
		return a, nil
	}
	archiveModel := overlay.NewArchive(count)
	sized, _ := archiveModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayArchive
	return a, nil
}

func (a App) handleArchiveConfirmed() (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	_, _ = a.svc.ArchiveDoneCards(context.Background())
	cmd := a.board.Init()
	return a, cmd
}

func (a App) openMoveWorkspaceOverlay(taskID string) (tea.Model, tea.Cmd) {
	workspaces := a.board.Workspaces()
	card, err := a.svc.GetCard(context.Background(), taskID)
	if err != nil {
		return a, nil
	}

	mwModel := overlay.NewMoveWorkspace(taskID, workspaces, card.WorkspaceID)
	sized, _ := mwModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayMoveWorkspace
	return a, nil
}

func (a App) handleWorkspaceAssigned(msg overlay.WorkspaceAssignedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	err := a.svc.UpdateTaskWorkspace(context.Background(), msg.TaskID, msg.WorkspaceID)
	if err != nil {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: "workspace: " + err.Error()}
		}
	}
	cmd := a.board.Init()
	return a, cmd
}

func (a App) openWorkspaceOverlay() (tea.Model, tea.Cmd) {
	workspaces := a.board.Workspaces()
	current := a.board.WorkspaceView()
	wsModel := overlay.NewWorkspace(workspaces, current)
	sized, _ := wsModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayWorkspace
	return a, nil
}

func (a App) handleWorkspaceSelected(msg overlay.WorkspaceSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	a.board.SetWorkspaceView(msg.View)
	// Update status bar with workspace indicator
	wsName := "All"
	wsColor := ""
	if msg.View.Kind == store.ViewUnassigned {
		wsName = "Unassigned"
	} else if msg.View.Kind == store.ViewWorkspace {
		for _, ws := range a.board.Workspaces() {
			if ws.ID == msg.View.WorkspaceID {
				wsName = ws.Name
				wsColor = ws.Color
				break
			}
		}
	}
	a.statusBar, _ = a.statusBar.Update(statusbar.WorkspaceMsg{Name: wsName, Color: wsColor})
	// Refresh board data with new filter
	cmd := a.board.Init()
	return a, cmd
}

func (a App) openImportOverlay() (tea.Model, tea.Cmd) {
	if a.syncSvc == nil {
		return a, nil
	}
	var activeWorkspaceID *int
	wsView := a.board.WorkspaceView()
	if wsView.Kind == store.ViewWorkspace {
		activeWorkspaceID = &wsView.WorkspaceID
	}
	importModel := overlay.NewImport(a.width, a.height, activeWorkspaceID)
	a.activeOverlay = importModel
	a.overlayType = overlayImport
	return a, nil
}

func (a App) handleImportQuery(msg overlay.ImportQueryChangedMsg) (tea.Model, tea.Cmd) {
	if a.syncSvc == nil {
		return a, nil
	}
	svc := a.syncSvc
	query := msg.Query
	cmd := func() tea.Msg {
		results, err := svc.SearchRemote(context.Background(), query)
		if err != nil {
			return ImportRemoteResultsMsg{Err: err.Error()}
		}
		return ImportRemoteResultsMsg{Results: results}
	}
	return a, cmd
}

func (a App) handleImportSelected(msg overlay.ImportSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.syncSvc == nil {
		return a, nil
	}
	svc := a.syncSvc
	ticketID := msg.TicketID
	workspaceID := msg.WorkspaceID
	return a, func() tea.Msg {
		card, err := svc.ImportRemoteTask(context.Background(), ticketID, workspaceID)
		if err != nil {
			return statusbar.ErrorMsg{Text: "import failed: " + err.Error()}
		}
		_ = card
		// Return a board refresh by re-initializing board data
		return boardRefreshMsg{}
	}
}

func (a App) manualRefresh() (tea.Model, tea.Cmd) {
	syncSvc := a.syncSvc
	prSvc := a.prSvc
	if syncSvc == nil && prSvc == nil {
		// No services — just reload board data
		return a, a.board.Init()
	}
	if syncSvc == nil {
		// No Jira sync → no bus event will fire, so show indicator manually
		a.statusBar, _ = a.statusBar.Update(statusbar.SyncStartedMsg{})
	}
	// When syncSvc is present, Sync() publishes EventSyncStarted to the bus,
	// which the TUI already routes to the status bar — no pre-emptive update needed.
	return a, func() tea.Msg {
		if syncSvc != nil {
			syncSvc.Sync(context.Background())
		}
		if prSvc != nil {
			prSvc.PollAll(context.Background())
		}
		return manualRefreshDoneMsg{}
	}
}

func (a App) openLinkPROverlay(taskID string) (tea.Model, tea.Cmd) {
	if a.prSvc == nil {
		return a, nil
	}
	// Try to detect repo for pre-fill
	defaultRepo := ""
	if owner, repo, err := a.prSvc.DetectRepo(); err == nil {
		defaultRepo = owner + "/" + repo
	}
	linkModel := overlay.NewLinkPR(taskID, defaultRepo, a.width, a.height)
	a.activeOverlay = linkModel
	a.overlayType = overlayLinkPR
	return a, nil
}

func (a App) handleLinkPRFetch(msg overlay.LinkPRFetchMsg) (tea.Model, tea.Cmd) {
	if a.prSvc == nil {
		return a, nil
	}
	prSvc := a.prSvc
	return a, func() tea.Msg {
		status, err := prSvc.FetchPRByNumber(msg.Owner, msg.Repo, msg.PRNumber)
		if err != nil {
			return overlay.LinkPRResultMsg{Err: err.Error()}
		}
		return overlay.LinkPRResultMsg{Status: status}
	}
}

func (a App) handleLinkPRConfirmed(msg overlay.LinkPRConfirmedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.prSvc == nil {
		return a, nil
	}
	prSvc := a.prSvc
	return a, func() tea.Msg {
		if err := prSvc.LinkPR(context.Background(), msg.TaskID, msg.Owner, msg.Repo, msg.PRNumber); err != nil {
			return statusbar.ErrorMsg{Text: "link PR failed: " + err.Error()}
		}
		return boardRefreshMsg{}
	}
}

func (a App) openTitleEditOverlay(taskID, currentTitle string) (tea.Model, tea.Cmd) {
	editModel := overlay.NewTitleEdit(taskID, currentTitle)
	sized, _ := editModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayTitleEdit
	return a, nil
}

func (a App) handleTitleEditSubmit(msg overlay.TitleEditSubmitMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil

	err := a.svc.UpdateTaskTitle(context.Background(), msg.TaskID, msg.Title)
	if err != nil {
		return a, nil
	}

	var cmds []tea.Cmd
	// Refresh board
	cmds = append(cmds, a.board.Init())

	// If in detail view, update the card title
	if a.active == viewDetail {
		card, err := a.svc.GetCard(context.Background(), msg.TaskID)
		if err == nil {
			a.detail.SetCard(card)
		}
		detailModel, cmd := a.detail.Update(detail.TitleUpdatedMsg{Title: msg.Title})
		a.detail = detailModel.(detail.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) openEphemeralSpawnOverlay() (tea.Model, tea.Cmd) {
	spawnModel := overlay.NewEphemeralSpawn()
	sized, _ := spawnModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayEphemeralSpawn
	return a, nil
}

func (a App) handleEphemeralSpawnSubmit(msg overlay.EphemeralSpawnSubmitMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil

	if a.agentSvc == nil {
		return a, nil
	}

	svc := a.agentSvc
	title := msg.Title
	termW := a.width - agents.SidebarWidth
	if termW < 1 {
		termW = 1
	}
	termH := a.height - 1

	return a, tea.Batch(
		func() tea.Msg {
			if err := svc.SpawnEphemeralAgent(context.Background(), title, termW, termH); err != nil {
				return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
			}
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList}
		},
		agentTickCmd(),
	)
}

func (a App) handleCreateTask(msg overlay.CreateTaskMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	card, err := a.svc.CreateTask(context.Background(), msg.Title, msg.Description, msg.Column, msg.Priority, msg.WorkspaceID)
	if err != nil {
		return a, nil
	}
	// Refresh board — navigation happens when DataLoadedMsg arrives
	cmd := a.board.Init()
	if card != nil {
		a.pendingNav = card.ID
	}
	return a, cmd
}

func (a App) switchToReportView() (tea.Model, tea.Cmd) {
	a.active = viewReport
	cmd := a.reportView.Init()
	return a, cmd
}

// webServerStartedMsg signals that the web server started.
type webServerStartedMsg struct{ port string }

// webServerStoppedMsg signals that the web server stopped.
type webServerStoppedMsg struct{}

func (a App) toggleWebServer() (tea.Model, tea.Cmd) {
	if a.webServer != nil {
		// Stop the server.
		srv := a.webServer
		stopFn := a.webServerStop
		a.webServer = nil
		a.webServerStop = nil
		a.statusBar = a.statusBar.ClearWebServer()
		return a, func() tea.Msg {
			if stopFn != nil {
				stopFn()
			}
			srv.Stop(context.Background())
			return webServerStoppedMsg{}
		}
	}

	// Start the server.
	addr := ":" + a.webServerPort
	srv := server.New(a.svc, a.agentSvc, a.tmux, addr)
	a.webServer = srv
	a.statusBar = a.statusBar.SetWebServer(a.webServerPort)

	// Bridge event bus → web server: notify WebSocket clients on state changes.
	stopCh := make(chan struct{})
	a.webServerStop = func() { close(stopCh) }
	if a.eventBus != nil {
		cardCh := a.eventBus.Subscribe(events.EventCardUpdated)
		prCh := a.eventBus.Subscribe(events.EventPRStatusUpdated)
		go func() {
			for {
				select {
				case <-stopCh:
					return
				case <-cardCh:
					srv.NotifyAgentsChanged()
				case <-prCh:
					srv.NotifyAgentsChanged()
				}
			}
		}()
	}

	return a, func() tea.Msg {
		if err := srv.Start(); err != nil {
			return webServerStoppedMsg{}
		}
		return nil
	}
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
	taskID := selected.TaskID
	return a, tea.Batch(
		func() tea.Msg {
			output, _ := svc.CaptureOutput(context.Background(), taskID)
			return agents.CaptureOutputMsg{Output: output}
		},
		agentTickCmd(),
	)
}

func (a App) handleSpawnAgent(msg agents.SpawnAgentMsg) (tea.Model, tea.Cmd) {
	if a.agentSvc == nil {
		return a, nil
	}
	taskID := msg.TaskID
	// If no ticket ID, use selected board card
	if taskID == "" {
		if card := a.board.SelectedCard(); card != nil {
			taskID = card.Key
		}
	}
	if taskID == "" {
		return a, nil
	}
	svc := a.agentSvc
	w, h := msg.Width, msg.Height
	return a, func() tea.Msg {
		if err := svc.SpawnAgent(context.Background(), taskID, w, h); err != nil {
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
	taskID := msg.TaskID
	return a, func() tea.Msg {
		_ = svc.KillAgent(context.Background(), taskID)
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
	taskID := selected.TaskID
	cmd, err := svc.AttachCmd(context.Background(), taskID)
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

	taskID := card.Key
	svc := a.agentSvc

	// Check if agent already exists for this card
	_, err := svc.CaptureOutput(context.Background(), taskID)
	if err == nil {
		// Agent already running — switch to agent view with it selected
		a.active = viewAgents
		a.agentView.StartPolling()
		a.agentView.SelectByTaskID(taskID)
		return a, tea.Batch(
			func() tea.Msg {
				_ = svc.ReconcileSessions(context.Background())
				agentList, _ := svc.ListAgents(context.Background())
				return agents.AgentsRefreshedMsg{Agents: agentList, SelectTask: taskID}
			},
			agentTickCmd(),
		)
	}

	// Spawn and switch to agent view
	a.active = viewAgents
	a.agentView.StartPolling()
	// Compute terminal dimensions for tmux: total width minus sidebar
	termW := a.width - agents.SidebarWidth
	if termW < 1 {
		termW = 1
	}
	termH := a.height - 1 // reserve for status bar
	return a, tea.Batch(
		func() tea.Msg {
			if err := svc.SpawnAgent(context.Background(), taskID, termW, termH); err != nil {
				return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
			}
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList, SelectTask: taskID}
		},
		agentTickCmd(),
	)
}

func (a App) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch a.active {
	case viewBoard:
		// 'X' opens archive confirmation for done cards
		if msg.String() == "X" {
			return a.openArchiveOverlay()
		}
		// 'A' switches to agent view
		if msg.String() == "A" {
			return a.switchToAgentView()
		}
		// 'S' switches to report view
		if msg.String() == "S" {
			return a.switchToReportView()
		}
		// 'W' toggles web server
		if msg.String() == "W" {
			return a.toggleWebServer()
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
	case viewReport:
		var cmd tea.Cmd
		a.reportView, cmd = a.reportView.Update(msg)
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
	case viewReport:
		statusBar := a.statusBar.View()
		statusBarHeight := lipgloss.Height(statusBar)
		a.reportView.SetSize(a.width, a.height-statusBarHeight)
		content := a.reportView.View()
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
