package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// conversationAgents is an agent service whose native agents can be asked
// what they said and did. An agent with no tail is driven through its
// terminal.
type conversationAgents struct {
	*fakeAgentService
	tails map[string]service.ConversationTail
}

func (c *conversationAgents) ConversationTail(_ context.Context, taskID string, _ int) (*service.ConversationTail, error) {
	tail, ok := c.tails[taskID]
	if !ok {
		return nil, service.ErrNoNativeEndpoint
	}
	return &tail, nil
}

func (c *conversationAgents) CaptureOutput(_ context.Context, taskID string) (string, error) {
	return "terminal of " + taskID, nil
}

// ensembleApp is an app showing the ensemble of twoAgents.
func ensembleApp(t *testing.T, agentSvc service.AgentService) App {
	t.Helper()
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 170, Height: 44})
	model, _ = model.(App).dispatchHotkey(hotkey.Binding{ID: hotkey.ActionViewAgents, Target: hotkey.TargetRoot}, tea.KeyMsg{})
	model, _ = model.(App).Update(agents.AgentsRefreshedMsg{Agents: twoAgents()})
	model, _ = model.(App).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	return model.(App)
}

func TestEnsembleShowsANativeAgentsConversationAndThePaneOfTheOthers(t *testing.T) {
	app := ensembleApp(t, &conversationAgents{
		fakeAgentService: &fakeAgentService{agents: twoAgents()},
		tails: map[string]service.ConversationTail{"task-1": {Entries: []service.ConversationEntry{
			{Kind: service.EntryAssistant, Text: "The flag is in."},
		}}},
	})

	model, cmd := app.Update(agentTickMsg{})
	for _, msg := range messagesOf(cmd) {
		switch msg.(type) {
		case agents.ConversationMsg, agents.PreviewMsg:
			model, _ = model.(App).Update(msg)
		}
	}

	view := model.(App).View()
	if !strings.Contains(view, "The flag is in.") {
		t.Errorf("the native agent's tile should show what it said: %q", view)
	}
	if !strings.Contains(view, "terminal of task-2") {
		t.Errorf("the other agent's tile should show its terminal: %q", view)
	}
	if strings.Contains(view, "terminal of task-1") {
		t.Errorf("the native agent's terminal should not be read: %q", view)
	}
}

func TestEnsembleShowsThePaneOfAnAgentWhoseConversationCannotBeRead(t *testing.T) {
	app := ensembleApp(t, &conversationAgents{fakeAgentService: &fakeAgentService{agents: twoAgents()}})

	model, cmd := app.Update(agentTickMsg{})
	for _, msg := range messagesOf(cmd) {
		if preview, ok := msg.(agents.PreviewMsg); ok {
			model, _ = model.(App).Update(preview)
		}
	}

	if view := model.(App).View(); !strings.Contains(view, "terminal of task-1") || !strings.Contains(view, "terminal of task-2") {
		t.Errorf("each tile should show its agent's terminal: %q", view)
	}
}
