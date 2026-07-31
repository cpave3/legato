package attention

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/hotkey"
)

type Service interface {
	List(ctx context.Context, filter service.AttentionFilter) ([]service.AttentionItem, error)
	Execute(ctx context.Context, id string, request service.AttentionActionRequest) error
	Dismiss(ctx context.Context, id, sourceRevision string) error
}

type queueMsg struct {
	items []service.AttentionItem
	err   error
}
type actionMsg struct {
	err error
}
type ChangedMsg struct{}
type ReturnToBoardMsg struct{}

type Model struct {
	svc           Service
	items         []service.AttentionItem
	cursor        int
	err           error
	info          string
	width, height int
}

func New(svc Service) Model { return Model{svc: svc} }

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m Model) Init() tea.Cmd { return m.loadQueue() }

func (m Model) loadQueue() tea.Cmd {
	if m.svc == nil {
		return nil
	}
	return func() tea.Msg {
		items, err := m.svc.List(context.Background(), service.AttentionFilter{})
		return queueMsg{items: items, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(x.Width, x.Height)
	case queueMsg:
		m.err = x.err
		if x.err == nil {
			m.items = x.items
			if m.cursor >= len(m.items) {
				m.cursor = max(0, len(m.items)-1)
			}
		}
	case actionMsg:
		m.err = x.err
		if x.err == nil {
			m.info = "Done"
			return m, m.loadQueue()
		}
	case ChangedMsg:
		return m, m.loadQueue()
	case tea.KeyMsg:
		return m.key(x)
	}
	return m, nil
}

func (m Model) key(key tea.KeyMsg) (Model, tea.Cmd) {
	binding, ok := hotkey.DefaultRegistry().Resolve(key, hotkey.Context{Page: hotkey.PageAttention})
	if !ok || binding.Target != hotkey.TargetPage {
		return m, nil
	}
	return m.HandleAction(binding.ID)
}

// HandleAction executes a registry action owned by the attention model.
func (m Model) HandleAction(action hotkey.ActionID) (Model, tea.Cmd) {
	switch action {
	case hotkey.ActionAttentionDown:
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case hotkey.ActionAttentionUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case hotkey.ActionAttentionExecute:
		if len(m.items) > 0 {
			item := m.items[m.cursor]
			for _, action := range item.Actions {
				if action.Kind == "execute" {
					return m, m.executeAction(item, action)
				}
			}
		}
	case hotkey.ActionAttentionDismiss:
		if len(m.items) > 0 {
			item := m.items[m.cursor]
			return m, m.dismiss(item)
		}
	case hotkey.ActionBack:
		return m, func() tea.Msg { return ReturnToBoardMsg{} }
	}
	return m, nil
}

func (m Model) executeAction(item service.AttentionItem, action service.AttentionAction) tea.Cmd {
	return func() tea.Msg {
		req := service.AttentionActionRequest{
			ActionID: action.ID, ExpectedRevision: item.SourceRevision,
			Confirmed: action.RequiresConfirm, Text: "",
		}
		return actionMsg{err: m.svc.Execute(context.Background(), item.ID, req)}
	}
}

func (m Model) dismiss(item service.AttentionItem) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{err: m.svc.Dismiss(context.Background(), item.ID, item.SourceRevision)}
	}
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to return.", m.err)
	}
	if len(m.items) == 0 {
		return "No items need attention.\n\nPress q to return."
	}
	var b []byte
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}
		age := formatRelativeTime(item.UpdatedAt)
		line := fmt.Sprintf("%s%s — %s [%s/%s] %s\n", cursor, item.Title, item.Reason, item.Producer, item.Urgency, age)
		b = append(b, line...)
	}
	if m.info != "" {
		b = append(b, []byte("\n"+m.info)...)
	}
	return string(b)
}

func formatRelativeTime(iso string) string {
	t, err := time.Parse("2006-01-02 15:04:05", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return ""
		}
	}
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return "just now"
	}
}
