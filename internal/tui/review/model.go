// Package review implements the review-tour TUI: a queue of tasks awaiting
// review and a steppable tour view showing each step's diff and narration.
package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

type mode int

const (
	modeQueue mode = iota
	modeTour
)

// Model is the review view Bubbletea model.
// Service is the review behavior required by the TUI.
type Service interface {
	Queue(context.Context) ([]service.ReviewQueueItem, error)
	Tour(context.Context, string) (*service.ReviewTourView, error)
	StepDiff(context.Context, string, string) ([]gitpkg.FileDiff, error)
	SetReviewed(context.Context, string, string, bool) error
	AskQuestion(context.Context, string, string, string) error
	Complete(context.Context, string) error
}

type Model struct {
	svc Service

	mode        mode
	queue       []service.ReviewQueueItem
	queueCursor int

	taskID  string
	view    *service.ReviewTourView
	stepIdx int
	diff    []gitpkg.FileDiff
	diffErr string

	viewport viewport.Model

	asking bool
	input  string

	info   string
	err    error
	width  int
	height int
}

// New creates the review view model.
func New(svc Service) Model {
	return Model{svc: svc}
}

// SetSize updates the dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = m.rightPaneWidth()
	m.viewport.Height = m.paneHeight()
}

// Init loads the review queue.
func (m Model) Init() tea.Cmd {
	return m.loadQueue()
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case QueueLoadedMsg:
		m.err = msg.Err
		if msg.Err == nil {
			m.queue = msg.Items
			if m.queueCursor >= len(m.queue) {
				m.queueCursor = max(0, len(m.queue)-1)
			}
		}
		return m, nil
	case TourLoadedMsg:
		m.err = msg.Err
		if msg.Err != nil {
			return m, nil
		}
		m.view = msg.View
		m.mode = modeTour
		if m.stepIdx >= len(m.view.Steps) {
			m.stepIdx = max(0, len(m.view.Steps)-1)
		}
		m.refreshViewport()
		return m, m.loadDiff()
	case DiffLoadedMsg:
		// Ignore stale loads from a step the user already moved past.
		if cur := m.currentStep(); cur == nil || cur.ID != msg.StepID {
			return m, nil
		}
		m.diff = msg.Files
		m.diffErr = ""
		if msg.Err != nil {
			m.diffErr = msg.Err.Error()
		}
		m.refreshViewport()
		return m, nil
	case ActionDoneMsg:
		m.err = msg.Err
		m.info = msg.Info
		if msg.TaskID != "" {
			cmd := m.loadTour(msg.TaskID)
			return m, cmd
		}
		return m, nil
	case ReviewChangedMsg:
		// Live update: reload whatever is on screen.
		if m.mode == modeTour && msg.TaskID == m.taskID {
			cmd := m.loadTour(m.taskID)
			return m, cmd
		}
		return m, m.loadQueue()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.asking {
		return m.handleQuestionKey(msg)
	}
	switch m.mode {
	case modeTour:
		return m.handleTourKey(msg)
	default:
		return m.handleQueueKey(msg)
	}
}

func (m Model) handleQueueKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.queueCursor < len(m.queue)-1 {
			m.queueCursor++
		}
	case "k", "up":
		if m.queueCursor > 0 {
			m.queueCursor--
		}
	case "enter":
		if len(m.queue) > 0 {
			cmd := m.loadTour(m.queue[m.queueCursor].TaskID)
			return m, cmd
		}
	case "r":
		return m, m.loadQueue()
	case "esc", "q":
		return m, func() tea.Msg { return ReturnToBoardMsg{} }
	}
	return m, nil
}

func (m Model) handleTourKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.view != nil && m.stepIdx < len(m.view.Steps)-1 {
			m.stepIdx++
			m.diff = nil
			m.refreshViewport()
			return m, m.loadDiff()
		}
	case "k", "up":
		if m.stepIdx > 0 {
			m.stepIdx--
			m.diff = nil
			m.refreshViewport()
			return m, m.loadDiff()
		}
	case "d", "ctrl+d":
		m.viewport.HalfViewDown()
	case "u", "ctrl+u":
		m.viewport.HalfViewUp()
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	case " ", "space":
		if step := m.currentStep(); step != nil {
			return m, m.toggleReviewed(step.ID, step.ReviewedAt == nil)
		}
	case "a":
		if m.currentStep() != nil {
			m.asking = true
			m.input = ""
		}
	case "c":
		return m, m.complete()
	case "esc", "q":
		m.mode = modeQueue
		m.view = nil
		m.diff = nil
		return m, m.loadQueue()
	}
	return m, nil
}

func (m Model) handleQuestionKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.asking = false
		m.input = ""
	case tea.KeyEnter:
		m.asking = false
		text := strings.TrimSpace(m.input)
		m.input = ""
		if step := m.currentStep(); step != nil && text != "" {
			return m, m.askQuestion(step.ID, text)
		}
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
	case tea.KeyRunes:
		m.input += string(msg.Runes)
	case tea.KeySpace:
		m.input += " "
	}
	return m, nil
}

func (m Model) currentStep() *store.ReviewStep {
	if m.view == nil || m.stepIdx >= len(m.view.Steps) {
		return nil
	}
	return &m.view.Steps[m.stepIdx]
}

// Commands

func (m Model) loadQueue() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		items, err := svc.Queue(context.Background())
		return QueueLoadedMsg{Items: items, Err: err}
	}
}

func (m *Model) loadTour(taskID string) tea.Cmd {
	m.taskID = taskID
	svc := m.svc
	return func() tea.Msg {
		view, err := svc.Tour(context.Background(), taskID)
		return TourLoadedMsg{View: view, Err: err}
	}
}

func (m Model) loadDiff() tea.Cmd {
	step := m.currentStep()
	if step == nil {
		return nil
	}
	svc, taskID, stepID := m.svc, m.taskID, step.ID
	return func() tea.Msg {
		files, err := svc.StepDiff(context.Background(), taskID, stepID)
		return DiffLoadedMsg{StepID: stepID, Files: files, Err: err}
	}
}

func (m Model) toggleReviewed(stepID string, reviewed bool) tea.Cmd {
	svc, taskID := m.svc, m.taskID
	return func() tea.Msg {
		err := svc.SetReviewed(context.Background(), taskID, stepID, reviewed)
		return ActionDoneMsg{TaskID: taskID, Err: err}
	}
}

func (m Model) askQuestion(stepID, text string) tea.Cmd {
	svc, taskID := m.svc, m.taskID
	return func() tea.Msg {
		err := svc.AskQuestion(context.Background(), taskID, stepID, text)
		info := "question sent"
		if err != nil {
			// The question is stored even when the agent is offline; keep
			// the tour open and surface the situation instead of failing.
			info = err.Error()
			err = nil
		}
		return ActionDoneMsg{TaskID: taskID, Info: info, Err: err}
	}
}

func (m Model) complete() tea.Cmd {
	svc, taskID := m.svc, m.taskID
	return func() tea.Msg {
		err := svc.Complete(context.Background(), taskID)
		return ActionDoneMsg{TaskID: taskID, Info: "review completed", Err: err}
	}
}

// Rendering

func (m Model) leftPaneWidth() int  { return min(44, m.width*2/5) }
func (m Model) rightPaneWidth() int { return max(20, m.width-m.leftPaneWidth()-3) }
func (m Model) paneHeight() int     { return max(4, m.height-4) }

// View renders the review view.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.mode == modeTour && m.view != nil {
		return m.viewTour()
	}
	return m.viewQueue()
}

func (m Model) viewQueue() string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	selStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)

	var lines []string
	lines = append(lines, titleStyle.Render("REVIEW QUEUE"), "")
	if m.err != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.err.Error()), "")
	}
	if len(m.queue) == 0 {
		lines = append(lines, dimStyle.Render("Nothing to review. Agents appear here when they signal `legato review ready`."))
	}
	for i, item := range m.queue {
		row := fmt.Sprintf("%s — %d unreviewed [%s]", item.Title, item.Unreviewed, item.Status)
		if item.Summary != "" {
			row += dimStyle.Render("  " + item.Summary)
		}
		if i == m.queueCursor {
			lines = append(lines, selStyle.Render("▸ "+row))
		} else {
			lines = append(lines, "  "+dimStyle.Render(item.TaskID)+" "+row)
		}
	}
	lines = append(lines, "", dimStyle.Render("j/k move · enter open · r refresh · esc back"))
	return lipgloss.NewStyle().Width(m.width).Height(m.height).Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m Model) viewTour() string {
	left := m.renderStepList()
	right := m.viewport.View()

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.leftPaneWidth()).Render(left),
		lipgloss.NewStyle().Width(1).Render(" "),
		lipgloss.NewStyle().Width(m.rightPaneWidth()).Render(right),
	)

	header := m.renderTourHeader()
	thread := ""
	if step := m.currentStep(); step != nil {
		thread = m.renderThread(step.ID)
	}
	footer := m.renderTourFooter()
	return lipgloss.NewStyle().Width(m.width).Height(m.height).Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body, thread, footer))
}

func (m Model) renderTourHeader() string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	reviewed := 0
	for _, s := range m.view.Steps {
		if s.ReviewedAt != nil {
			reviewed++
		}
	}
	title := titleStyle.Render("REVIEW ") + m.view.Tour.TaskID
	progress := dimStyle.Render(fmt.Sprintf("  %d/%d reviewed · %s", reviewed, len(m.view.Steps), m.view.Tour.Status))
	summary := ""
	if m.view.Tour.Summary != "" {
		summary = "\n" + dimStyle.Render(truncate(m.view.Tour.Summary, m.width-4))
	}
	return title + progress + summary
}

func (m Model) renderStepList() string {
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	selStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)

	var lines []string
	for i, s := range m.view.Steps {
		mark := "○"
		if s.ReviewedAt != nil {
			mark = lipgloss.NewStyle().Foreground(theme.SyncOK).Render("✓")
		}
		risk := ""
		if s.Risk != "" {
			risk = " " + riskStyle(s.Risk).Render("!"+s.Risk)
		}
		orphan := ""
		if s.OrphanedAt != nil {
			orphan = dimStyle.Render(" (rewritten)")
		}
		title := truncate(s.Title, m.leftPaneWidth()-8)
		row := fmt.Sprintf("%s %s%s%s", mark, title, risk, orphan)
		if i == m.stepIdx {
			lines = append(lines, selStyle.Render("▸ "+row))
		} else {
			lines = append(lines, "  "+row)
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderTourFooter() string {
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	if m.asking {
		return lipgloss.NewStyle().Foreground(theme.TextPrimary).
			Render("Question: "+m.input) + dimStyle.Render(" (enter send · esc cancel)")
	}
	line := "j/k step · d/u scroll · space reviewed · a ask · c complete · esc queue"
	if m.info != "" {
		line = m.info + " · " + line
	}
	if m.err != nil {
		line = m.err.Error() + " · " + line
	}
	return dimStyle.Render(truncate(line, m.width-4))
}

// refreshViewport rebuilds the right-pane content for the focused step:
// narration, diff, then the step's Q&A thread.
func (m *Model) refreshViewport() {
	m.viewport.Width = m.rightPaneWidth()
	m.viewport.Height = m.paneHeight()

	step := m.currentStep()
	if step == nil {
		m.viewport.SetContent("")
		return
	}

	var sections []string
	titleStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	head := titleStyle.Render(step.Title)
	if step.CommitSHA != "" {
		head += dimStyle.Render("  " + shortSHA(step.CommitSHA))
	}
	sections = append(sections, head)

	if step.Narration != "" {
		narration := step.Narration
		if renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(styles.DarkStyleConfig),
			glamour.WithWordWrap(max(20, m.rightPaneWidth()-2)),
		); err == nil {
			if rendered, renderErr := renderer.Render(step.Narration); renderErr == nil {
				narration = strings.TrimSpace(rendered)
			}
		}
		sections = append(sections, narration)
	}

	if m.diffErr != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.diffErr))
	} else if len(m.diff) == 0 {
		sections = append(sections, dimStyle.Render("(loading diff...)"))
	} else {
		sections = append(sections, renderDiff(m.diff, m.rightPaneWidth()))
	}

	m.viewport.SetContent(strings.Join(sections, "\n\n"))
	m.viewport.GotoTop()
}

func (m Model) renderThread(stepID string) string {
	dimStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	qStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple)
	aStyle := lipgloss.NewStyle().Foreground(theme.SyncOK)

	var lines []string
	for _, msg := range m.view.Messages {
		if msg.StepID != stepID {
			continue
		}
		prefix := qStyle.Render("Q: ")
		if msg.Kind == "answer" {
			prefix = aStyle.Render("A: ")
		}
		suffix := ""
		if msg.Kind == "question" && msg.DeliveredAt == nil {
			suffix = dimStyle.Render(" (not delivered — agent offline)")
		}
		lines = append(lines, prefix+msg.Body+suffix)
	}
	if len(lines) == 0 {
		return ""
	}
	return dimStyle.Render("— thread —") + "\n" + strings.Join(lines, "\n")
}

// renderDiff colorizes parsed FileDiffs for the terminal.
func renderDiff(files []gitpkg.FileDiff, width int) string {
	fileStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	hunkStyle := lipgloss.NewStyle().Foreground(theme.ColReady)
	addStyle := lipgloss.NewStyle().Foreground(theme.SyncOK)
	delStyle := lipgloss.NewStyle().Foreground(theme.SyncError)
	ctxStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	var lines []string
	for _, f := range files {
		header := f.NewPath
		if f.Status == gitpkg.FileRenamed {
			header = f.OldPath + " → " + f.NewPath
		}
		lines = append(lines, fileStyle.Render(fmt.Sprintf("── %s (%s)", header, f.Status)))
		if f.Status == gitpkg.FileBinary {
			lines = append(lines, ctxStyle.Render("(binary file)"))
			continue
		}
		for _, h := range f.Hunks {
			lines = append(lines, hunkStyle.Render(h.Header))
			for _, l := range h.Lines {
				text := truncate(l.Text, width-2)
				switch l.Kind {
				case gitpkg.LineAdded:
					lines = append(lines, addStyle.Render("+"+text))
				case gitpkg.LineDeleted:
					lines = append(lines, delStyle.Render("-"+text))
				default:
					lines = append(lines, ctxStyle.Render(" "+text))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func riskStyle(risk string) lipgloss.Style {
	switch risk {
	case "high":
		return lipgloss.NewStyle().Foreground(theme.SyncError).Bold(true)
	case "medium", "unsure":
		return lipgloss.NewStyle().Foreground(theme.SyncActive)
	default:
		return lipgloss.NewStyle().Foreground(theme.TextTertiary)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
