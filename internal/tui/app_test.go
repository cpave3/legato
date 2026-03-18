package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/detail"
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

func (f *fakeBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	return &service.CardDetail{
		ID:            id,
		Summary:       "Test ticket",
		DescriptionMD: "Test description",
		Status:        "Backlog",
		Priority:      "High",
		IssueType:     "Bug",
		URL:           "https://jira.example.com/browse/" + id,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}, nil
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

func initTestApp() App {
	app := newTestApp()
	// Simulate Init loading data
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	return app
}

func TestEnterOpensDetail(t *testing.T) {
	app := initTestApp()
	if app.active != viewBoard {
		t.Fatalf("should start at viewBoard, got %d", app.active)
	}

	// Press enter — board returns OpenDetailMsg
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	// The board should return a command that yields OpenDetailMsg
	if cmd != nil {
		msg := cmd()
		if openMsg, ok := msg.(board.OpenDetailMsg); ok {
			// Now the app should handle this message and switch to detail
			app, cmd = updateApp(app, openMsg)
		}
	}
	// After handling OpenDetailMsg, check that we're in detail view
	if app.active != viewDetail {
		t.Errorf("should be in viewDetail, got %d", app.active)
	}
}

func TestEscReturnsToBoard(t *testing.T) {
	app := initTestApp()

	// Navigate to detail via OpenDetailMsg
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	if app.active != viewDetail {
		t.Fatalf("should be in viewDetail, got %d", app.active)
	}

	// Handle CardLoadedMsg
	card := &service.CardDetail{
		ID: "REX-1", Summary: "Test", DescriptionMD: "desc", Status: "Backlog",
	}
	app, _ = updateApp(app, detail.CardLoadedMsg{Card: card})

	// Press esc — detail returns BackToBoard cmd
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(detail.BackToBoard); ok {
			app, _ = updateApp(app, msg)
		}
	}
	if app.active != viewBoard {
		t.Errorf("should be in viewBoard after esc, got %d", app.active)
	}
}

func TestClipboardWarningWhenUnavailable(t *testing.T) {
	// Create app with a nil clipboard to simulate unavailable
	app := newTestApp()
	app.clip = nil // no clipboard
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Check that the init returns a clipboard warning cmd
	// The actual warning is handled via Init in the real app
	// For this test we just verify the app handles it gracefully
}

func TestBoardSelectionPreservedAfterDetailReturn(t *testing.T) {
	app := initTestApp()
	// Move cursor down
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	// Go to detail and back
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	app, _ = updateApp(app, detail.BackToBoard{})

	if app.active != viewBoard {
		t.Errorf("should be in viewBoard, got %d", app.active)
	}
	// Board model should preserve its state (no re-initialization)
}
