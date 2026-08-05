package overlay

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/tui/theme"
)

type WorkflowTriggerSubmitMsg struct {
	RequestID, Repository string
	Remember              bool
}
type WorkflowTriggerCancelledMsg struct{}
type WorkflowTriggerSkipMsg struct{ RequestID string }
type WorkflowTriggerConfirmMsg struct{ RequestID string }

type WorkflowTriggerOverlay struct {
	requestID     string
	presets       []workflowengine.RepositoryPreset
	preset        int
	custom        bool
	values        [3]string
	focus         int
	remember      bool
	confirmation  bool
	workflowTitle string
	width, height int
}

func NewWorkflowTrigger(requestID string, presets []workflowengine.RepositoryPreset) WorkflowTriggerOverlay {
	m := WorkflowTriggerOverlay{requestID: requestID, presets: presets, remember: true, values: [3]string{"", "", "main"}}
	if len(presets) == 0 {
		m.custom = true
	}
	return m
}

func NewWorkflowConfirmation(requestID, workflowTitle string) WorkflowTriggerOverlay {
	return WorkflowTriggerOverlay{requestID: requestID, confirmation: true, workflowTitle: workflowTitle}
}
func (m WorkflowTriggerOverlay) Init() tea.Cmd { return nil }
func (m WorkflowTriggerOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return WorkflowTriggerCancelledMsg{} }
		case "s":
			id := m.requestID
			return m, func() tea.Msg { return WorkflowTriggerSkipMsg{RequestID: id} }
		case "r":
			if !m.confirmation {
				m.remember = !m.remember
			}
		case "tab", "down":
			if m.custom {
				m.focus = (m.focus + 1) % 3
			} else {
				m.preset = (m.preset + 1) % (len(m.presets) + 1)
				m.custom = m.preset == len(m.presets)
			}
		case "shift+tab", "up":
			if m.custom {
				m.focus = (m.focus + 2) % 3
			} else {
				m.preset = (m.preset + len(m.presets)) % (len(m.presets) + 1)
				m.custom = m.preset == len(m.presets)
			}
		case "backspace":
			if m.custom && len(m.values[m.focus]) > 0 {
				m.values[m.focus] = m.values[m.focus][:len(m.values[m.focus])-1]
			}
		case "enter":
			if m.confirmation {
				id := m.requestID
				return m, func() tea.Msg { return WorkflowTriggerConfirmMsg{RequestID: id} }
			}
			var repository string
			if m.custom {
				if strings.TrimSpace(m.values[0]) == "" || strings.TrimSpace(m.values[1]) == "" {
					return m, nil
				}
				encoded, _ := json.Marshal(map[string]string{"repo": m.values[0], "primary_dir": m.values[1], "base_branch": m.values[2]})
				repository = string(encoded)
			} else if len(m.presets) > 0 {
				repository = m.presets[m.preset].Name
			}
			id, remember := m.requestID, m.remember
			return m, func() tea.Msg {
				return WorkflowTriggerSubmitMsg{RequestID: id, Repository: repository, Remember: remember}
			}
		default:
			if m.custom && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
				m.values[m.focus] += string(msg.Runes)
				if msg.Type == tea.KeySpace {
					m.values[m.focus] += " "
				}
			}
		}
	}
	return m, nil
}
func (m WorkflowTriggerOverlay) View() string {
	if m.confirmation {
		title := m.workflowTitle
		if title == "" {
			title = "Start workflow"
		}
		lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render(title), "", "Run this workflow?", "", lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("enter: start  esc: not now  s: skip")}
		return lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(strings.Join(lines, "\n"))
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Repository required")}
	if !m.custom {
		for i, preset := range m.presets {
			marker := "  "
			if i == m.preset {
				marker = "> "
			}
			lines = append(lines, marker+preset.Name+"  "+preset.Repo)
		}
		lines = append(lines, "  Custom")
	} else {
		labels := []string{"Repository", "Primary directory", "Base branch"}
		for i, label := range labels {
			marker := "  "
			if i == m.focus {
				marker = "> "
			}
			lines = append(lines, marker+label+": "+m.values[i])
		}
	}
	remember := "no"
	if m.remember {
		remember = "yes"
	}
	lines = append(lines, "", "Remember on task: "+remember, lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("enter: start  r: remember  esc: not now  s: skip"))
	return lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(strings.Join(lines, "\n"))
}
