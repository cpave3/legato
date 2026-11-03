package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/theme"
)

// With the sandbox on in the configuration, the agents view marks an agent
// that runs outside it.
func TestTheAgentsViewMarksAnAgentOutsideTheSandboxWhenTheSandboxIsOn(t *testing.T) {
	outside := []service.AgentSession{{ID: 1, TaskID: "task-1", TmuxSession: "legato-task-1", Status: "running", AgentKind: "codex"}}
	app := NewApp(&fakeBoardService{}, nil, &fakeAgentService{agents: outside}, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	app.SetSandboxEnabled(true)

	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 140, Height: 40})
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app, _ = updateApp(app, agents.AgentsRefreshedMsg{Agents: outside})

	if view := app.View(); !strings.Contains(view, "task-1 "+theme.NewIcons("unicode").Unsandboxed) {
		t.Errorf("the agents view should mark the agent outside the sandbox: %q", view)
	}
}
