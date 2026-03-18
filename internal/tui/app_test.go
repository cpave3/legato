package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

type fakeBoardService struct{}

func (f *fakeBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return []service.Column{
		{Name: "Backlog", SortOrder: 0},
		{Name: "Doing", SortOrder: 1},
	}, nil
}

func (f *fakeBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	if column == "Backlog" {
		return []service.Card{
			{ID: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug", Status: "Backlog"},
		}, nil
	}
	return nil, nil
}

func (f *fakeBoardService) GetCard(_ context.Context, _ string) (*service.CardDetail, error) {
	return nil, nil
}
func (f *fakeBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error  { return nil }
func (f *fakeBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
}

func newTestApp() App {
	return NewApp(&fakeBoardService{}, nil)
}

func updateApp(a App, msg tea.Msg) (App, tea.Cmd) {
	m, cmd := a.Update(msg)
	return m.(App), cmd
}

func TestQuitOnQ(t *testing.T) {
	app := newTestApp()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q should return tea.Quit, got %T", msg)
	}
}

func TestWindowResizePropagation(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	if app.width != 100 {
		t.Errorf("width = %d, want 100", app.width)
	}
	if app.height != 30 {
		t.Errorf("height = %d, want 30", app.height)
	}
}

func TestViewComposition(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	view := app.View()
	if view == "" {
		t.Error("view should not be empty")
	}
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Errorf("view should have multiple lines, got %d", len(lines))
	}
}

func TestNavigationDelegatedToBoard(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, key := range []rune{'h', 'j', 'k', 'l'} {
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil {
			msg := cmd()
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Errorf("key %c should not quit", key)
			}
		}
	}
}
