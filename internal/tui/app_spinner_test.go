package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/theme"
)

// boardWithAgent is an app showing a board whose one card has an agent in
// the given activity.
func boardWithAgent(t *testing.T, activity string) (App, tea.Cmd) {
	t.Helper()
	agentSvc := &fakeAgentService{agents: []service.AgentSession{{TaskID: "REX-1", Status: "running", Activity: activity}}}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	var loaded tea.Msg
	for _, msg := range messagesOf(model.(App).board.Init()) {
		if _, ok := msg.(board.DataLoadedMsg); ok {
			loaded = msg
		}
	}
	model, cmd := model.(App).Update(loaded)
	return model.(App), cmd
}

func hasSpinnerTick(cmd tea.Cmd) bool {
	for _, msg := range messagesOf(cmd) {
		if _, ok := msg.(spinnerTickMsg); ok {
			return true
		}
	}
	return false
}

func TestTheSpinnerTurnsWhileAnAgentOnTheBoardRuns(t *testing.T) {
	app, cmd := boardWithAgent(t, "working")
	if !hasSpinnerTick(cmd) {
		t.Fatal("a running agent came into view: the spinner should start")
	}
	before := app.View()

	model, next := app.Update(spinnerTickMsg{})

	if model.(App).View() == before {
		t.Error("a tick should turn the spinner")
	}
	if !hasSpinnerTick(next) {
		t.Error("the spinner should keep turning while the agent runs")
	}
}

func TestTheSpinnerIsStartedOnlyOnce(t *testing.T) {
	app, _ := boardWithAgent(t, "working")

	_, cmd := app.Update(tea.WindowSizeMsg{Width: 150, Height: 40})

	if hasSpinnerTick(cmd) {
		t.Error("the spinner already turns; a second one would turn it twice as fast")
	}
}

func TestTheSpinnerRestsWhenNothingRuns(t *testing.T) {
	app, cmd := boardWithAgent(t, "waiting")
	if hasSpinnerTick(cmd) {
		t.Fatal("no agent runs: nothing should tick")
	}
	before := app.View()

	model, next := app.Update(spinnerTickMsg{})

	if model.(App).View() != before || hasSpinnerTick(next) {
		t.Error("a late tick should change nothing and ask for no more")
	}
}
