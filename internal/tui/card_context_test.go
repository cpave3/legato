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
	// reviewReason is why the card offers no review passes.
	reviewReason string
}

func (f *fakeTaskWorkflowLauncher) ReviewUnavailable(context.Context, string) string {
	return f.reviewReason
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

func TestCardContextStartsReviewPassesOnTheCardsPullRequest(t *testing.T) {
	app := NewApp(&fakeBoardService{}, nil, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	launcher := &fakeTaskWorkflowLauncher{options: []service.TaskWorkflowOption{{
		Name: "review-loop", Title: "Review passes · #12 · agent",
		Values: map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true"},
	}}}
	app.SetTaskWorkflowLauncher(launcher)
	loaded := app.board.Init()()
	app, _ = updateApp(app, loaded)
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app, _ = updateApp(app, cmd())
	for range 3 {
		app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	if view := app.activeOverlay.View(); !strings.Contains(view, "Review passes · #12 · agent") {
		t.Fatalf("the menu does not offer review passes: %s", view)
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 4 && launcher.startedName == ""; i++ {
		if cmd == nil {
			app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
			continue
		}
		app, cmd = updateApp(app, cmd())
	}
	if launcher.startedName != "review-loop" || launcher.startedInputs["agent_author"] != "true" || launcher.startedInputs["pr_number"] != "12" {
		t.Fatalf("start = %q %#v", launcher.startedName, launcher.startedInputs)
	}
}

// pickFirstWorkflow presses Enter on the first item of the Workflows list,
// and on its form, running what each emits.
func pickFirstWorkflow(app App) App {
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 6; i++ {
		if cmd == nil {
			if app.overlayType == overlayNone {
				return app
			}
			app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
			continue
		}
		app, cmd = updateApp(app, cmd())
	}
	return app
}

// A pull request a loop holds, whatever started it, opens that loop in the
// web UI instead of starting another (spec LEG-559 AC 2).
func TestCardContextOpensTheLoopThatHoldsThePullRequest(t *testing.T) {
	launcher := &fakeTaskWorkflowLauncher{options: []service.TaskWorkflowOption{{
		Name: "review-loop", Title: "Open review loop · #12",
		Loop: &service.FoundReviewLoop{LoopID: "rl-1", Kind: service.ReviewLoopKindHandoff, Link: "/board?highlight_task=t-9"},
	}}}
	app := openCardWorkflows(t, launcher)
	app.SetWebURL("http://localhost:3080")
	var opened []string
	app.openURL = func(url string) error { opened = append(opened, url); return nil }
	app = pickFirstWorkflow(app)
	if launcher.startedName != "" {
		t.Fatalf("started %q on a held pull request", launcher.startedName)
	}
	if len(opened) != 1 || opened[0] != "http://localhost:3080/board?highlight_task=t-9" || app.overlayType != overlayNone {
		t.Fatalf("opened %v, overlay %v", opened, app.overlayType)
	}
}

// A refused start names the loop that holds the pull request and links to
// it (spec LEG-559 story 8).
func TestCardContextRefusedReviewNamesTheHoldingLoopWithItsLink(t *testing.T) {
	launcher := &fakeTaskWorkflowLauncher{
		options: []service.TaskWorkflowOption{{Name: "review-loop", Title: "Review passes · #12 · agent",
			Values: map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true"}}},
		startErr: &service.WorkflowStepError{Code: service.ReviewRefusalLoopActive, Link: "/workflows/runs/wr-7",
			Message: "review_loop_active: pull request #12 of acme/widget already has an active review loop: run wr-7 (/workflows/runs/wr-7), loop rl-7"},
	}
	app := openCardWorkflows(t, launcher)
	app.SetWebURL("http://localhost:3080")
	app = pickFirstWorkflow(app)
	view := app.activeOverlay.View()
	if !strings.Contains(view, "run wr-7") || !strings.Contains(view, "Open http://localhost:3080/workflows/runs/wr-7") {
		t.Fatalf("view: %s", view)
	}
}

// openCardWorkflows opens the selected card's menu and its Workflows list.
func openCardWorkflows(t *testing.T, launcher *fakeTaskWorkflowLauncher) App {
	t.Helper()
	app := NewApp(&fakeBoardService{}, nil, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	app.SetTaskWorkflowLauncher(launcher)
	app, _ = updateApp(app, app.board.Init()())
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app, _ = updateApp(app, cmd())
	for range 3 {
		app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	app, _ = updateApp(app, cmd())
	return app
}

// With no Legato on the data folder running the review engine, the card's
// menu offers no review and says why (spec LEG-559 AC 5).
func TestCardContextSaysWhyItOffersNoReviewPasses(t *testing.T) {
	launcher := &fakeTaskWorkflowLauncher{
		options: []service.TaskWorkflowOption{{
			Name: "triage", Title: "Triage", TaskInput: "subject",
			Inputs: []service.TaskWorkflowInput{{Key: "subject", Type: "task"}},
			Values: map[string]string{"subject": "REX-1"},
		}},
		reviewReason: service.ReviewEngineStoppedReason,
	}
	app := openCardWorkflows(t, launcher)
	view := app.activeOverlay.View()
	if strings.Contains(view, "Review passes ·") || !strings.Contains(view, "Review passes need `legato serve` running.") {
		t.Fatalf("the menu does not say why: %s", view)
	}
	// The reason is no item: enter still opens the one workflow there is.
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyDown})
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, cmd = updateApp(app, cmd())
	_, _ = updateApp(app, cmd())
	if launcher.startedName != "triage" {
		t.Fatalf("start = %q", launcher.startedName)
	}
}

// With nothing else to offer, the reason stands alone.
func TestCardContextSaysWhyWhenReviewWasAllItWouldOffer(t *testing.T) {
	app := openCardWorkflows(t, &fakeTaskWorkflowLauncher{reviewReason: service.ReviewPassesOffReason})
	view := app.activeOverlay.View()
	if !strings.Contains(view, "review_passes.enabled") {
		t.Fatalf("the menu does not name the setting: %s", view)
	}
}
