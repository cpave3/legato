package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

type CardContextAction int

const (
	CardContextOpen CardContextAction = iota
	CardContextMove
	CardContextDelete
)

type CardContextActionMsg struct {
	TaskID string
	Action CardContextAction
}

type CardContextCancelledMsg struct{}
type CardContextLoadWorkflowsMsg struct{ TaskID string }
type CardContextStartWorkflowMsg struct {
	Name   string
	Inputs map[string]string
}

type cardContextScreen int

const (
	cardContextMain cardContextScreen = iota
	cardContextWorkflows
	cardContextForm
)

type CardContextOverlay struct {
	taskID        string
	screen        cardContextScreen
	cursor        int
	workflows     []service.TaskWorkflowOption
	loading       bool
	errorText     string
	warningText   string
	selected      int
	values        map[string]string
	fields        []service.TaskWorkflowInput
	width, height int
}

func NewCardContext(taskID string) CardContextOverlay {
	return CardContextOverlay{taskID: taskID}
}

func (m CardContextOverlay) Init() tea.Cmd { return nil }

func (m CardContextOverlay) ConsumesPrintableInput() bool {
	return m.screen == cardContextForm
}

func (m CardContextOverlay) SetWorkflows(workflows []service.TaskWorkflowOption, err error) CardContextOverlay {
	m.screen, m.cursor, m.loading = cardContextWorkflows, 0, false
	m.workflows = workflows
	m.errorText, m.warningText = "", ""
	if err != nil {
		if len(workflows) == 0 {
			m.errorText = err.Error()
		} else {
			m.warningText = err.Error()
		}
	}
	return m
}

func (m CardContextOverlay) SetError(err error) CardContextOverlay {
	if err != nil {
		m.errorText = err.Error()
	}
	return m
}

func (m CardContextOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m CardContextOverlay) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		if m.screen == cardContextMain {
			return m, func() tea.Msg { return CardContextCancelledMsg{} }
		}
		if m.screen == cardContextForm {
			m.screen, m.cursor, m.errorText = cardContextWorkflows, m.selected, ""
			return m, nil
		}
		m.screen, m.cursor, m.errorText = cardContextMain, 0, ""
		return m, nil
	}
	if m.screen == cardContextForm {
		return m.updateForm(msg)
	}
	count := 4
	if m.screen == cardContextWorkflows {
		count = len(m.workflows)
	}
	switch msg.String() {
	case "j", "down":
		if m.cursor+1 < count {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if m.screen == cardContextMain {
			switch m.cursor {
			case 0, 1, 2:
				action := CardContextAction(m.cursor)
				return m, func() tea.Msg { return CardContextActionMsg{TaskID: m.taskID, Action: action} }
			case 3:
				m.screen, m.cursor, m.loading, m.errorText = cardContextWorkflows, 0, true, ""
				id := m.taskID
				return m, func() tea.Msg { return CardContextLoadWorkflowsMsg{TaskID: id} }
			}
		} else if !m.loading && m.errorText == "" && m.cursor < len(m.workflows) {
			m.openForm(m.cursor)
		}
	}
	return m, nil
}

func (m *CardContextOverlay) openForm(index int) {
	m.screen, m.selected, m.cursor, m.errorText = cardContextForm, index, 0, ""
	m.values = make(map[string]string, len(m.workflows[index].Values))
	for key, value := range m.workflows[index].Values {
		m.values[key] = value
	}
	m.fields = m.fields[:0]
	for _, input := range m.workflows[index].Inputs {
		if input.Key != m.workflows[index].TaskInput {
			m.fields = append(m.fields, input)
			if m.values[input.Key] == "" {
				if len(input.Choices) > 0 {
					m.values[input.Key] = input.Choices[0]
				} else if input.Type == "boolean" {
					m.values[input.Key] = "false"
				}
			}
		}
	}
}

func (m CardContextOverlay) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.fields) == 0 {
		if msg.String() == "enter" {
			return m.submit()
		}
		return m, nil
	}
	field := m.fields[m.cursor]
	choices := field.Choices
	if field.Type == "boolean" && len(choices) == 0 {
		choices = []string{"true", "false"}
	}
	switch msg.String() {
	case "tab", "down":
		m.cursor = (m.cursor + 1) % len(m.fields)
	case "shift+tab", "up":
		m.cursor = (m.cursor + len(m.fields) - 1) % len(m.fields)
	case "left", "right":
		if len(choices) > 0 {
			m.values[field.Key] = nextChoice(choices, m.values[field.Key], msg.String() == "right")
		}
	case "backspace":
		if len(choices) == 0 {
			value := []rune(m.values[field.Key])
			if len(value) > 0 {
				m.values[field.Key] = string(value[:len(value)-1])
			}
		}
	case "enter":
		return m.submit()
	default:
		if len(choices) == 0 && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
			m.values[field.Key] += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				m.values[field.Key] += " "
			}
		}
	}
	return m, nil
}

func (m CardContextOverlay) submit() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.selected]
	inputs := make(map[string]string, len(m.values))
	for key, value := range m.values {
		inputs[key] = value
	}
	return m, func() tea.Msg { return CardContextStartWorkflowMsg{Name: workflow.Name, Inputs: inputs} }
}

func nextChoice(choices []string, current string, forward bool) string {
	index := 0
	for i, choice := range choices {
		if choice == current {
			index = i
			break
		}
	}
	if forward {
		index = (index + 1) % len(choices)
	} else {
		index = (index + len(choices) - 1) % len(choices)
	}
	return choices[index]
}

func (m CardContextOverlay) View() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary).Render("Task " + m.taskID)}
	switch m.screen {
	case cardContextMain:
		lines = append(lines, renderContextItems([]string{"Open details", "Move", "Delete", "Workflows ›"}, m.cursor)...)
	case cardContextWorkflows:
		lines[0] = lipgloss.NewStyle().Bold(true).Render("Workflows")
		switch {
		case m.loading:
			lines = append(lines, "Loading workflows…")
		case m.errorText != "":
			lines = append(lines, lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.errorText))
		case len(m.workflows) == 0:
			lines = append(lines, "No workflows accept exactly one task input.")
		default:
			for i, workflow := range m.workflows {
				label := workflow.Title
				if workflow.Description != "" {
					label += " — " + workflow.Description
				}
				lines = append(lines, contextItem(label, i == m.cursor))
			}
			if m.warningText != "" {
				lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.warningText))
			}
		}
	case cardContextForm:
		workflow := m.workflows[m.selected]
		lines[0] = lipgloss.NewStyle().Bold(true).Render(workflow.Title)
		lines = append(lines, "  "+workflow.TaskInput+": "+m.taskID+" (selected card)")
		for i, field := range m.fields {
			value := m.values[field.Key]
			if field.Secret && value != "" {
				value = strings.Repeat("•", len([]rune(value)))
			}
			if value == "" {
				value = "<enter value>"
			}
			lines = append(lines, contextItem(fmt.Sprintf("%s: %s", field.Key, value), i == m.cursor))
		}
		if m.errorText != "" {
			lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.SyncError).Render(m.errorText))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.TextTertiary).Render("↑/↓ navigate · enter select/start · esc back"))
	return RenderPanel(strings.Join(lines, "\n"), m.width, m.height)
}

func renderContextItems(items []string, cursor int) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = contextItem(item, i == cursor)
	}
	return out
}

func contextItem(label string, selected bool) string {
	prefix := "  "
	style := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	if selected {
		prefix = "> "
		style = style.Foreground(theme.TextPrimary).Bold(true)
	}
	return prefix + style.Render(label)
}
