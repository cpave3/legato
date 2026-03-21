package overlay

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/tui/theme"
)

// LinkPRFetchMsg is sent when the user submits the PR form for fetching.
type LinkPRFetchMsg struct {
	TaskID   string
	Owner    string
	Repo     string
	PRNumber int
}

// LinkPRConfirmedMsg is sent when the user confirms linking the PR.
type LinkPRConfirmedMsg struct {
	TaskID   string
	Owner    string
	Repo     string
	PRNumber int
}

// LinkPRCancelledMsg is sent when the user dismisses the overlay.
type LinkPRCancelledMsg struct{}

// LinkPRResultMsg carries fetched PR details back to the overlay.
type LinkPRResultMsg struct {
	Status *github.PRStatus
	Err    string
}

type linkPRPhase int

const (
	phaseInput   linkPRPhase = iota
	phaseLoading
	phaseConfirm
)

type linkPRField int

const (
	fieldRepo linkPRField = iota
	fieldNumber
)

// LinkPROverlay provides a two-phase overlay for linking a PR to a task.
type LinkPROverlay struct {
	taskID    string
	repoInput string // "owner/repo"
	numInput  string // PR number as string
	focus     linkPRField
	phase     linkPRPhase
	status    *github.PRStatus
	errMsg    string
	width     int
	height    int
}

// NewLinkPR creates a new link PR overlay.
func NewLinkPR(taskID string, defaultRepo string, width, height int) LinkPROverlay {
	return LinkPROverlay{
		taskID:    taskID,
		repoInput: defaultRepo,
		focus:     fieldNumber, // start on PR number since repo is pre-filled
		phase:     phaseInput,
		width:     width,
		height:    height,
	}
}

// SetResult updates the overlay with fetched PR data.
func (m LinkPROverlay) SetResult(status *github.PRStatus) LinkPROverlay {
	m.status = status
	m.phase = phaseConfirm
	m.errMsg = ""
	return m
}

// SetError displays an error and returns to input phase.
func (m LinkPROverlay) SetError(err string) LinkPROverlay {
	m.errMsg = err
	m.phase = phaseInput
	return m
}

func (m LinkPROverlay) Init() tea.Cmd {
	return nil
}

func (m LinkPROverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.phase {
		case phaseInput:
			return m.handleInputKey(msg)
		case phaseConfirm:
			return m.handleConfirmKey(msg)
		case phaseLoading:
			if msg.String() == "esc" {
				return m, func() tea.Msg { return LinkPRCancelledMsg{} }
			}
		}
	}
	return m, nil
}

func (m LinkPROverlay) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return LinkPRCancelledMsg{} }

	case "tab":
		if m.focus == fieldRepo {
			m.focus = fieldNumber
		} else {
			m.focus = fieldRepo
		}
		return m, nil

	case "enter":
		owner, repo, err := parseRepoInput(m.repoInput)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		num, err := strconv.Atoi(strings.TrimSpace(m.numInput))
		if err != nil || num <= 0 {
			m.errMsg = "Enter a valid PR number"
			return m, nil
		}
		m.errMsg = ""
		m.phase = phaseLoading
		taskID := m.taskID
		return m, func() tea.Msg {
			return LinkPRFetchMsg{TaskID: taskID, Owner: owner, Repo: repo, PRNumber: num}
		}

	case "backspace":
		if m.focus == fieldRepo && len(m.repoInput) > 0 {
			m.repoInput = m.repoInput[:len(m.repoInput)-1]
		} else if m.focus == fieldNumber && len(m.numInput) > 0 {
			m.numInput = m.numInput[:len(m.numInput)-1]
		}
		m.errMsg = ""
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			ch := string(msg.Runes)
			if m.focus == fieldRepo {
				m.repoInput += ch
			} else {
				// Only allow digits for PR number
				if ch >= "0" && ch <= "9" {
					m.numInput += ch
				}
			}
			m.errMsg = ""
		}
		return m, nil
	}
}

func (m LinkPROverlay) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		owner, repo, _ := parseRepoInput(m.repoInput)
		num, _ := strconv.Atoi(strings.TrimSpace(m.numInput))
		taskID := m.taskID
		return m, func() tea.Msg {
			return LinkPRConfirmedMsg{TaskID: taskID, Owner: owner, Repo: repo, PRNumber: num}
		}
	case "n", "esc":
		return m, func() tea.Msg { return LinkPRCancelledMsg{} }
	}
	return m, nil
}

func parseRepoInput(input string) (owner, repo string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("Enter owner/repo")
	}
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("Use owner/repo format")
	}
	return parts[0], parts[1], nil
}

func (m LinkPROverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary)
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	inputStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	errStyle := lipgloss.NewStyle().Foreground(theme.SyncError)

	var lines []string
	lines = append(lines, titleStyle.Render("Link PR"))
	lines = append(lines, "")

	switch m.phase {
	case phaseInput, phaseLoading:
		repoCursor, numCursor := " ", " "
		repoSuffix, numSuffix := "", ""
		if m.phase == phaseInput {
			if m.focus == fieldRepo {
				repoCursor = ">"
				repoSuffix = "█"
			} else {
				numCursor = ">"
				numSuffix = "█"
			}
		}

		lines = append(lines,
			labelStyle.Render(repoCursor+" Repo:  ")+inputStyle.Render(m.repoInput+repoSuffix),
			labelStyle.Render(numCursor+" PR #:  ")+inputStyle.Render(m.numInput+numSuffix),
		)

		if m.errMsg != "" {
			lines = append(lines, "", errStyle.Render("  "+m.errMsg))
		}

		lines = append(lines, "")
		if m.phase == phaseLoading {
			lines = append(lines, hintStyle.Render("  Fetching PR details..."))
		} else {
			lines = append(lines, hintStyle.Render("  tab: switch  enter: fetch  esc: cancel"))
		}

	case phaseConfirm:
		if m.status != nil {
			s := m.status
			stateStyle := lipgloss.NewStyle().Foreground(theme.SyncOK)
			if s.State == "CLOSED" {
				stateStyle = stateStyle.Foreground(theme.SyncError)
			} else if s.State == "MERGED" {
				stateStyle = stateStyle.Foreground(theme.AccentPurple)
			}

			lines = append(lines,
				inputStyle.Render(fmt.Sprintf("  #%d: %s", s.Number, s.Title)),
			)
			if s.HeadBranch != "" {
				lines = append(lines, labelStyle.Render("  Branch: ")+hintStyle.Render(s.HeadBranch))
			}
			stateLine := labelStyle.Render("  State:  ") + stateStyle.Render(s.State)
			if s.IsDraft {
				stateLine += labelStyle.Render(" (draft)")
			}
			lines = append(lines, stateLine)

			if s.CommentCount > 0 {
				lines = append(lines, labelStyle.Render(fmt.Sprintf("  Comments: %d", s.CommentCount)))
			}

			lines = append(lines, "")
			lines = append(lines, hintStyle.Render(fmt.Sprintf("  Link to task %s?", m.taskID)))
			lines = append(lines, hintStyle.Render("  y: confirm  n: cancel"))
		}
	}

	panelWidth := m.width * 50 / 100
	if panelWidth < 45 {
		panelWidth = 45
	}
	contentWidth := panelWidth - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	content := lipgloss.NewStyle().Width(contentWidth).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
	return RenderPanel(content, m.width, m.height)
}
