package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeEngramPicker struct {
	asked      []service.EngramPickerRequest
	remembered []string
	rememberAt service.EngramPickerRequest
}

func (f *fakeEngramPicker) Picker(_ context.Context, req service.EngramPickerRequest) (service.EngramPicker, error) {
	f.asked = append(f.asked, req)
	return service.EngramPicker{
		EngramStatus: service.EngramStatus{Enabled: true},
		Place:        &service.EngramPlace{Key: "repo:github.com/cpave3/legato", Label: "github.com/cpave3/legato", Kind: "repo", Default: "argus"},
		Engrams:      []service.EngramChoice{{Name: "mneme", Mark: "last"}, {Name: "argus", Mark: "default", Bound: true}},
		Preselect:    "mneme",
	}, nil
}

func (f *fakeEngramPicker) Remember(_ context.Context, req service.EngramPickerRequest, name string) error {
	f.rememberAt = req
	f.remembered = append(f.remembered, name)
	return nil
}

// warningAgents reports that the engram did not boot.
type warningAgents struct{ *fakeAgentService }

func (w warningAgents) SpawnEphemeralAgent(ctx context.Context, title string, width, height int, opts ...service.AgentSpawnOptions) error {
	if report := service.SpawnReportFromContext(ctx); report != nil {
		report.EngramWarning = "mneme did not boot, so the agent started without it: disk is full"
	}
	return w.fakeAgentService.SpawnEphemeralAgent(ctx, title, width, height, opts...)
}

func openEngramSpawn(t *testing.T, agentSvc service.AgentService, engrams *fakeEngramPicker) App {
	t.Helper()
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/home/me/Projects/legato", nil, nil)
	app.SetEngramService(engrams)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 110, Height: 40})
	app, cmd := updateApp(app, agents.OpenAgentSpawnMsg{})
	if cmd == nil {
		t.Fatal("opening the overlay did not ask for the engrams")
	}
	app, _ = updateApp(app, cmd())
	return app
}

func TestTheSpawnOverlayStartsTheAgentAsTheEngramPicked(t *testing.T) {
	agentSvc := &fakeAgentService{captureErr: true, defaultAdapter: "claude-code"}
	engrams := &fakeEngramPicker{}
	app := openEngramSpawn(t, agentSvc, engrams)
	if len(engrams.asked) != 1 || engrams.asked[0].Dir != "/home/me/Projects/legato" {
		t.Fatalf("asked %+v", engrams.asked)
	}
	if view := app.View(); !strings.Contains(view, "‹ mneme · last on this task ›") {
		t.Fatalf("view:\n%s", view)
	}
	// title → agent → engram, then remember.
	app = spawnOverlayKeys(t, app, tea.KeyMsg{Type: tea.KeyTab}, tea.KeyMsg{Type: tea.KeyTab}, typed("r"))
	spawnWithEnter(t, app)
	if agentSvc.lastSpawnOpts == nil || agentSvc.lastSpawnOpts.Engram != "mneme" {
		t.Fatalf("spawn options = %+v", agentSvc.lastSpawnOpts)
	}
	if len(engrams.remembered) != 1 || engrams.remembered[0] != "mneme" || engrams.rememberAt.Dir != "/home/me/Projects/legato" {
		t.Fatalf("remembered %v at %+v", engrams.remembered, engrams.rememberAt)
	}
}

func TestTheStatusBarSaysWhenTheEngramDidNotBoot(t *testing.T) {
	agentSvc := warningAgents{&fakeAgentService{captureErr: true, defaultAdapter: "claude-code"}}
	app := openEngramSpawn(t, agentSvc, &fakeEngramPicker{})
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	var warned bool
	var run func(msg tea.Msg)
	run = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, each := range msg {
				if each != nil {
					run(each())
				}
			}
		case statusbar.WarningMsg:
			warned = strings.Contains(msg.Text, "disk is full")
		}
	}
	run(cmd())
	if !warned {
		t.Fatal("the warning did not reach the status bar")
	}
}

func TestATypedPathGetsTheEngramsOfTheDirectoryItNames(t *testing.T) {
	agentSvc := &fakeAgentService{captureErr: true, defaultAdapter: "claude-code"}
	engrams := &fakeEngramPicker{}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/home/me/Projects/legato", nil, nil)
	app.SetDirectoryService(&fakeDirectoryService{})
	app.SetEngramService(engrams)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 110, Height: 40})
	answer := app.spawnEngrams("~/Projects/legato", "")().(overlay.AgentSpawnEngramsMsg)
	if answer.Dir != "~/Projects/legato" || engrams.asked[0].Dir != "/home/me/Projects/legato" {
		t.Fatalf("answer for %q, asked %+v", answer.Dir, engrams.asked)
	}
}
