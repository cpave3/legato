package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/clipboard"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/report"
	"github.com/cpave3/legato/internal/tui/review"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/tui/theme"
)

type viewType int

const (
	viewBoard viewType = iota
	viewDetail
	viewAgents
	viewReport
	viewReview
)

// VoiceService is the interface the TUI uses for voice dictation. The
// concrete implementation is service.VoiceService.
type WorktreeService interface {
	Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (*store.TaskWorktree, error)
	Path(ctx context.Context, taskID string) (string, error)
}

type VoiceService interface {
	StartRecording(device string) error
	IsRecording() bool
	Stop() (string, error)
	Transcribe(ctx context.Context) (string, error)
	Deliver(ctx context.Context, tmuxSession, agentKind, text string, autoSend bool) error
	Levels() []float64
	Cleanup()
	AutoSend() bool
	MicDevice() string
}

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
	overlayAgentSpawn
	overlayChimeraSession
	overlaySwarmInit
	overlayPlanApproval
	overlayAgentAction
	overlayMacroPicker
	overlaySwarmCancel
	overlayWorktree
	overlayGroup
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

// importRemoteDoneMsg carries the result of an asynchronous remote ticket import.
type importRemoteDoneMsg struct {
	TicketID string
	Err      string
}

// worktreeCreateDoneMsg carries the result of asynchronous worktree creation.
type worktreeCreateDoneMsg struct {
	TaskID string
	Err    string
}

// manualRefreshDoneMsg triggers a board reload and clears the sync indicator.
type manualRefreshDoneMsg struct{}

// App is the root Bubbletea model.
type App struct {
	svc                service.BoardService
	syncSvc            service.SyncService
	agentSvc           service.AgentService
	board              board.Model
	detail             detail.Model
	agentView          agents.Model
	reportView         report.Model
	reviewView         review.Model
	reviewSvc          review.Service
	statusBar          statusbar.Model
	clip               *clipboard.Clipboard
	editor             string
	activeOverlay      tea.Model
	overlayType        overlayKind
	active             viewType
	width              int
	height             int
	pendingNav         string // card ID to navigate to after next board data load
	pendingImport      string // remote ticket ID waiting to appear on the board
	prSvc              service.PRTrackingService
	eventBus           *events.Bus
	eventSubs          []<-chan events.Event // sync lifecycle events (started/completed/failed/errors)
	cardUpdateSub      <-chan events.Event
	prSub              <-chan events.Event
	swarmSub           <-chan events.Event
	planSub            <-chan events.Event
	reviewSub          <-chan events.Event
	tmux               service.TmuxManager
	swarmSvc           service.SwarmService
	workDir            string
	webServer          *server.Server
	webServerStop      func()
	webServerPort      string
	macros             []macros.Macro
	lastSparklineFetch time.Time
	sparklineWindow    time.Duration
	sparklineBuckets   int
	voiceSvc           VoiceService
	voiceEnabled       bool
	voiceAutoSend      bool
	voiceMicDevice     string
	voiceRecording     bool
	voiceTargetSession string
	voiceTargetKind    string
	worktreeSvc        WorktreeService
	worktreesEnabled   bool
	groupDefaults      []string
}

// SetWebServerRunning tells the TUI that the web server was auto-started
// externally, so the status bar shows the indicator without the TUI managing
// the server lifecycle.
// SetWorktreeService enables task worktree actions.
func (a *App) SetWorktreeService(svc WorktreeService) {
	a.worktreeSvc = svc
	a.worktreesEnabled = svc != nil
}

// SetGroupDefaults configures the group choices always shown in the group modal.
func (a *App) SetGroupDefaults(defaults []string) {
	a.groupDefaults = append([]string(nil), defaults...)
}

// SetReviewService enables the review queue and tour view.
func (a *App) SetReviewService(svc review.Service) {
	a.reviewSvc = svc
	a.reviewView = review.New(svc)
}

func (a *App) SetWebServerRunning(port string) {
	a.webServerPort = port
	a.statusBar = a.statusBar.SetWebServer(port)
}

// SetNtfyConfigured tells the agent view and status bar whether a
// notification channel is available, so the 'n' keybinding shows conditionally.
func (a *App) SetNtfyConfigured(v bool) {
	a.agentView.SetNtfyConfigured(v)
	a.statusBar, _ = a.statusBar.Update(statusbar.NtfyMsg{Configured: v})
}

// SetVoiceService wires the voice dictation service into the TUI. When svc
// is non-nil, the 'v' keybinding is enabled in the agents view and status bar.
// autoSend and micDevice are forwarded from config.
func (a *App) SetVoiceService(svc VoiceService, autoSend bool, micDevice string) {
	a.voiceSvc = svc
	a.voiceEnabled = svc != nil
	a.voiceAutoSend = autoSend
	a.voiceMicDevice = micDevice
	a.agentView.SetVoiceEnabled(a.voiceEnabled)
	a.statusBar, _ = a.statusBar.Update(statusbar.VoiceMsg{Enabled: a.voiceEnabled})
}

// SetSparklineWindow configures the window and bucket count used when
// fetching state timelines for the agent sidebar. Zero or negative values are
// ignored (the handler's fallback applies).
func (a *App) SetSparklineWindow(window time.Duration, buckets int) {
	if window > 0 {
		a.sparklineWindow = window
	}
	if buckets > 0 {
		a.sparklineBuckets = buckets
	}
}

// NewApp creates a new root application model. Pass nil for swarmSvc to
// disable swarm UI (S keybinding is no-op, swarm panels hidden).
func NewApp(svc service.BoardService, syncSvc service.SyncService, agentSvc service.AgentService, prSvc service.PRTrackingService, reportSvc service.ReportService, icons theme.Icons, bus *events.Bus, editor string, workspaces []service.Workspace, tmux service.TmuxManager, workDir string, swarmSvc service.SwarmService, macrosList []macros.Macro, reviewServices ...review.Service) App {
	clip := clipboard.New()
	b := board.New(svc, icons)
	b.SetWorkspaces(workspaces)
	app := App{
		svc:           svc,
		syncSvc:       syncSvc,
		agentSvc:      agentSvc,
		prSvc:         prSvc,
		board:         b,
		agentView:     agents.New(icons),
		reportView:    report.New(reportSvc),
		statusBar:     statusbar.New(),
		clip:          clip,
		editor:        editor,
		active:        viewBoard,
		eventBus:      bus,
		tmux:          tmux,
		workDir:       workDir,
		webServerPort: "3080",
		swarmSvc:      swarmSvc,
		macros:        macrosList,
	}
	if len(reviewServices) > 0 && reviewServices[0] != nil {
		app.SetReviewService(reviewServices[0])
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
		app.swarmSub = bus.Subscribe(events.EventSwarmChanged)
		app.planSub = bus.Subscribe(events.EventPlanProposed)
		app.reviewSub = bus.Subscribe(events.EventReviewChanged)
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
	if a.swarmSub != nil {
		cmds = append(cmds, a.listenSwarmUpdates())
	}
	if a.planSub != nil {
		cmds = append(cmds, a.listenPlanProposals())
	}
	if a.reviewSub != nil {
		cmds = append(cmds, a.listenReviewUpdates())
	}
	if a.reviewSvc != nil {
		cmds = append(cmds, a.loadReviewBadges())
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

// swarmUpdateMsg triggers a board data reload when swarm state changes.
type swarmUpdateMsg struct {
	Payload events.SwarmChangedPayload
}

type reviewUpdateMsg struct {
	Payload events.ReviewChangedPayload
}

type reviewBadgesMsg struct {
	States map[string]service.ReviewBadgeState
}

func (a App) listenReviewUpdates() tea.Cmd {
	ch := a.reviewSub
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		payload, _ := ev.Payload.(events.ReviewChangedPayload)
		return reviewUpdateMsg{Payload: payload}
	}
}

func (a App) loadReviewBadges() tea.Cmd {
	svc, ok := a.reviewSvc.(interface {
		ReviewBadgeStates(context.Context) (map[string]service.ReviewBadgeState, error)
	})
	if !ok {
		return nil
	}
	return func() tea.Msg {
		states, _ := svc.ReviewBadgeStates(context.Background())
		return reviewBadgesMsg{States: states}
	}
}

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

// listenSwarmUpdates returns a command that listens for EventSwarmChanged.
func (a App) listenSwarmUpdates() tea.Cmd {
	ch := a.swarmSub
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		var payload events.SwarmChangedPayload
		if p, ok2 := ev.Payload.(events.SwarmChangedPayload); ok2 {
			payload = p
		}
		return swarmUpdateMsg{Payload: payload}
	}
}

// planProposalMsg carries a plan-proposed payload from the IPC server through
// to the bubbletea Update loop, which opens the approval overlay.
type planProposalMsg struct {
	ParentTaskID string
	PlanPath     string
	ReplySocket  string
	Mode         string
}

// listenPlanProposals returns a command that listens for EventPlanProposed.
func (a App) listenPlanProposals() tea.Cmd {
	ch := a.planSub
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		p, ok := ev.Payload.(events.PlanProposedPayload)
		if !ok {
			return nil
		}
		return planProposalMsg{
			ParentTaskID: p.ParentTaskID,
			PlanPath:     p.PlanPath,
			ReplySocket:  p.ReplySocket,
			Mode:         p.Mode,
		}
	}
}

// Update handles messages.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// ? opens help unless an active text input owns printable keys.
		if msg.String() == "?" && !(a.active == viewReview && a.reviewView.InputFocused()) {
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
		case "esc":
			// Escape during recording cancels the recording without
			// transcribing — stops arecord and discards the audio.
			if a.voiceRecording {
				a.voiceRecording = false
				a.voiceSvc.Stop()
				a.voiceSvc.Cleanup()
				a.agentView, _ = a.agentView.Update(agents.VoiceRecordingMsg{Recording: false})
				return a, func() tea.Msg {
					return statusbar.InfoMsg{Text: "voice: cancelled"}
				}
			}
			return a.delegateKey(msg)
		case "q", "ctrl+c":
			if a.active == viewBoard {
				return a, tea.Quit
			}
			// In detail or agent view, q goes back to board
			if a.active == viewAgents {
				a.agentView.StopPolling()
			}
			a = a.setMode(viewBoard)
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
		case "S":
			if a.active == viewBoard && a.swarmSvc != nil {
				return a.openSwarmInitOverlay()
			}
			return a.delegateKey(msg)
		case "C":
			if a.active == viewBoard && a.swarmSvc != nil {
				return a.openSwarmCancelOverlay()
			}
			return a.delegateKey(msg)
		case "R":
			if a.active == viewBoard {
				return a.switchToReportView()
			}
			return a.delegateKey(msg)
		case "V":
			if a.active == viewBoard && a.reviewSvc != nil {
				a = a.setMode(viewReview)
				return a, a.reviewView.Init()
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

		// Propagate to report and review views
		a.reportView.SetSize(msg.Width, msg.Height-1)
		a.reviewView.SetSize(msg.Width, msg.Height-1)

		// Propagate to status bar
		a.statusBar, _ = a.statusBar.Update(msg)

	case board.OpenDetailMsg:
		a = a.setMode(viewDetail)
		// Load card synchronously — GetCard hits local SQLite, not remote API
		card, err := a.svc.GetCard(context.Background(), msg.CardKey)
		if err != nil {
			a.detail = detail.NewLoading(msg.CardKey, a.svc, a.clip, a.editor)
		} else {
			a.detail = detail.New(card, a.svc, a.clip, a.editor)
		}
		// Populate swarm sub-tasks if applicable.
		if a.swarmSvc != nil {
			if subs, err := a.swarmSvc.ListSubtaskInfos(context.Background(), msg.CardKey); err == nil && len(subs) > 0 {
				a.detail.SetSubtasks(subs)
			}
		}
		// Send window size
		detailModel, cmd := a.detail.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height - 1})
		a.detail = detailModel.(detail.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case detail.BackToBoard:
		a = a.setMode(viewBoard)
		return a, nil

	case agents.ReturnToBoardMsg:
		a = a.setMode(viewBoard)
		a.agentView.StopPolling()
		return a, nil

	case report.ReturnToBoardMsg, review.ReturnToBoardMsg:
		a = a.setMode(viewBoard)
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

	case review.QueueLoadedMsg, review.TourLoadedMsg, review.DiffLoadedMsg, review.ActionDoneMsg:
		var cmd tea.Cmd
		a.reviewView, cmd = a.reviewView.Update(msg)
		return a, cmd

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

	case agents.StateTimelinesRefreshedMsg:
		a.agentView, _ = a.agentView.Update(msg)
		return a, nil

	case agents.ToggleNotifyMsg:
		if a.agentSvc != nil {
			svc := a.agentSvc
			taskID := msg.TaskID
			return a, func() tea.Msg {
				cur, _ := svc.GetTaskNotifyEnabled(context.Background(), taskID)
				next := !cur
				if err := svc.SetTaskNotifyEnabled(context.Background(), taskID, next); err != nil {
					return agents.NotifyToggledMsg{TaskID: taskID, Enabled: cur, Err: err.Error()}
				}
				return agents.NotifyToggledMsg{TaskID: taskID, Enabled: next}
			}
		}
		return a, nil

	case agents.NotifyToggledMsg:
		if msg.Err != "" {
			a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: "notify error: " + msg.Err})
		} else {
			state := "off"
			if msg.Enabled {
				state = "on"
			}
			a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: "notify " + state + " for " + msg.TaskID})
		}
		a.agentView, _ = a.agentView.Update(msg)
		return a, nil
	case agentTickMsg:
		if a.active != viewAgents || a.agentSvc == nil {
			return a, nil
		}
		return a.handleAgentTick()

	case sparklineTickMsg:
		if a.active != viewAgents || a.agentSvc == nil {
			return a, nil
		}
		return a.handleSparklineTick()
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

	case overlay.SwarmStartMsg:
		return a.handleSwarmStart(msg)

	case overlay.SwarmInitCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.PlanApproveMsg:
		return a.handlePlanApprove(msg)

	case overlay.PlanRejectMsg:
		return a.handlePlanReject(msg)

	case overlay.PlanCancelMsg:
		return a.handlePlanCancel(msg)

	case overlay.PlanEditedMsg:
		// Reload the plan from disk via the service layer, then dispatch
		// PlanReloadedMsg back to the overlay so it can re-render. This keeps
		// the engine import out of the TUI overlay package.
		if a.overlayType != overlayPlanApproval {
			return a, nil
		}
		var (
			plan *service.SwarmPlan
			err  = msg.Err
		)
		if err == nil && a.swarmSvc != nil {
			plan, err = a.swarmSvc.LoadPlan(msg.PlanPath)
		}
		reload := overlay.PlanReloadedMsg{ParentTaskID: msg.ParentTaskID, PlanPath: msg.PlanPath, Plan: plan, Err: err}
		next, cmd := a.activeOverlay.Update(reload)
		a.activeOverlay = next
		return a, cmd

	case agents.OpenAgentSpawnMsg:
		return a.openAgentSpawnOverlay(msg.TaskID, msg.Title)

	case agents.OpenAgentActionMsg:
		return a.openAgentActionOverlay(msg.TaskID, msg.ParentTaskID, msg.Role)

	case agents.OpenMacroPickerMsg:
		return a.openMacroPickerOverlay()

	case agents.OpenGroupMsg:
		return a.openGroupOverlay(msg.TaskID, msg.Group)

	case agents.VoiceToggleMsg:
		return a.handleVoiceToggle(msg)

	case agents.VoiceRecordingMsg:
		a.voiceRecording = msg.Recording
		a.agentView, _ = a.agentView.Update(msg)
		if msg.Recording {
			return a, voiceLevelTickCmd()
		}
		return a, nil

	case agents.VoiceLevelMsg:
		a.agentView, _ = a.agentView.Update(msg)
		if a.voiceRecording {
			return a, voiceLevelTickCmd()
		}
		return a, nil

	case agents.VoiceTranscribingMsg:
		a.agentView, _ = a.agentView.Update(msg)
		return a, nil

	case agents.VoiceTranscriptionMsg:
		return a.handleVoiceTranscription(msg)

	case voiceLevelTickMsg:
		if !a.voiceRecording || a.voiceSvc == nil {
			return a, nil
		}
		levels := a.voiceSvc.Levels()
		a.agentView, _ = a.agentView.Update(agents.VoiceLevelMsg{Levels: levels})
		return a, voiceLevelTickCmd()

	case overlay.MacroSelectedMsg:
		return a.handleMacroSelected(msg)

	case overlay.MacroCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.AgentMessageSentMsg:
		return a.handleAgentMessageSent(msg)

	case overlay.AgentCloseConfirmedMsg:
		return a.handleAgentCloseConfirmed(msg)

	case overlay.SwarmFinishConfirmedMsg:
		return a.handleSwarmFinishConfirmed(msg)

	case overlay.SwarmCancelConfirmedMsg:
		return a.handleSwarmCancelConfirmed(msg)

	case overlay.SwarmCancelCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.AgentActionCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.AgentSpawnSubmitMsg:
		return a.handleAgentSpawnSubmit(msg)

	case overlay.GroupSelectedMsg:
		a.overlayType, a.activeOverlay = overlayNone, nil
		if a.agentSvc == nil {
			return a, nil
		}
		svc := a.agentSvc
		return a, func() tea.Msg {
			if err := svc.SetTaskGroup(context.Background(), msg.TaskID, msg.Group); err != nil {
				return statusbar.ErrorMsg{Text: "group failed: " + err.Error()}
			}
			agentList, err := svc.ListAgents(context.Background())
			if err != nil {
				return statusbar.ErrorMsg{Text: "refresh agents: " + err.Error()}
			}
			return agents.AgentsRefreshedMsg{Agents: agentList, SelectTask: msg.TaskID}
		}

	case overlay.GroupCancelledMsg:
		a.overlayType, a.activeOverlay = overlayNone, nil
		return a, nil
	case overlay.WorktreeSubmitMsg:
		a.overlayType, a.activeOverlay = overlayNone, nil
		svc := a.worktreeSvc
		taskID := msg.TaskID
		a.statusBar, _ = a.statusBar.Update(statusbar.ProgressMsg{Text: "creating worktree for " + taskID + "..."})
		return a, func() tea.Msg {
			if _, err := svc.Create(context.Background(), taskID, msg.PrimaryDir, msg.Branch, msg.BaseBranch); err != nil {
				return worktreeCreateDoneMsg{TaskID: taskID, Err: err.Error()}
			}
			return worktreeCreateDoneMsg{TaskID: taskID}
		}

	case overlay.ChimeraSessionChoiceMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		policy := "new"
		if msg.Resume {
			policy = "resume"
		}
		return a.spawnTaskAgent(msg.Spawn, msg.SessionID, policy)

	case overlay.ChimeraSessionCancelledMsg:
		a.overlayType = overlayNone
		a.activeOverlay = nil
		return a, nil

	case overlay.AgentSpawnCancelledMsg, overlay.WorktreeCancelledMsg:
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

	case detail.SwarmNextStepMsg:
		// Detail view emitted a "next step" request — call the service.
		if a.swarmSvc != nil {
			err := a.swarmSvc.NextStep(context.Background(), msg.TaskID)
			if err != nil {
				cmds = append(cmds, func() tea.Msg {
					return statusbar.ErrorMsg{Text: err.Error()}
				})
			} else {
				cmds = append(cmds, func() tea.Msg {
					return statusbar.InfoMsg{Text: fmt.Sprintf("Advanced swarm for %s", msg.TaskID)}
				})
			}
		}
		return a, tea.Batch(cmds...)

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

	case swarmUpdateMsg:
		// Swarm state changed — reload the board so badges refresh live and
		// re-fetch the agent list so newly-dispatched workers show up in
		// the agents view in real time.
		cmds = append(cmds, a.board.Init())
		if a.agentSvc != nil {
			svc := a.agentSvc
			cmds = append(cmds, func() tea.Msg {
				agentList, _ := svc.ListAgents(context.Background())
				return agents.AgentsRefreshedMsg{Agents: agentList}
			})
		}
		// If the plan approval overlay is open for this parent and the plan
		// was resolved elsewhere (web, another TUI), dismiss the overlay.
		if a.overlayType == overlayPlanApproval && a.activeOverlay != nil {
			if po, ok := a.activeOverlay.(overlay.PlanApprovalOverlay); ok {
				if po.ParentTaskID() == msg.Payload.ParentTaskID {
					if msg.Payload.NewStatus == "plan_applied" || msg.Payload.NewStatus == "rejected" {
						a.overlayType = overlayNone
						a.activeOverlay = nil
						cmds = append(cmds, func() tea.Msg {
							return statusbar.InfoMsg{Text: "Plan resolved on another surface"}
						})
					}
				}
			}
		}
		if a.swarmSub != nil {
			cmds = append(cmds, a.listenSwarmUpdates())
		}
		return a, tea.Batch(cmds...)

	case reviewUpdateMsg:
		if a.active == viewReview {
			var cmd tea.Cmd
			a.reviewView, cmd = a.reviewView.Update(review.ReviewChangedMsg{TourID: msg.Payload.TourID})
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := a.loadReviewBadges(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.reviewSub != nil {
			cmds = append(cmds, a.listenReviewUpdates())
		}
		return a, tea.Batch(cmds...)

	case reviewBadgesMsg:
		a.board.SetReviewStates(msg.States)
		return a, nil

	case planProposalMsg:
		// Conductor proposed a plan; surface the approval overlay.
		next, cmd := a.openPlanApprovalOverlayWithMode(msg.ParentTaskID, msg.PlanPath, msg.ReplySocket, msg.Mode)
		if a.planSub != nil {
			return next, tea.Batch(cmd, a.listenPlanProposals())
		}
		return next, cmd

	case boardRefreshMsg:
		cmd := a.board.Init()
		return a, cmd

	case worktreeCreateDoneMsg:
		a.statusBar, _ = a.statusBar.Update(statusbar.ProgressMsg{})
		if msg.Err != "" {
			a.statusBar, _ = a.statusBar.Update(statusbar.ErrorMsg{Text: "worktree failed: " + msg.Err})
			return a, nil
		}
		a.statusBar, _ = a.statusBar.Update(statusbar.InfoMsg{Text: "worktree created"})
		return a, a.board.Init()

	case importRemoteDoneMsg:
		if msg.Err != "" {
			a.pendingImport = ""
			a.statusBar, _ = a.statusBar.Update(statusbar.ProgressMsg{})
			a.statusBar, _ = a.statusBar.Update(statusbar.ErrorMsg{Text: "import failed: " + msg.Err})
			return a, nil
		}
		a.pendingNav = msg.TicketID
		return a, a.board.Init()

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
		// Populate swarm stats per board card.
		if a.swarmSvc != nil {
			if taskIDs := a.board.TaskIDs(); len(taskIDs) > 0 {
				stats := make(map[string]board.SwarmStats)
				for _, id := range taskIDs {
					subs, err := a.swarmSvc.ListSubtaskInfos(context.Background(), id)
					if err != nil || len(subs) == 0 {
						continue
					}
					var s board.SwarmStats
					s.Total = len(subs)
					for _, sub := range subs {
						switch sub.Status {
						case "done":
							s.Done++
						case "reporting":
							s.InReview++
						case "in_progress":
							s.Building++
						case "queued":
							s.Queued++
						case "cancelled":
							s.Rejected++
						}
					}
					stats[id] = s
				}
				if len(stats) > 0 {
					a.board.SetSwarmStats(stats)
				}
			}
		}
		if cmd := a.loadReviewBadges(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Apply pending navigation (e.g. after task creation or remote import).
		if a.pendingNav != "" {
			if a.pendingNav != a.pendingImport || a.boardHasTask(a.pendingNav) {
				a.board.NavigateTo(a.pendingNav)
				if a.pendingNav == a.pendingImport {
					a.pendingImport = ""
					a.statusBar, _ = a.statusBar.Update(statusbar.ProgressMsg{})
				}
				a.pendingNav = ""
			}
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

// setMode switches the active view and notifies the status bar so its key
// hints match the current context.
func (a App) setMode(v viewType) App {
	a.active = v
	a.statusBar, _ = a.statusBar.Update(statusbar.ModeMsg{Mode: viewToStatusMode(v)})
	return a
}

func (a App) boardHasTask(taskID string) bool {
	for _, visibleID := range a.board.TaskIDs() {
		if visibleID == taskID {
			return true
		}
	}
	return false
}

func viewToStatusMode(v viewType) statusbar.Mode {
	switch v {
	case viewDetail:
		return statusbar.ModeDetail
	case viewAgents:
		return statusbar.ModeAgents
	case viewReport:
		return statusbar.ModeReport
	case viewReview:
		return statusbar.ModeReview
	default:
		return statusbar.ModeBoard
	}
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
	a.activeOverlay = overlay.NewHelpWithMode(a.width, a.height, appViewToHelpMode(a.active)).WithWorktrees(a.worktreesEnabled)
	a.overlayType = overlayHelp
	return a, nil
}

func appViewToHelpMode(v viewType) overlay.HelpMode {
	switch v {
	case viewDetail:
		return overlay.HelpModeDetail
	case viewAgents:
		return overlay.HelpModeAgents
	case viewReport:
		return overlay.HelpModeReport
	case viewReview:
		return overlay.HelpModeReview
	default:
		return overlay.HelpModeBoard
	}
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
		a = a.setMode(viewBoard)
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
	a.pendingImport = ticketID
	a.statusBar, _ = a.statusBar.Update(statusbar.ProgressMsg{Text: "importing " + ticketID + "..."})
	return a, func() tea.Msg {
		_, err := svc.ImportRemoteTask(context.Background(), ticketID, workspaceID)
		if err != nil {
			return importRemoteDoneMsg{TicketID: ticketID, Err: err.Error()}
		}
		return importRemoteDoneMsg{TicketID: ticketID}
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

func (a App) openSwarmInitOverlay() (tea.Model, tea.Cmd) {
	selected := a.board.SelectedCard()
	if selected == nil {
		return a, nil
	}
	suggested, _ := os.Getwd()
	swModel := overlay.NewSwarmInit(selected.Key, selected.Title, suggested)
	sized, _ := swModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlaySwarmInit
	return a, nil
}

func (a App) openSwarmCancelOverlay() (tea.Model, tea.Cmd) {
	selected := a.board.SelectedCard()
	if selected == nil {
		return a, nil
	}
	cancelModel := overlay.NewSwarmCancel(selected.Key, selected.Title)
	sized, _ := cancelModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlaySwarmCancel
	return a, nil
}

func (a App) handleSwarmCancelConfirmed(msg overlay.SwarmCancelConfirmedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.swarmSvc == nil {
		return a, nil
	}
	if err := a.swarmSvc.CancelSwarm(context.Background(), msg.ParentTaskID); err != nil {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: fmt.Sprintf("Cancel swarm failed: %v", err)}
		}
	}
	return a, a.board.Init()
}

func (a App) openPlanApprovalOverlay(parentTaskID, planPath, replySocket string) (tea.Model, tea.Cmd) {
	return a.openPlanApprovalOverlayWithMode(parentTaskID, planPath, replySocket, "")
}

func (a App) openPlanApprovalOverlayWithMode(parentTaskID, planPath, replySocket, mode string) (tea.Model, tea.Cmd) {
	var (
		plan    *service.SwarmPlan
		loadErr error
	)
	if a.swarmSvc != nil {
		plan, loadErr = a.swarmSvc.LoadPlan(planPath)
	} else {
		loadErr = fmt.Errorf("swarm service unavailable")
	}
	isExtension := mode == "extension"
	po := overlay.NewPlanApproval(parentTaskID, planPath, replySocket, a.editor, plan, loadErr, overlay.WithExtension(isExtension))
	sized, _ := po.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayPlanApproval
	return a, nil
}

func (a App) handleSwarmStart(msg overlay.SwarmStartMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.swarmSvc == nil {
		return a, nil
	}
	if err := a.swarmSvc.StartSwarm(context.Background(), msg.ParentTaskID, msg.WorkingDir); err != nil {
		errText := fmt.Sprintf("Start swarm failed: %v", err)
		if strings.Contains(err.Error(), "cancel the existing swarm first") {
			errText = errText + " (press Shift+C to cancel the existing swarm)"
		}
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: errText}
		}
	}
	return a, a.board.Init()
}

func (a App) handlePlanApprove(msg overlay.PlanApproveMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if msg.ReplySocket != "" {
		_ = ipc.Send(msg.ReplySocket, ipc.Message{
			Type:     "plan_verdict",
			TaskID:   msg.ParentTaskID,
			Status:   "approved",
			PlanPath: msg.PlanPath,
		})
	}
	// Notify the rest of the system (web clients, other TUIs) so the modal
	// disappears everywhere.
	if a.eventBus != nil && a.swarmSvc != nil {
		_ = a.swarmSvc.DeletePendingPlan(context.Background(), msg.ParentTaskID)
		a.eventBus.Publish(events.Event{
			Type: events.EventSwarmChanged,
			Payload: events.SwarmChangedPayload{
				ParentTaskID: msg.ParentTaskID,
				NewStatus:    "plan_applied",
			},
			At: time.Now(),
		})
	}
	return a, a.board.Init()
}

func (a App) handlePlanReject(msg overlay.PlanRejectMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if msg.ReplySocket != "" {
		_ = ipc.Send(msg.ReplySocket, ipc.Message{
			Type:   "plan_verdict",
			TaskID: msg.ParentTaskID,
			Status: "rejected",
			Notes:  msg.Notes,
		})
	}
	// Notify the rest of the system (web clients, other TUIs) so the modal
	// disappears everywhere.
	if a.eventBus != nil && a.swarmSvc != nil {
		_ = a.swarmSvc.DeletePendingPlan(context.Background(), msg.ParentTaskID)
		a.eventBus.Publish(events.Event{
			Type: events.EventSwarmChanged,
			Payload: events.SwarmChangedPayload{
				ParentTaskID: msg.ParentTaskID,
				NewStatus:    "rejected",
			},
			At: time.Now(),
		})
	}
	return a, nil
}

func (a App) handlePlanCancel(msg overlay.PlanCancelMsg) (tea.Model, tea.Cmd) {
	// User dismissed without verdict — leave the conductor blocked. They can
	// restart the propose-plan call or the conductor's CLI will time out.
	a.overlayType = overlayNone
	a.activeOverlay = nil
	return a, func() tea.Msg {
		return statusbar.InfoMsg{Text: "Plan dismissed; the conductor is still waiting on a verdict."}
	}
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

func (a App) openAgentActionOverlay(taskID, parentTaskID, role string) (tea.Model, tea.Cmd) {
	if a.swarmSvc == nil {
		return a, nil
	}
	model := overlay.NewAgentAction(taskID, parentTaskID, role)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayAgentAction
	return a, nil
}

func (a App) handleAgentMessageSent(msg overlay.AgentMessageSentMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.swarmSvc == nil {
		return a, nil
	}
	// If role is conductor, message the parent; otherwise message the worker subtask.
	var err error
	if msg.Role == "conductor" {
		err = a.swarmSvc.MessageParent(context.Background(), msg.ParentTaskID, msg.Text, false)
	} else {
		err = a.swarmSvc.Message(context.Background(), msg.TaskID, msg.Text, false)
	}
	if err != nil {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: "message failed: " + err.Error()}
		}
	}
	return a, func() tea.Msg {
		return statusbar.InfoMsg{Text: "Message sent"}
	}
}

func (a App) handleAgentCloseConfirmed(msg overlay.AgentCloseConfirmedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.swarmSvc == nil {
		return a, nil
	}
	err := a.swarmSvc.Close(context.Background(), msg.TaskID)
	if err != nil {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: "close failed: " + err.Error()}
		}
	}
	return a, tea.Batch(
		func() tea.Msg {
			return statusbar.InfoMsg{Text: "Worker closed"}
		},
		a.board.Init(),
	)
}

func (a App) handleSwarmFinishConfirmed(msg overlay.SwarmFinishConfirmedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil
	if a.swarmSvc == nil {
		return a, nil
	}
	err := a.swarmSvc.Finish(context.Background(), msg.ParentTaskID, msg.Summary)
	if err != nil {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: "finish failed: " + err.Error()}
		}
	}
	return a, tea.Batch(
		func() tea.Msg {
			return statusbar.InfoMsg{Text: "Swarm finished"}
		},
		a.board.Init(),
	)
}

func (a App) openGroupOverlay(taskID, current string) (tea.Model, tea.Cmd) {
	seen := make(map[string]bool)
	options := make([]string, 0, len(a.groupDefaults))
	add := func(group string) {
		group = strings.TrimSpace(group)
		if group != "" && !seen[group] {
			seen[group] = true
			options = append(options, group)
		}
	}
	for _, group := range a.groupDefaults {
		add(group)
	}
	for _, agent := range a.agentView.Agents() {
		add(agent.Group)
	}
	add(current)
	model := overlay.NewGroup(taskID, current, options)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay, a.overlayType = sized, overlayGroup
	return a, nil
}

func (a App) openAgentSpawnOverlay(taskID, title string) (tea.Model, tea.Cmd) {
	defaultAdapter := ""
	var adapters, filtered []string
	if a.agentSvc != nil {
		defaultAdapter = a.agentSvc.DefaultAdapter()
		adapters = a.agentSvc.RegisteredAdapters()
		filtered = make([]string, 0, len(adapters))
		for _, name := range adapters {
			if name != defaultAdapter {
				filtered = append(filtered, name)
			}
		}
	}
	defaultDir := a.workDir
	if taskID != "" && a.worktreeSvc != nil {
		if path, err := a.worktreeSvc.Path(context.Background(), taskID); err == nil && path != "" {
			defaultDir = path
			if idx := strings.IndexAny(defaultDir, "\n\r"); idx >= 0 {
				defaultDir = strings.TrimSpace(defaultDir[:idx])
			}
		}
	}
	spawnModel := overlay.NewAgentSpawn(filtered, defaultAdapter, defaultDir, taskID, title)
	sized, _ := spawnModel.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayAgentSpawn
	return a, nil
}

func (a App) handleAgentSpawnSubmit(msg overlay.AgentSpawnSubmitMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil

	if a.agentSvc == nil {
		return a, nil
	}

	svc := a.agentSvc
	termW := a.width - agents.SidebarWidth
	if termW < 1 {
		termW = 1
	}
	termH := a.height - 1

	opts := service.AgentSpawnOptions{
		AgentKind:  msg.AgentKind,
		WorkingDir: msg.WorkingDir,
	}

	if msg.TaskID == "" {
		// Ephemeral agent: no task backing
		title := msg.Title
		return a, tea.Batch(
			func() tea.Msg {
				if err := svc.SpawnEphemeralAgent(context.Background(), title, termW, termH, opts); err != nil {
					return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
				}
				agentList, _ := svc.ListAgents(context.Background())
				return agents.AgentsRefreshedMsg{Agents: agentList}
			},
			agentTickCmd(),
		)
	}

	if msg.AgentKind == "chimera" || (msg.AgentKind == "" && svc.DefaultAdapter() == "chimera") {
		if sessionID, err := svc.GetTaskChimeraSessionID(context.Background(), msg.TaskID); err == nil && sessionID != "" {
			model := overlay.NewChimeraSession(msg, sessionID)
			sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
			a.activeOverlay = sized
			a.overlayType = overlayChimeraSession
			return a, nil
		}
	}
	return a.spawnTaskAgent(msg, "", "")
}

func (a App) spawnTaskAgent(msg overlay.AgentSpawnSubmitMsg, chimeraSessionID, chimeraPolicy string) (tea.Model, tea.Cmd) {
	svc := a.agentSvc
	termW := a.width - agents.SidebarWidth
	if termW < 1 {
		termW = 1
	}
	termH := a.height - 1
	opts := service.AgentSpawnOptions{AgentKind: msg.AgentKind, WorkingDir: msg.WorkingDir, ChimeraSessionID: chimeraSessionID, ChimeraSessionExists: chimeraPolicy}

	// Task-bound agent: switch to agent view and spawn with options
	a = a.setMode(viewAgents)
	a.agentView.StartPolling()
	return a, tea.Batch(
		func() tea.Msg {
			if err := svc.SpawnAgent(context.Background(), msg.TaskID, termW, termH, opts); err != nil {
				return statusbar.ErrorMsg{Text: "spawn failed: " + err.Error()}
			}
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList, SelectTask: msg.TaskID}
		},
		agentTickCmd(),
	)
}

func (a App) openMacroPickerOverlay() (tea.Model, tea.Cmd) {
	model := overlay.NewMacroPicker(a.macros)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay = sized
	a.overlayType = overlayMacroPicker
	return a, nil
}

func (a App) handleMacroSelected(msg overlay.MacroSelectedMsg) (tea.Model, tea.Cmd) {
	a.overlayType = overlayNone
	a.activeOverlay = nil

	if a.agentSvc == nil {
		return a, nil
	}
	sel := a.agentView.SelectedAgent()
	if sel == nil {
		return a, nil
	}

	session := sel.TmuxSession
	if session == "" {
		session = "legato-" + sel.TaskID
	}
	if a.tmux == nil {
		return a, nil
	}
	keys := msg.Macro.Keys
	tmuxer := a.tmux
	return a, func() tea.Msg {
		var err error
		if len(keys) > 0 && keys[len(keys)-1] == '\n' {
			err = tmuxer.SendKeysLine(session, keys[:len(keys)-1])
			if err == nil {
				err = tmuxer.SendKey(session, "Enter")
			}
		} else {
			err = tmuxer.SendKeysLine(session, keys)
		}
		if err != nil {
			return statusbar.ErrorMsg{Text: "macro send failed: " + err.Error()}
		}
		return statusbar.InfoMsg{Text: "Sent macro: " + msg.Macro.Name}
	}
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
	a = a.setMode(viewReport)
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
	srv := server.NewWithSwarm(a.svc, a.agentSvc, a.tmux, addr, a.swarmSvc, a.eventBus, a.workDir)
	a.webServer = srv
	a.statusBar = a.statusBar.SetWebServer(a.webServerPort)
	if a.eventBus != nil {
		srv.StartSwarmEvents()
	}

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
	a = a.setMode(viewAgents)
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
	cmds = append(cmds, agentTickCmd(), sparklineTickCmd())
	return a, tea.Batch(cmds...)
}

// sparklineTickMsg is the 5-second tick for fetching sparkline data.
type sparklineTickMsg struct{}

func sparklineTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return sparklineTickMsg{}
	})
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

func (a App) handleSparklineTick() (tea.Model, tea.Cmd) {
	if a.agentSvc == nil || a.active != viewAgents {
		return a, sparklineTickCmd()
	}
	agentList := a.agentView.Agents()
	if len(agentList) == 0 {
		return a, sparklineTickCmd()
	}
	window := a.sparklineWindow
	if window <= 0 {
		window = 10 * time.Minute
	}
	buckets := a.sparklineBuckets
	if buckets <= 0 {
		buckets = 10
	}
	svc := a.agentSvc
	cmds := make([]tea.Cmd, 0, len(agentList))
	for _, ag := range agentList {
		tid := ag.TaskID
		cmds = append(cmds, func() tea.Msg {
			tl, _ := svc.GetStateTimeline(context.Background(), tid, window, buckets)
			return agents.StateTimelinesRefreshedMsg{Timelines: map[string][]string{tid: tl}}
		})
	}
	cmds = append(cmds, sparklineTickCmd())
	return a, tea.Batch(cmds...)
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
	// Revert to tmux auto-sizing so the terminal gets its natural
	// dimensions when attached. The web server's resize heartbeat
	// will re-apply manual sizing if web clients are still connected.
	sessionName := "legato-" + taskID
	if a.tmux != nil {
		a.tmux.SetOption(sessionName, "window-size", "latest")
	}
	return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
		// After detach, refresh agent list
		agentList, _ := svc.ListAgents(context.Background())
		return agents.AgentsRefreshedMsg{Agents: agentList}
	})
}

// handleVoiceToggle starts or stops recording. When starting, it records the
// target session/kind for later delivery. When stopping, it kicks off
// transcription and then delivery in a goroutine.
func (a App) handleVoiceToggle(msg agents.VoiceToggleMsg) (tea.Model, tea.Cmd) {
	if a.voiceSvc == nil {
		return a, nil
	}

	if !a.voiceRecording {
		// Start recording
		if err := a.voiceSvc.StartRecording(a.voiceMicDevice); err != nil {
			return a, func() tea.Msg {
				return statusbar.ErrorMsg{Text: "voice: " + err.Error()}
			}
		}
		a.voiceRecording = true
		a.voiceTargetSession = msg.TmuxSession
		a.voiceTargetKind = msg.AgentKind
		if a.voiceTargetSession == "" {
			a.voiceTargetSession = "legato-" + msg.TaskID
		}
		return a, func() tea.Msg {
			return agents.VoiceRecordingMsg{Recording: true}
		}
	}

	// Stop recording → transcribe → deliver.
	// Update model state directly so the UI transitions immediately.
	a.voiceRecording = false
	a.agentView, _ = a.agentView.Update(agents.VoiceRecordingMsg{Recording: false})
	a.agentView, _ = a.agentView.Update(agents.VoiceTranscribingMsg{})

	svc := a.voiceSvc
	session := a.voiceTargetSession
	agentKind := a.voiceTargetKind
	autoSend := a.voiceAutoSend

	return a, func() tea.Msg {
		text, err := svc.Transcribe(context.Background())
		if err != nil {
			return agents.VoiceTranscriptionMsg{Err: err.Error()}
		}
		if err := svc.Deliver(context.Background(), session, agentKind, text, autoSend); err != nil {
			return agents.VoiceTranscriptionMsg{Err: err.Error()}
		}
		return agents.VoiceTranscriptionMsg{Text: text}
	}
}

// handleVoiceTranscription delivers the transcribed text and shows a
// status bar message.
func (a App) handleVoiceTranscription(msg agents.VoiceTranscriptionMsg) (tea.Model, tea.Cmd) {
	a.agentView, _ = a.agentView.Update(msg)
	if msg.Err != "" {
		return a, func() tea.Msg {
			return statusbar.ErrorMsg{Text: "voice: " + msg.Err}
		}
	}
	return a, func() tea.Msg {
		return statusbar.InfoMsg{Text: "Sent voice: " + msg.Text}
	}
}

// voiceLevelTickMsg is the fast tick for polling audio levels during recording.
type voiceLevelTickMsg struct{}

func voiceLevelTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return voiceLevelTickMsg{}
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
		a = a.setMode(viewAgents)
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

	// Open spawn overlay so user can pick agent + CWD
	return a.openAgentSpawnOverlay(taskID, card.Title)
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
		// 'W' toggles web server
		if msg.String() == "W" {
			return a.toggleWebServer()
		}
		if msg.String() == "b" && a.worktreesEnabled {
			card := a.board.SelectedCard()
			if card == nil {
				return a, nil
			}
			branch := strings.ToLower(strings.ReplaceAll(card.Key, "_", "-"))
			model := overlay.NewWorktree(card.Key, a.workDir, branch)
			sized, _ := model.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
			a.activeOverlay, a.overlayType = sized, overlayWorktree
			return a, nil
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
	case viewReview:
		var cmd tea.Cmd
		a.reviewView, cmd = a.reviewView.Update(msg)
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
	case viewReview:
		statusBar := a.statusBar.View()
		statusBarHeight := lipgloss.Height(statusBar)
		a.reviewView.SetSize(a.width, a.height-statusBarHeight)
		content := a.reviewView.View()
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
