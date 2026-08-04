package hotkey

import tea "github.com/charmbracelet/bubbletea"

const (
	ActionHelp ActionID = "global.help"
	ActionQuit ActionID = "global.quit"
	ActionBack ActionID = "global.back"

	ActionBoardLeft          ActionID = "board.left"
	ActionBoardRight         ActionID = "board.right"
	ActionBoardDown          ActionID = "board.down"
	ActionBoardUp            ActionID = "board.up"
	ActionBoardFirst         ActionID = "board.first"
	ActionBoardLast          ActionID = "board.last"
	ActionBoardColumn1       ActionID = "board.column.1"
	ActionBoardColumn2       ActionID = "board.column.2"
	ActionBoardColumn3       ActionID = "board.column.3"
	ActionBoardColumn4       ActionID = "board.column.4"
	ActionBoardColumn5       ActionID = "board.column.5"
	ActionBoardOpen          ActionID = "board.open"
	ActionBoardContext       ActionID = "board.context"
	ActionBoardMove          ActionID = "board.move"
	ActionBoardMoveWorkspace ActionID = "board.move-workspace"
	ActionBoardDelete        ActionID = "board.delete"
	ActionBoardImport        ActionID = "board.import"
	ActionBoardLinkPR        ActionID = "board.link-pr"
	ActionBoardRefreshPR     ActionID = "board.refresh-pr"
	ActionBoardRediscoverPR  ActionID = "board.rediscover-pr"
	ActionBoardWorkspace     ActionID = "board.workspace"
	ActionBoardSearch        ActionID = "board.search"
	ActionBoardRefresh       ActionID = "board.refresh"
	ActionBoardCreate        ActionID = "board.create"
	ActionBoardStartSwarm    ActionID = "board.start-swarm"
	ActionBoardCancelSwarm   ActionID = "board.cancel-swarm"
	ActionBoardArchive       ActionID = "board.archive"
	ActionBoardArchived      ActionID = "board.archived"
	ActionBoardWorktree      ActionID = "board.worktree"
	ActionBoardSpawnAgent    ActionID = "board.spawn-agent"
	ActionBoardTerminal      ActionID = "board.terminal"
	ActionBoardToggleWeb     ActionID = "board.toggle-web"
	ActionViewAgents         ActionID = "view.agents"
	ActionViewReport         ActionID = "view.reporting"
	ActionViewReview         ActionID = "view.reviews"
	ActionViewPlans          ActionID = "view.plans"
	ActionViewAttention      ActionID = "view.attention"

	ActionDetailDown            ActionID = "detail.down"
	ActionDetailUp              ActionID = "detail.up"
	ActionDetailHalfDown        ActionID = "detail.half-down"
	ActionDetailHalfUp          ActionID = "detail.half-up"
	ActionDetailTop             ActionID = "detail.top"
	ActionDetailBottom          ActionID = "detail.bottom"
	ActionDetailCopyDescription ActionID = "detail.copy-description"
	ActionDetailCopyFull        ActionID = "detail.copy-full"
	ActionDetailOpenURL         ActionID = "detail.open-url"
	ActionDetailMove            ActionID = "detail.move"
	ActionDetailMoveWorkspace   ActionID = "detail.move-workspace"
	ActionDetailDelete          ActionID = "detail.delete"
	ActionDetailEditDescription ActionID = "detail.edit-description"
	ActionDetailEditTitle       ActionID = "detail.edit-title"
	ActionDetailNextSwarmStep   ActionID = "detail.next-swarm-step"

	ActionAgentsDown    ActionID = "agents.down"
	ActionAgentsUp      ActionID = "agents.up"
	ActionAgentsSpawn   ActionID = "agents.spawn"
	ActionAgentsKill    ActionID = "agents.kill"
	ActionAgentsAttach  ActionID = "agents.attach"
	ActionAgentsMacro   ActionID = "agents.macro"
	ActionAgentsGroup   ActionID = "agents.group"
	ActionAgentsActions ActionID = "agents.actions"
	ActionAgentsDetails ActionID = "agents.details"
	ActionAgentsNotify  ActionID = "agents.notify"
	ActionAgentsVoice   ActionID = "agents.voice"

	ActionReportPeriod   ActionID = "report.period"
	ActionReportPrevious ActionID = "report.previous"
	ActionReportNext     ActionID = "report.next"
	ActionReportDown     ActionID = "report.down"
	ActionReportUp       ActionID = "report.up"
	ActionReportCopy     ActionID = "report.copy"

	ActionReviewDown     ActionID = "review.down"
	ActionReviewUp       ActionID = "review.up"
	ActionReviewOpen     ActionID = "review.open"
	ActionReviewRefresh  ActionID = "review.refresh"
	ActionReviewHalfDown ActionID = "review.half-down"
	ActionReviewHalfUp   ActionID = "review.half-up"
	ActionReviewTop      ActionID = "review.top"
	ActionReviewBottom   ActionID = "review.bottom"
	ActionReviewToggle   ActionID = "review.toggle"
	ActionReviewAsk      ActionID = "review.ask"
	ActionReviewComplete ActionID = "review.complete"
	ActionReviewDelete   ActionID = "review.delete"
	ActionConfirm        ActionID = "mode.confirm"
	ActionCancel         ActionID = "mode.cancel"

	ActionPlanDown         ActionID = "plans.down"
	ActionPlanUp           ActionID = "plans.up"
	ActionPlanOpen         ActionID = "plans.open"
	ActionPlanNextQuestion ActionID = "plans.next-question"
	ActionPlanAnswer       ActionID = "plans.answer"
	ActionPlanComment      ActionID = "plans.comment"
	ActionPlanQuestion     ActionID = "plans.question"
	ActionPlanApprove      ActionID = "plans.approve"
	ActionPlanReject       ActionID = "plans.reject"
	ActionPlanChanges      ActionID = "plans.changes"
	ActionPlanReopen       ActionID = "plans.reopen"

	ActionAttentionDown    ActionID = "attention.down"
	ActionAttentionUp      ActionID = "attention.up"
	ActionAttentionExecute ActionID = "attention.execute"
	ActionAttentionDismiss ActionID = "attention.dismiss"
)

const (
	OverlayMove          Overlay = "move"
	OverlayWorkspace     Overlay = "workspace"
	OverlayMoveWorkspace Overlay = "move-workspace"
)

const (
	ActionOverlayDown   ActionID = "overlay.down"
	ActionOverlayUp     ActionID = "overlay.up"
	ActionOverlaySelect ActionID = "overlay.select"
	ActionOverlayCancel ActionID = "overlay.cancel"
	ActionMoveWorkspace ActionID = "overlay.move.workspace"
)

const (
	ModeQueue         Mode = "queue"
	ModeReader        Mode = "reader"
	ModeTour          Mode = "tour"
	ModeDeleteConfirm Mode = "delete-confirm"
	ModeQuestionInput Mode = "question-input"
	ModeTextInput     Mode = "text-input"
	ModeApproval      Mode = "approval"
)

const (
	PredicateLocalTask Predicate = "local-task"
	PredicateEditor    Predicate = "editor"
	PredicateSwarmNext Predicate = "swarm-next-step"
)

var defaultRegistry = MustRegistry(DefaultBindings())

func DefaultRegistry() *Registry { return defaultRegistry }

func DefaultBindings() []Binding {
	var bindings []Binding
	add := func(id ActionID, scope Scope, target Target, keys []Key, label, help, section string, sectionOrder, order int, surfaces Surface, requires ...Capability) {
		bindings = append(bindings, Binding{ID: id, Scope: scope, Target: target, Keys: keys, Label: label, Help: help, Section: section, SectionOrder: sectionOrder, Order: order, Surfaces: surfaces, Requires: requires})
	}
	global := GlobalScope()
	add(ActionHelp, global, TargetRoot, []Key{Rune('?')}, "keys", "Toggle keyboard reference", "General", 90, 10, FullHelp|ShortcutBar)
	bindings[len(bindings)-1].Precedence = YieldToSpecific
	add(ActionQuit, global, TargetRoot, []Key{Rune('q'), Special(tea.KeyCtrlC, "ctrl+c")}, "quit", "Quit Legato", "General", 90, 20, FullHelp)
	bindings[len(bindings)-1].Precedence = YieldToSpecific

	board := PageScope(PageBoard)
	add(ActionBoardLeft, board, TargetPage, []Key{Rune('h')}, "left", "Move to previous column", "Navigation", 10, 10, FullHelp)
	add(ActionBoardRight, board, TargetPage, []Key{Rune('l')}, "right", "Move to next column", "Navigation", 10, 20, FullHelp)
	add(ActionBoardDown, board, TargetPage, []Key{Rune('j')}, "down", "Move down within column", "Navigation", 10, 30, FullHelp)
	add(ActionBoardUp, board, TargetPage, []Key{Rune('k')}, "up", "Move up within column", "Navigation", 10, 40, FullHelp)
	add(ActionBoardFirst, board, TargetPage, []Key{Rune('g')}, "first", "Jump to first card", "Navigation", 10, 50, FullHelp)
	add(ActionBoardLast, board, TargetPage, []Key{Rune('G')}, "last", "Jump to last card", "Navigation", 10, 60, FullHelp)
	for i, id := range []ActionID{ActionBoardColumn1, ActionBoardColumn2, ActionBoardColumn3, ActionBoardColumn4, ActionBoardColumn5} {
		key := rune('1' + i)
		add(id, board, TargetPage, []Key{Rune(key)}, "column", "Jump to column", "Navigation", 10, 70+i, FullHelp)
	}
	add(ActionBoardOpen, board, TargetPage, []Key{Special(tea.KeyEnter, "enter")}, "detail", "Open task detail", "Actions", 20, 10, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardContext, board, TargetPage, []Key{Rune('c')}, "context", "Open task context menu", "Actions", 20, 15, FullHelp)
	add(ActionBoardMove, board, TargetPage, []Key{Rune('m')}, "move", "Move task", "Actions", 20, 20, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardMoveWorkspace, board, TargetPage, []Key{Rune('v')}, "move workspace", "Move task to workspace", "Actions", 20, 25, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardCreate, board, TargetRoot, []Key{Rune('n')}, "new", "Create new task", "Actions", 20, 30, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardDelete, board, TargetPage, []Key{Rune('d')}, "delete", "Delete task", "Actions", 20, 40, FullHelp)
	add(ActionBoardImport, board, TargetPage, []Key{Rune('i')}, "import", "Import remote ticket", "Actions", 20, 50, FullHelp, CapabilitySync)
	add(ActionBoardLinkPR, board, TargetPage, []Key{Rune('p')}, "link PR", "Link PR to task", "Actions", 20, 60, FullHelp, CapabilityPR)
	add(ActionBoardRefreshPR, board, TargetPage, []Key{Special(tea.KeyCtrlR, "ctrl+r")}, "refresh PR", "Refresh linked PR", "Actions", 20, 70, FullHelp, CapabilityPR)
	add(ActionBoardRediscoverPR, board, TargetPage, []Key{Special(tea.KeyCtrlP, "ctrl+p")}, "rediscover PR", "Rediscover task PR", "Actions", 20, 80, FullHelp, CapabilityPR)
	add(ActionBoardWorkspace, board, TargetPage, []Key{Rune('w')}, "workspace", "Switch workspace", "Actions", 20, 90, FullHelp)
	add(ActionBoardSearch, board, TargetRoot, []Key{Rune('/')}, "search", "Search tasks", "Actions", 20, 100, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardRefresh, board, TargetRoot, []Key{Rune('r')}, "sync", "Force sync", "Actions", 20, 110, FullHelp|Shortlist|ShortcutBar)
	add(ActionBoardArchive, board, TargetRoot, []Key{Rune('X')}, "archive", "Archive done cards", "Actions", 20, 120, FullHelp)
	add(ActionBoardArchived, board, TargetRoot, []Key{Rune('B')}, "archived", "Browse archived tasks", "Actions", 20, 130, FullHelp)
	add(ActionBoardWorktree, board, TargetRoot, []Key{Rune('b')}, "worktree", "Create task worktree", "Actions", 20, 140, FullHelp, CapabilityWorktrees)
	add(ActionBoardSpawnAgent, board, TargetRoot, []Key{Rune('a')}, "agent", "Spawn agent for task", "Actions", 20, 150, FullHelp)
	add(ActionBoardTerminal, board, TargetRoot, []Key{Rune('t')}, "terminal", "Open task agent terminal", "Actions", 20, 160, FullHelp)
	add(ActionBoardStartSwarm, board, TargetRoot, []Key{Rune('S')}, "start swarm", "Start a swarm", "Swarm", 30, 10, FullHelp, CapabilitySwarm)
	add(ActionBoardCancelSwarm, board, TargetRoot, []Key{Rune('C')}, "cancel swarm", "Cancel active swarm", "Swarm", 30, 20, FullHelp, CapabilitySwarm)
	add(ActionBoardToggleWeb, board, TargetRoot, []Key{Rune('W')}, "web", "Toggle web server", "Actions", 20, 170, FullHelp)
	add(ActionViewAgents, board, TargetRoot, []Key{Rune('A')}, "agents", "Open agents view", "Views", 40, 10, FullHelp)
	add(ActionViewReport, board, TargetRoot, []Key{Rune('R')}, "reports", "Open reporting view", "Views", 40, 20, FullHelp)
	add(ActionViewReview, board, TargetRoot, []Key{Rune('V')}, "reviews", "Open review tours", "Views", 40, 30, FullHelp, CapabilityReview)
	add(ActionViewPlans, board, TargetRoot, []Key{Rune('P')}, "plans", "Open implementation plans", "Views", 40, 40, FullHelp, CapabilityPlans)
	add(ActionViewAttention, board, TargetRoot, []Key{Rune('I')}, "attention", "Open attention inbox", "Views", 40, 50, FullHelp, CapabilityAttention)

	addPageBindings(&bindings, add)
	addOverlayBindings(add)
	return bindings
}

func addOverlayBindings(add func(ActionID, Scope, Target, []Key, string, string, string, int, int, Surface, ...Capability)) {
	for _, overlay := range []Overlay{OverlayMove, OverlayWorkspace, OverlayMoveWorkspace} {
		scope := OverlayScope(overlay)
		add(ActionOverlayDown, scope, TargetOverlay, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "select", "Select next item", "Overlay", 100, 10, 0)
		add(ActionOverlayUp, scope, TargetOverlay, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "select", "Select previous item", "Overlay", 100, 11, 0)
		add(ActionOverlaySelect, scope, TargetOverlay, []Key{Special(tea.KeyEnter, "enter")}, "select", "Choose selected item", "Overlay", 100, 20, 0)
		add(ActionOverlayCancel, scope, TargetOverlay, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "cancel", "Close overlay", "Overlay", 100, 30, 0)
	}
	add(ActionMoveWorkspace, OverlayScope(OverlayMove), TargetOverlay, []Key{Rune('w')}, "workspace", "Assign workspace", "Overlay", 100, 25, 0)
}

func addPageBindings(bindings *[]Binding, add func(ActionID, Scope, Target, []Key, string, string, string, int, int, Surface, ...Capability)) {
	detail := PageScope(PageDetail)
	add(ActionBack, detail, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to board", "Detail", 20, 1, FullHelp|Shortlist|ShortcutBar|InlineHint)
	add(ActionDetailDown, detail, TargetPage, []Key{Rune('j')}, "scroll", "Scroll down", "Detail", 20, 10, FullHelp)
	add(ActionDetailUp, detail, TargetPage, []Key{Rune('k')}, "scroll", "Scroll up", "Detail", 20, 11, FullHelp)
	add(ActionDetailHalfDown, detail, TargetPage, []Key{Rune('d')}, "page down", "Scroll half page down", "Detail", 20, 12, FullHelp)
	add(ActionDetailHalfUp, detail, TargetPage, []Key{Rune('u')}, "page up", "Scroll half page up", "Detail", 20, 13, FullHelp)
	add(ActionDetailTop, detail, TargetPage, []Key{Rune('g')}, "top", "Jump to top", "Detail", 20, 14, FullHelp)
	add(ActionDetailBottom, detail, TargetPage, []Key{Rune('G')}, "bottom", "Jump to bottom", "Detail", 20, 15, FullHelp)
	add(ActionDetailCopyDescription, detail, TargetPage, []Key{Rune('y')}, "copy desc", "Copy description", "Detail", 20, 20, FullHelp|Shortlist|ShortcutBar|InlineHint)
	add(ActionDetailCopyFull, detail, TargetPage, []Key{Rune('Y')}, "copy full", "Copy full task context", "Detail", 20, 21, FullHelp|InlineHint)
	add(ActionDetailMove, detail, TargetPage, []Key{Rune('m')}, "move", "Move task", "Detail", 20, 22, FullHelp|Shortlist|ShortcutBar|InlineHint)
	add(ActionDetailMoveWorkspace, detail, TargetPage, []Key{Rune('v')}, "move workspace", "Move task to workspace", "Detail", 20, 23, FullHelp|Shortlist|ShortcutBar|InlineHint)
	add(ActionDetailDelete, detail, TargetPage, []Key{Rune('D')}, "delete", "Delete task", "Detail", 20, 24, FullHelp|Shortlist|ShortcutBar|InlineHint)
	add(ActionDetailOpenURL, detail, TargetPage, []Key{Rune('o')}, "open", "Open remote task", "Detail", 20, 24, FullHelp|InlineHint)
	add(ActionDetailEditTitle, detail, TargetPage, []Key{Rune('t')}, "edit title", "Edit task title", "Detail", 20, 25, FullHelp|InlineHint)
	(*bindings)[len(*bindings)-1].DisplayWhen = []Predicate{PredicateLocalTask}
	add(ActionDetailEditDescription, detail, TargetPage, []Key{Rune('e')}, "edit details", "Edit Legato task details", "Detail", 20, 26, FullHelp|Shortlist|ShortcutBar|InlineHint)
	(*bindings)[len(*bindings)-1].DisplayWhen = []Predicate{PredicateEditor}
	add(ActionDetailNextSwarmStep, detail, TargetPage, []Key{Rune('s')}, "next step", "Advance swarm to next step", "Detail", 20, 27, InlineHint)
	(*bindings)[len(*bindings)-1].When = []Predicate{PredicateSwarmNext}

	agents := PageScope(PageAgents)
	add(ActionBack, agents, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "board", "Return to board", "Agents", 50, 1, FullHelp|ShortcutBar)
	add(ActionAgentsDown, agents, TargetPage, []Key{Rune('j')}, "select", "Select next agent", "Agents", 50, 10, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsUp, agents, TargetPage, []Key{Rune('k')}, "select", "Select previous agent", "Agents", 50, 11, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsSpawn, agents, TargetPage, []Key{Rune('s')}, "spawn", "Spawn agent", "Agents", 50, 20, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsKill, agents, TargetPage, []Key{Rune('X')}, "kill", "Kill agent", "Agents", 50, 21, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsAttach, agents, TargetPage, []Key{Special(tea.KeyEnter, "enter"), Special(tea.KeyTab, "tab")}, "attach", "Attach to agent session", "Agents", 50, 22, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsMacro, agents, TargetPage, []Key{Rune('m')}, "macro", "Send macro", "Agents", 50, 23, FullHelp|Shortlist|ShortcutBar)
	add(ActionAgentsGroup, agents, TargetPage, []Key{Rune('g')}, "group", "Set task group", "Agents", 50, 24, FullHelp)
	add(ActionAgentsActions, agents, TargetPage, []Key{Rune('M')}, "actions", "Open swarm agent actions", "Agents", 50, 25, FullHelp, CapabilitySwarm)
	add(ActionAgentsDetails, agents, TargetPage, []Key{Rune('l')}, "details", "Toggle worker details", "Agents", 50, 26, FullHelp)
	add(ActionAgentsNotify, agents, TargetPage, []Key{Rune('n')}, "notify", "Toggle notifications", "Agents", 50, 27, FullHelp|ShortcutBar, CapabilityNotifications)
	add(ActionAgentsVoice, agents, TargetPage, []Key{Rune('v')}, "voice", "Start voice dictation", "Agents", 50, 28, FullHelp|ShortcutBar, CapabilityVoice)

	report := PageScope(PageReport)
	add(ActionBack, report, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to board", "Reports", 60, 1, FullHelp|Shortlist|ShortcutBar)
	add(ActionReportPeriod, report, TargetPage, []Key{Special(tea.KeyTab, "tab")}, "period", "Change report period", "Reports", 60, 10, FullHelp|InlineHint)
	add(ActionReportPrevious, report, TargetPage, []Key{Rune('h'), Special(tea.KeyLeft, "left")}, "navigate", "Previous period", "Reports", 60, 11, FullHelp|InlineHint)
	add(ActionReportNext, report, TargetPage, []Key{Rune('l'), Special(tea.KeyRight, "right")}, "navigate", "Next period", "Reports", 60, 12, FullHelp|InlineHint)
	add(ActionReportDown, report, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "scroll", "Scroll down", "Reports", 60, 13, FullHelp|InlineHint)
	add(ActionReportUp, report, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "scroll", "Scroll up", "Reports", 60, 14, FullHelp|InlineHint)
	add(ActionReportCopy, report, TargetPage, []Key{Rune('C')}, "copy", "Copy report markdown", "Reports", 60, 20, FullHelp|Shortlist|ShortcutBar|InlineHint)

	addReviewBindings(bindings, add)
	addPlanBindings(bindings, add)
	attention := PageScope(PageAttention)
	add(ActionBack, attention, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to board", "Attention", 80, 1, FullHelp|Shortlist|ShortcutBar)
	add(ActionAttentionDown, attention, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "select", "Select next item", "Attention", 80, 10, FullHelp|Shortlist|ShortcutBar)
	add(ActionAttentionUp, attention, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "select", "Select previous item", "Attention", 80, 11, FullHelp|Shortlist|ShortcutBar)
	add(ActionAttentionExecute, attention, TargetPage, []Key{Special(tea.KeyEnter, "enter")}, "open", "Execute primary action", "Attention", 80, 20, FullHelp|Shortlist|ShortcutBar)
	add(ActionAttentionDismiss, attention, TargetPage, []Key{Rune('d')}, "dismiss", "Dismiss attention item", "Attention", 80, 21, FullHelp|Shortlist|ShortcutBar)
}

func addReviewBindings(bindings *[]Binding, add func(ActionID, Scope, Target, []Key, string, string, string, int, int, Surface, ...Capability)) {
	queue := PageModeScope(PageReview, ModeQueue)
	add(ActionBack, queue, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to board", "Reviews", 70, 1, FullHelp|ShortcutBar)
	add(ActionReviewDown, queue, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "select", "Select next review", "Reviews", 70, 10, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewUp, queue, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "select", "Select previous review", "Reviews", 70, 11, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewOpen, queue, TargetPage, []Key{Special(tea.KeyEnter, "enter")}, "open", "Open review tour", "Reviews", 70, 20, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewRefresh, queue, TargetPage, []Key{Rune('r')}, "refresh", "Refresh review queue", "Reviews", 70, 21, FullHelp)
	add(ActionReviewDelete, queue, TargetPage, []Key{Rune('x')}, "delete", "Delete review", "Reviews", 70, 22, FullHelp|ShortcutBar)
	tour := PageModeScope(PageReview, ModeTour)
	add(ActionReviewDown, tour, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "select", "Select next step", "Reviews", 70, 30, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewUp, tour, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "select", "Select previous step", "Reviews", 70, 31, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewHalfDown, tour, TargetPage, []Key{Rune('d'), Special(tea.KeyCtrlD, "ctrl+d")}, "page down", "Scroll diff down", "Reviews", 70, 32, FullHelp)
	add(ActionReviewHalfUp, tour, TargetPage, []Key{Rune('u'), Special(tea.KeyCtrlU, "ctrl+u")}, "page up", "Scroll diff up", "Reviews", 70, 33, FullHelp)
	add(ActionReviewTop, tour, TargetPage, []Key{Rune('g')}, "top", "Jump to top of diff", "Reviews", 70, 34, FullHelp)
	add(ActionReviewBottom, tour, TargetPage, []Key{Rune('G')}, "bottom", "Jump to bottom of diff", "Reviews", 70, 35, FullHelp)
	add(ActionReviewToggle, tour, TargetPage, []Key{Special(tea.KeySpace, "space")}, "reviewed", "Toggle step reviewed", "Reviews", 70, 36, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewAsk, tour, TargetPage, []Key{Rune('a')}, "ask", "Ask a question", "Reviews", 70, 37, FullHelp|Shortlist|ShortcutBar)
	add(ActionReviewComplete, tour, TargetPage, []Key{Rune('c')}, "complete", "Complete review", "Reviews", 70, 38, FullHelp|Shortlist)
	add(ActionReviewDelete, tour, TargetPage, []Key{Rune('x')}, "delete", "Delete review", "Reviews", 70, 39, FullHelp|Shortlist|ShortcutBar)
	add(ActionBack, tour, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to review queue", "Reviews", 70, 40, FullHelp|Shortlist|ShortcutBar)
}

func addPlanBindings(bindings *[]Binding, add func(ActionID, Scope, Target, []Key, string, string, string, int, int, Surface, ...Capability)) {
	queue := PageModeScope(PagePlans, ModeQueue)
	add(ActionBack, queue, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to board", "Plans", 75, 1, FullHelp|ShortcutBar)
	add(ActionPlanDown, queue, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "select", "Select next plan", "Plans", 75, 10, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanUp, queue, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "select", "Select previous plan", "Plans", 75, 11, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanOpen, queue, TargetPage, []Key{Special(tea.KeyEnter, "enter")}, "open", "Open plan", "Plans", 75, 20, FullHelp|Shortlist|ShortcutBar)
	reader := PageModeScope(PagePlans, ModeReader)
	add(ActionBack, reader, TargetPage, []Key{Special(tea.KeyEsc, "esc"), Rune('q')}, "back", "Return to plan queue", "Plans", 75, 30, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanDown, reader, TargetPage, []Key{Rune('j'), Special(tea.KeyDown, "down")}, "scroll", "Scroll down", "Plans", 75, 31, FullHelp)
	add(ActionPlanUp, reader, TargetPage, []Key{Rune('k'), Special(tea.KeyUp, "up")}, "scroll", "Scroll up", "Plans", 75, 32, FullHelp)
	add(ActionPlanNextQuestion, reader, TargetPage, []Key{Special(tea.KeyTab, "tab")}, "question", "Select next question", "Plans", 75, 33, FullHelp)
	add(ActionPlanAnswer, reader, TargetPage, []Key{Rune('e')}, "answer", "Answer selected question", "Plans", 75, 34, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanComment, reader, TargetPage, []Key{Rune('c')}, "comment", "Comment on plan", "Plans", 75, 35, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanQuestion, reader, TargetPage, []Key{Rune('?')}, "question", "Ask about plan", "Plans", 75, 36, FullHelp|Shortlist)
	add(ActionPlanApprove, reader, TargetPage, []Key{Rune('a')}, "approve", "Approve plan", "Plans", 75, 37, FullHelp|Shortlist|ShortcutBar)
	add(ActionPlanReject, reader, TargetPage, []Key{Rune('x')}, "reject", "Reject plan", "Plans", 75, 38, FullHelp)
	add(ActionPlanChanges, reader, TargetPage, []Key{Rune('r')}, "changes", "Request plan changes", "Plans", 75, 39, FullHelp)
	add(ActionPlanReopen, reader, TargetPage, []Key{Rune('o')}, "reopen", "Reopen plan", "Plans", 75, 40, FullHelp)
}
