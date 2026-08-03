package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeTaskWorkflowLauncher struct {
	options       []service.TaskWorkflowOption
	listErr       error
	startErr      error
	startedName   string
	startedInputs map[string]string
}

func (f *fakeTaskWorkflowLauncher) List(context.Context, string) ([]service.TaskWorkflowOption, error) {
	return f.options, f.listErr
}
func (f *fakeTaskWorkflowLauncher) Start(_ context.Context, name string, inputs map[string]string) (*service.TaskWorkflowRun, error) {
	f.startedName, f.startedInputs = name, inputs
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &service.TaskWorkflowRun{ID: "wr-1", Title: "Triage Card", RelatedTaskID: inputs["subject"]}, nil
}

func TestCardContextWorkflowLaunchBindsSelectedCard(t *testing.T) {
	app := NewApp(&fakeBoardService{}, nil, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	launcher := &fakeTaskWorkflowLauncher{options: []service.TaskWorkflowOption{{
		Name: "triage", Title: "Triage", TaskInput: "subject",
		Inputs: []service.TaskWorkflowInput{{Key: "subject", Type: "task", Required: true}},
		Values: map[string]string{"subject": "REX-1"},
	}}}
	app.SetTaskWorkflowLauncher(launcher)
	loaded := app.board.Init()()
	app, _ = updateApp(app, loaded)

	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("c did not emit context message")
	}
	app, _ = updateApp(app, cmd())
	if app.overlayType != overlayCardContext {
		t.Fatalf("overlay = %v", app.overlayType)
	}

	// Workflows is the fourth item.
	for range 3 {
		app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening workflow form should not launch yet")
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())

	if launcher.startedName != "triage" || launcher.startedInputs["subject"] != "REX-1" {
		t.Fatalf("start = %q %#v", launcher.startedName, launcher.startedInputs)
	}
	if app.overlayType != overlayNone {
		t.Fatal("successful launch did not close overlay")
	}
}

func TestCardContextLaunchErrorRemainsVisible(t *testing.T) {
	model := overlay.NewCardContext("REX-1").SetWorkflows([]service.TaskWorkflowOption{{
		Name: "triage", Title: "Triage", TaskInput: "subject",
		Inputs: []service.TaskWorkflowInput{{Key: "subject", Type: "task"}, {Key: "count", Type: "integer", Required: true}},
		Values: map[string]string{"subject": "REX-1"},
	}}, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(overlay.CardContextOverlay).SetError(context.DeadlineExceeded)
	if !strings.Contains(model.View(), context.DeadlineExceeded.Error()) {
		t.Fatalf("view did not contain launch error: %s", model.View())
	}
}
