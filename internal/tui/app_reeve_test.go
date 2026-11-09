package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeReeve struct {
	opened int
	briefs []string
}

func (f *fakeReeve) Open(context.Context, string) (string, error) {
	f.opened++
	return "rv-1", nil
}

func (f *fakeReeve) Ask(_ context.Context, brief, _ string) (string, error) {
	f.briefs = append(f.briefs, brief)
	return "rv-1", nil
}

func reeveApp(reeve *fakeReeve) App {
	app := NewApp(&fakeBoardService{}, nil, &fakeAgentService{agents: []service.AgentSession{
		{ID: 1, TaskID: "T-1", Title: "Fix the sync", AgentKind: "codex", Status: "running", Activity: "waiting"},
		{ID: 2, TaskID: "rv-1", Title: "The Reeve", AgentKind: "claude-code", Status: "running", Reeve: true},
	}}, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	app.SetReeve(reeve)
	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return updated.(App)
}

// runAll runs cmd and feeds what it gives back to the app, a few levels deep.
func runAll(app App, cmd tea.Cmd) App {
	for depth := 0; cmd != nil && depth < 4; depth++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				m := c()
				if _, tick := m.(agentTickMsg); tick {
					continue
				}
				var out tea.Cmd
				app, out = updateApp(app, m)
				next = append(next, out)
			}
			cmd = tea.Batch(next...)
			continue
		}
		app, cmd = updateApp(app, msg)
	}
	return app
}

// R opens the Reeve from anywhere: its session started or resumed, the
// agents view on it.
func TestROpensTheReeveFromTheBoard(t *testing.T) {
	reeve := &fakeReeve{}
	app := reeveApp(reeve)
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	app = runAll(app, cmd)
	if reeve.opened != 1 || app.active != viewAgents {
		t.Fatalf("opened %d, view %d", reeve.opened, app.active)
	}
	if a := app.agentView.SelectedAgent(); a == nil || a.TaskID != "rv-1" {
		t.Fatalf("selected %+v", a)
	}
}

// In the agents view R reloads the selected agent instead.
func TestRInTheAgentsViewStillReloads(t *testing.T) {
	reeve := &fakeReeve{}
	app := reeveApp(reeve)
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app = runAll(app, cmd)
	_, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if reeve.opened != 0 {
		t.Fatal("R in the agents view opened the Reeve")
	}
	if cmd == nil {
		t.Fatal("R in the agents view did nothing")
	}
	if _, ok := cmd().(agents.ReloadAgentMsg); !ok {
		t.Fatal("R in the agents view did not reload")
	}
}

// a asks the Reeve about an agent with a brief: what it is, its IDs, its
// state.
func TestAskingTheReeveAboutAnAgentGivesItABrief(t *testing.T) {
	reeve := &fakeReeve{}
	app := reeveApp(reeve)
	app, cmd := updateApp(app, agents.AskReeveMsg{TaskID: "T-1", Title: "Fix the sync", AgentKind: "codex", State: "needs you"})
	app = runAll(app, cmd)
	if len(reeve.briefs) != 1 {
		t.Fatalf("briefs %v", reeve.briefs)
	}
	for _, want := range []string{"the agent “Fix the sync”", "- task: T-1", "- agent tool: codex", "needs you"} {
		if !strings.Contains(reeve.briefs[0], want) {
			t.Errorf("brief lacks %q:\n%s", want, reeve.briefs[0])
		}
	}
	if app.active != viewAgents {
		t.Fatalf("view %d", app.active)
	}
}
