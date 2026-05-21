package overlay

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// PlanApproveMsg is emitted when the user approves a plan via the overlay.
type PlanApproveMsg struct {
	ParentTaskID string
	PlanPath     string
	ReplySocket  string
}

// PlanRejectMsg is emitted when the user rejects a plan with notes.
type PlanRejectMsg struct {
	ParentTaskID string
	PlanPath     string
	ReplySocket  string
	Notes        string
}

// PlanCancelMsg is emitted when the user dismisses the overlay without rendering
// a verdict. The pending plan is retained in app state.
type PlanCancelMsg struct {
	ParentTaskID string
	PlanPath     string
	ReplySocket  string
}

// PlanEditedMsg is emitted after the user finishes editing the plan in $EDITOR.
// The app handles this by reloading the plan and dispatching PlanReloadedMsg.
type PlanEditedMsg struct {
	ParentTaskID string
	PlanPath     string
	Err          error
}

// PlanReloadedMsg carries a freshly-parsed plan back to the overlay after an
// editor session. Produced by the app (which holds the SwarmService); consumed
// by the overlay so it can re-render without ever importing engine types.
type PlanReloadedMsg struct {
	ParentTaskID string
	PlanPath     string
	Plan         *service.SwarmPlan
	Err          error
}

// planMode tracks the overlay's substate.
type planMode int

const (
	planModeReview planMode = iota
	planModeRejecting
)

// PlanApprovalOverlay surfaces a conductor-proposed plan for HITL review.
type PlanApprovalOverlay struct {
	parentTaskID string
	planPath     string
	replySocket  string
	editor       string
	plan         *service.SwarmPlan
	loadErr      error
	mode         planMode
	notes        string
	width        int
	height       int
	isExtension  bool
}

// NewPlanApproval constructs the overlay with a pre-loaded plan. Pass loadErr
// non-nil if loading failed so the overlay can render the error state.
func NewPlanApproval(parentTaskID, planPath, replySocket, editor string, plan *service.SwarmPlan, loadErr error, opts ...func(*PlanApprovalOverlay)) PlanApprovalOverlay {
	m := PlanApprovalOverlay{
		parentTaskID: parentTaskID,
		planPath:     planPath,
		replySocket:  replySocket,
		editor:       editor,
		plan:         plan,
		loadErr:      loadErr,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// WithExtension marks the overlay as an extension plan approval.
func WithExtension(isExt bool) func(*PlanApprovalOverlay) {
	return func(m *PlanApprovalOverlay) {
		m.isExtension = isExt
	}
}

// ParentTaskID returns the parent task ID being reviewed in this overlay.
func (m PlanApprovalOverlay) ParentTaskID() string { return m.parentTaskID }

func (m PlanApprovalOverlay) Init() tea.Cmd { return nil }

func (m PlanApprovalOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case PlanReloadedMsg:
		if msg.Err == nil {
			m.plan = msg.Plan
			m.loadErr = nil
		} else {
			m.loadErr = msg.Err
		}
		return m, nil
	case tea.KeyMsg:
		if m.mode == planModeRejecting {
			return m.handleRejectInput(msg)
		}
		return m.handleReviewKey(msg)
	}
	return m, nil
}

func (m PlanApprovalOverlay) handleReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if m.plan == nil {
			return m, nil
		}
		parent, path, sock := m.parentTaskID, m.planPath, m.replySocket
		return m, func() tea.Msg {
			return PlanApproveMsg{ParentTaskID: parent, PlanPath: path, ReplySocket: sock}
		}
	case "e":
		editor := m.editor
		if editor == "" {
			editor = "vi"
		}
		parts := strings.Fields(editor)
		parts = append(parts, m.planPath)
		c := exec.Command(parts[0], parts[1:]...)
		parent, path := m.parentTaskID, m.planPath
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return PlanEditedMsg{ParentTaskID: parent, PlanPath: path, Err: err}
		})
	case "n":
		m.mode = planModeRejecting
		return m, nil
	case "esc":
		parent, path, sock := m.parentTaskID, m.planPath, m.replySocket
		return m, func() tea.Msg {
			return PlanCancelMsg{ParentTaskID: parent, PlanPath: path, ReplySocket: sock}
		}
	}
	return m, nil
}

func (m PlanApprovalOverlay) handleRejectInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = planModeReview
		m.notes = ""
		return m, nil
	case "enter":
		parent, path, sock, notes := m.parentTaskID, m.planPath, m.replySocket, m.notes
		return m, func() tea.Msg {
			return PlanRejectMsg{ParentTaskID: parent, PlanPath: path, ReplySocket: sock, Notes: notes}
		}
	case "backspace":
		if len(m.notes) > 0 {
			m.notes = m.notes[:len(m.notes)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeySpace {
			m.notes += " "
		} else if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.notes += string(msg.Runes)
		}
		return m, nil
	}
}

func (m PlanApprovalOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentPurple)
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	bodyStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)

	var heading string
	if m.isExtension {
		heading = titleStyle.Render("Extension plan: append sub-tasks to existing swarm")
	} else {
		heading = titleStyle.Render("Plan proposed for " + m.parentTaskID)
	}

	if m.loadErr != nil {
		body := bodyStyle.Render(fmt.Sprintf("Failed to load plan: %v\n\nPath: %s", m.loadErr, m.planPath))
		hint := hintStyle.Render("e edit · esc dismiss")
		return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, heading, "", body, "", hint), m.width, m.height)
	}

	if m.mode == planModeRejecting {
		notesLine := lipgloss.NewStyle().Foreground(theme.AccentPurple).Render(m.notes + "█")
		body := lipgloss.JoinVertical(lipgloss.Left,
			labelStyle.Render("Rejection notes (sent back to the conductor):"),
			"",
			notesLine,
		)
		hint := hintStyle.Render("enter submit · esc back")
		return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, heading, "", body, "", hint), m.width, m.height)
	}

	var lines []string
	lines = append(lines, heading)
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("Working dir:")+" "+bodyStyle.Render(m.plan.Header.WorkingDir))
	if m.plan.Header.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, bodyStyle.Render(strings.TrimSpace(m.plan.Header.Summary)))
	}
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render(fmt.Sprintf("Sub-tasks (%d):", len(m.plan.Subtasks))))
	for i, st := range m.plan.Subtasks {
		role := st.Role
		if role == "" {
			role = "(no role)"
		}
		agent := st.Agent
		if agent == "" {
			agent = "(default)"
		}
		scope := strings.Join(st.Scope, ", ")
		if scope == "" {
			scope = "(no scope)"
		}
		header := fmt.Sprintf("  %d. %s — role=%s, agent=%s", i+1, st.Title, role, agent)
		if st.Tier != "" {
			header += ", tier=" + st.Tier
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true).Render(header))
		lines = append(lines, labelStyle.Render("       scope: ")+bodyStyle.Render(scope))
		if st.Prompt != "" {
			preview := firstLine(st.Prompt, 100)
			lines = append(lines, labelStyle.Render("       brief: ")+bodyStyle.Render(preview))
		}
	}
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("y approve · e edit · n reject with notes · esc dismiss"))

	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, lines...), m.width, m.height)
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}
