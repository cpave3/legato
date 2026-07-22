package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

type Service interface {
	Queue(context.Context) ([]service.PlanQueueItem, error)
	Plan(context.Context, string) (*service.PlanView, error)
	Respond(context.Context, string, string, service.PlanResponseInput) error
	AddComment(context.Context, string, service.PlanCommentInput) (*store.PlanComment, error)
	AskQuestion(context.Context, string, string) (string, error)
	RequestChanges(context.Context, string) error
	Approve(context.Context, string) error
	Reject(context.Context, string) error
	Reopen(context.Context, string) error
}

type queueMsg struct {
	items []service.PlanQueueItem
	err   error
}
type planMsg struct {
	view *service.PlanView
	err  error
}
type actionMsg struct {
	info string
	err  error
}
type ChangedMsg struct{ PlanID string }
type ReturnToBoardMsg struct{}

type mode int

const (
	queueMode mode = iota
	readerMode
)

type inputMode int

const (
	inputNone inputMode = iota
	inputAnswer
	inputComment
	inputQuestion
)

type Model struct {
	svc            Service
	mode           mode
	queue          []service.PlanQueueItem
	cursor         int
	view           *service.PlanView
	viewport       viewport.Model
	questionCursor int
	inputMode      inputMode
	input          string
	err            error
	info           string
	width, height  int
}

func New(svc Service) Model { return Model{svc: svc} }
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = max(20, w-4)
	m.viewport.Height = max(5, h-7)
}
func (m Model) Init() tea.Cmd      { return m.loadQueue() }
func (m Model) InputFocused() bool { return m.inputMode != inputNone }
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(x.Width, x.Height)
	case queueMsg:
		m.err = x.err
		if x.err == nil {
			m.queue = x.items
			if m.cursor >= len(m.queue) {
				m.cursor = max(0, len(m.queue)-1)
			}
		}
	case planMsg:
		m.err = x.err
		if x.err == nil {
			m.view = x.view
			m.mode = readerMode
			m.refresh()
		}
	case actionMsg:
		m.err = x.err
		m.info = x.info
		if x.err == nil && m.view != nil {
			return m, m.loadPlan(m.view.Plan.ID)
		}
	case ChangedMsg:
		if m.mode == readerMode && m.view != nil && x.PlanID == m.view.Plan.ID {
			return m, m.loadPlan(x.PlanID)
		}
		return m, m.loadQueue()
	case tea.KeyMsg:
		return m.key(x)
	}
	return m, nil
}
func (m Model) key(key tea.KeyMsg) (Model, tea.Cmd) {
	if m.inputMode != inputNone {
		return m.inputKey(key)
	}
	if m.mode == queueMode {
		switch key.String() {
		case "j", "down":
			if m.cursor < len(m.queue)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if len(m.queue) > 0 {
				return m, m.loadPlan(m.queue[m.cursor].PlanID)
			}
		case "esc", "q":
			return m, func() tea.Msg { return ReturnToBoardMsg{} }
		}
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.mode = queueMode
		m.view = nil
		return m, m.loadQueue()
	case "j", "down":
		m.viewport.LineDown(1)
	case "k", "up":
		m.viewport.LineUp(1)
	case "tab":
		if m.view != nil && len(m.view.Questions) > 0 {
			m.questionCursor = (m.questionCursor + 1) % len(m.view.Questions)
		}
		m.refresh()
	case "e":
		if m.view != nil && len(m.view.Questions) > 0 {
			m.inputMode = inputAnswer
			m.input = ""
		}
	case "c":
		m.inputMode = inputComment
		m.input = ""
	case "?":
		m.inputMode = inputQuestion
		m.input = ""
	case "a":
		return m, m.action("Plan approved", func(ctx context.Context, id string) error { return m.svc.Approve(ctx, id) })
	case "x":
		return m, m.action("Plan rejected", func(ctx context.Context, id string) error { return m.svc.Reject(ctx, id) })
	case "r":
		return m, m.action("Changes requested", func(ctx context.Context, id string) error { return m.svc.RequestChanges(ctx, id) })
	case "o":
		return m, m.action("Plan reopened", func(ctx context.Context, id string) error { return m.svc.Reopen(ctx, id) })
	}
	return m, nil
}
func (m Model) inputKey(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.inputMode = inputNone
		m.input = ""
		return m, nil
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil
	case "enter":
		if strings.TrimSpace(m.input) == "" {
			return m, nil
		}
		text := m.input
		kind := m.inputMode
		m.inputMode = inputNone
		m.input = ""
		if m.view == nil {
			return m, nil
		}
		id := m.view.Plan.ID
		switch kind {
		case inputComment:
			return m, func() tea.Msg {
				_, err := m.svc.AddComment(context.Background(), id, service.PlanCommentInput{Body: text})
				return actionMsg{info: "Draft comment added", err: err}
			}
		case inputQuestion:
			return m, func() tea.Msg {
				_, err := m.svc.AskQuestion(context.Background(), id, text)
				if err == service.ErrAgentOffline {
					return actionMsg{info: "Question saved; agent is offline"}
				}
				return actionMsg{info: "Question sent", err: err}
			}
		case inputAnswer:
			q := m.view.Questions[m.questionCursor]
			return m, func() tea.Msg {
				input := service.PlanResponseInput{Text: text}
				if q.Kind != "free_text" {
					for _, v := range strings.Split(text, ",") {
						input.Values = append(input.Values, strings.TrimSpace(v))
					}
					input.Text = ""
				}
				return actionMsg{info: "Answer saved", err: m.svc.Respond(context.Background(), id, q.Key, input)}
			}
		}
	default:
		if len(key.Runes) > 0 {
			m.input += string(key.Runes)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) loadQueue() tea.Cmd {
	return func() tea.Msg { items, err := m.svc.Queue(context.Background()); return queueMsg{items, err} }
}
func (m Model) loadPlan(id string) tea.Cmd {
	return func() tea.Msg { view, err := m.svc.Plan(context.Background(), id); return planMsg{view, err} }
}
func (m Model) action(info string, fn func(context.Context, string) error) tea.Cmd {
	if m.view == nil {
		return nil
	}
	id := m.view.Plan.ID
	return func() tea.Msg { return actionMsg{info: info, err: fn(context.Background(), id)} }
}
func (m *Model) refresh() {
	if m.view == nil {
		return
	}
	rendered, err := glamour.Render(m.view.Revision.Markdown, "dark")
	if err != nil {
		rendered = m.view.Revision.Markdown
	}
	var b strings.Builder
	b.WriteString(rendered)
	b.WriteString("\nCHOICES\n")
	for i, q := range m.view.Questions {
		mark := "  "
		if i == m.questionCursor {
			mark = "> "
		}
		answered := false
		for _, r := range m.view.Responses {
			if r.QuestionID == q.ID {
				answered = true
			}
		}
		fmt.Fprintf(&b, "%s%s%s: %s\n", mark, map[bool]string{true: "[x]", false: "[ ]"}[answered], q.Key, q.Prompt)
		if q.Kind != "free_text" {
			var opts []service.PlanOption
			_ = json.Unmarshal([]byte(q.OptionsJSON), &opts)
			for _, o := range opts {
				fmt.Fprintf(&b, "      %s — %s\n", o.ID, o.Label)
			}
		}
	}
	if len(m.view.Comments) > 0 {
		b.WriteString("\nCOMMENTS\n")
		for _, c := range m.view.Comments {
			fmt.Fprintf(&b, "• %s\n", c.Body)
		}
	}
	if len(m.view.Messages) > 0 {
		b.WriteString("\nQ&A\n")
		for _, msg := range m.view.Messages {
			fmt.Fprintf(&b, "%s: %s\n", msg.Author, msg.Body)
		}
	}
	m.viewport.SetContent(b.String())
}
func (m Model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentPurple)
	if m.mode == queueMode {
		var b strings.Builder
		b.WriteString(title.Render("Plan queue") + "\n\n")
		if len(m.queue) == 0 {
			b.WriteString("No plans awaiting review.\n")
		}
		for i, p := range m.queue {
			mark := "  "
			if i == m.cursor {
				mark = "> "
			}
			fmt.Fprintf(&b, "%s%s  [%s] rev %d", mark, p.Title, p.Status, p.Revision)
			if p.UnansweredRequired > 0 {
				fmt.Fprintf(&b, "  %d required", p.UnansweredRequired)
			}
			b.WriteByte('\n')
		}
		b.WriteString("\nenter open • j/k move • q board")
		return b.String()
	}
	if m.view == nil {
		return "Loading plan…"
	}
	header := fmt.Sprintf("%s  rev %d  [%s]", title.Render(m.view.Plan.Title), m.view.Plan.LatestRevision, m.view.Plan.Status)
	footer := "j/k scroll • tab choice • e answer • c comment • ? ask • a approve • r changes • x reject • q queue"
	if m.inputMode != inputNone {
		labels := map[inputMode]string{inputAnswer: "Answer (option IDs comma-separated or text)", inputComment: "Comment", inputQuestion: "Question"}
		footer = fmt.Sprintf("%s: %s█  (enter submit • esc cancel)", labels[m.inputMode], m.input)
	}
	if m.err != nil {
		footer = "Error: " + m.err.Error()
	}
	if m.info != "" {
		footer = m.info + "  •  " + footer
	}
	return header + "\n\n" + m.viewport.View() + "\n" + footer
}
