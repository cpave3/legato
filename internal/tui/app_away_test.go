package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/theme"
)

const awayRefusal = "AWAY-1 is away on vm-1: return or recall it from the web UI"

// awayBoard is a board whose one Backlog task, AWAY-1, is away on the
// worker vm-1. Its service refuses every change to it as the real one does,
// and counts the changes it was asked for.
type awayBoard struct {
	fakeBoardService
	changes  []string
	returned bool // the task has come back from the worker
}

func (f *awayBoard) ListCards(_ context.Context, column string) ([]service.Card, error) {
	if column != "Backlog" {
		return nil, nil
	}
	card := service.Card{ID: "AWAY-1", Title: "Away task", Status: "Backlog"}
	if !f.returned {
		card.Away = &service.AwayTask{TaskID: "AWAY-1", WorkerName: "vm-1", State: "in_progress", LeftColumn: "Backlog"}
	}
	return []service.Card{card}, nil
}

func (f *awayBoard) ListCardsByWorkspace(ctx context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return f.ListCards(ctx, column)
}

func (f *awayBoard) refuse(change, id string) error {
	f.changes = append(f.changes, change)
	return &service.TaskAwayError{TaskID: id, Worker: "vm-1"}
}

func (f *awayBoard) MoveCard(_ context.Context, id, _ string) error { return f.refuse("move", id) }
func (f *awayBoard) DeleteTask(_ context.Context, id string) error  { return f.refuse("delete", id) }
func (f *awayBoard) UpdateTaskTitle(_ context.Context, id, _ string) error {
	return f.refuse("title", id)
}
func (f *awayBoard) UpdateTaskDescription(_ context.Context, id, _ string) error {
	return f.refuse("description", id)
}
func (f *awayBoard) UpdateTaskWorkspace(_ context.Context, id string, _ *int) error {
	return f.refuse("workspace", id)
}

func newAwayApp(t *testing.T) (App, *awayBoard) {
	t.Helper()
	svc := &awayBoard{}
	app := NewApp(svc, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "vi", nil, nil, "", &fakeSwarmService{}, nil)
	app.SetWorktreeService(&fakeWorktreeService{})
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 160, Height: 40})
	app, _ = updateApp(app, app.board.Init()())
	if card := app.board.SelectedCard(); card == nil || card.Key != "AWAY-1" {
		t.Fatalf("selected card = %+v, want AWAY-1", card)
	}
	return app, svc
}

// pressKey presses a key and delivers the message its command answers at
// once, as a page's key asks the app to open an overlay; a command that
// waits, such as the status bar's timer, is left alone.
func pressKey(app App, r rune) App {
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	if msg := immediate(cmd); msg != nil {
		app, _ = updateApp(app, msg)
	}
	return app
}

func immediate(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	select {
	case msg := <-answer:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func TestAwayCardIsShownOnTheBoardWithItsWorker(t *testing.T) {
	app, _ := newAwayApp(t)
	if view := app.View(); !strings.Contains(view, "away on vm-1") {
		t.Fatalf("the board should mark the away card, got:\n%s", view)
	}
}

func TestChangingAnAwayCardFromTheBoardIsRefusedInTheStatusBar(t *testing.T) {
	for _, key := range []struct {
		name string
		r    rune
	}{
		{"move", 'm'},
		{"move workspace", 'v'},
		{"delete", 'd'},
		{"spawn agent", 'a'},
		{"terminal", 't'},
		{"start swarm", 'S'},
		{"worktree", 'b'},
	} {
		t.Run(key.name, func(t *testing.T) {
			app, svc := newAwayApp(t)
			app = pressKey(app, key.r)
			if app.overlayType != overlayNone {
				t.Fatalf("an overlay opened for an away card: %v", app.overlayType)
			}
			if app.active != viewBoard {
				t.Fatalf("the view changed to %v", app.active)
			}
			if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
				t.Fatalf("status bar = %q, want %q", bar, awayRefusal)
			}
			if len(svc.changes) != 0 {
				t.Fatalf("the service was asked for %v", svc.changes)
			}
		})
	}
}

func TestContextMenuOfAnAwayCardRefusesMoveDeleteAndWorkflows(t *testing.T) {
	for _, msg := range []tea.Msg{
		overlay.CardContextActionMsg{TaskID: "AWAY-1", Action: overlay.CardContextMove},
		overlay.CardContextActionMsg{TaskID: "AWAY-1", Action: overlay.CardContextDelete},
		overlay.CardContextLoadWorkflowsMsg{TaskID: "AWAY-1"},
	} {
		app, _ := newAwayApp(t)
		app = pressKey(app, 'c')
		if app.overlayType != overlayCardContext {
			t.Fatalf("c should open the context menu of an away card, got %v", app.overlayType)
		}
		app, _ = updateApp(app, msg)
		if app.overlayType != overlayNone {
			t.Fatalf("%T: overlay %v is still open", msg, app.overlayType)
		}
		if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
			t.Fatalf("%T: status bar = %q", msg, bar)
		}
	}
}

func TestDetailOfAnAwayCardShowsItsWorkerAndStateAndRefusesChanges(t *testing.T) {
	for _, r := range []rune{'e', 't', 'm', 'v', 'D'} {
		app, svc := newAwayApp(t)
		app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
		app, _ = updateApp(app, cmd())
		if app.active != viewDetail {
			t.Fatalf("enter should open the detail view, got %v", app.active)
		}
		view := app.View()
		for _, want := range []string{"vm-1", "in progress"} {
			if !strings.Contains(view, want) {
				t.Fatalf("the detail view should show %q, got:\n%s", want, view)
			}
		}

		app = pressKey(app, r)
		if app.overlayType != overlayNone {
			t.Fatalf("%c: an overlay opened for an away card: %v", r, app.overlayType)
		}
		if view := app.View(); !strings.Contains(view, awayRefusal) {
			t.Fatalf("%c: the detail view should say %q, got:\n%s", r, awayRefusal, view)
		}
		if len(svc.changes) != 0 {
			t.Fatalf("%c: the service was asked for %v", r, svc.changes)
		}
	}
}

func TestServiceRefusalOfAnAwayTaskIsShownPlainly(t *testing.T) {
	app, _ := newAwayApp(t)
	app.board = board.New(&fakeBoardService{}, theme.NewIcons("unicode")) // the board has not heard yet
	app, _ = updateApp(app, overlay.MoveSelectedMsg{TaskID: "AWAY-1", TargetColumn: "Doing"})
	if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
		t.Fatalf("after a refused move the status bar = %q", bar)
	}

	app, _ = updateApp(app, overlay.TitleEditSubmitMsg{TaskID: "AWAY-1", Title: "New"})
	if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
		t.Fatalf("after a refused title edit the status bar = %q", bar)
	}

	app, _ = updateApp(app, overlay.DeleteConfirmedMsg{TaskID: "AWAY-1"})
	if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
		t.Fatalf("after a refused delete the status bar = %q", bar)
	}
}

func TestOpenDetailFollowsItsTaskBackFromTheWorker(t *testing.T) {
	app, svc := newAwayApp(t)
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	app, _ = updateApp(app, cmd())

	svc.returned = true
	app, _ = updateApp(app, app.board.Init()())
	if view := app.View(); strings.Contains(view, "vm-1") {
		t.Fatalf("the detail view still says the task is away:\n%s", view)
	}
	app = pressKey(app, 'm')
	if app.overlayType != overlayMove {
		t.Fatalf("a task that came back can be moved again, got overlay %v", app.overlayType)
	}
}
