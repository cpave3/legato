package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

const tmuxMissing = "tmux not found: agents are off. Install tmux (apt install tmux, brew install tmux): agents run in tmux sessions."

func agentsOffApp(t *testing.T) App {
	t.Helper()
	app := newTestApp()
	app.SetAgentsOff(tmuxMissing)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	return app
}

func TestTheTUIWarnsAtStartThatAgentsAreOff(t *testing.T) {
	app := agentsOffApp(t)
	var warning string
	for _, msg := range messagesOf(app.Init()) {
		if w, ok := msg.(statusbar.WarningMsg); ok {
			warning = w.Text
		}
	}
	if !strings.Contains(warning, "tmux not found: agents are off") {
		t.Fatalf("no warning at start, got %q", warning)
	}
}

func refusalOf(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	for _, msg := range messagesOf(cmd) {
		if e, ok := msg.(statusbar.ErrorMsg); ok {
			return e.Text
		}
	}
	return ""
}

func TestEverySpawnSaysWhyAgentsAreOff(t *testing.T) {
	cases := map[string]tea.Msg{
		"spawn dialog": agents.OpenAgentSpawnMsg{TaskID: "REX-1", Title: "Test"},
		"spawn submit": overlay.AgentSpawnSubmitMsg{TaskID: "REX-1"},
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			_, cmd := updateApp(agentsOffApp(t), msg)
			if got := refusalOf(t, cmd); got != tmuxMissing {
				t.Fatalf("refusal = %q", got)
			}
		})
	}
	t.Run("a on the board", func(t *testing.T) {
		app := agentsOffApp(t)
		for _, msg := range messagesOf(app.board.Init()) {
			app, _ = updateApp(app, msg)
		}
		if app.board.SelectedCard() == nil {
			t.Fatal("no card selected")
		}
		_, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		if got := refusalOf(t, cmd); got != tmuxMissing {
			t.Fatalf("refusal = %q", got)
		}
	})
}
