package board

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// fakeBoardService implements the subset of BoardService needed by the board model.
type fakeBoardService struct{}

func (f *fakeBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return []service.Column{
		{Name: "Backlog", SortOrder: 0},
		{Name: "Doing", SortOrder: 1},
		{Name: "Review", SortOrder: 2},
		{Name: "Done", SortOrder: 3},
	}, nil
}

func (f *fakeBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	cards := map[string][]service.Card{
		"Backlog": {
			{ID: "REX-1", Title: "First", Priority: "High", IssueType: "Bug", Status: "Backlog"},
			{ID: "REX-2", Title: "Second", Priority: "Medium", IssueType: "Story", Status: "Backlog"},
			{ID: "REX-3", Title: "Third", Priority: "Low", IssueType: "Task", Status: "Backlog"},
		},
		"Doing": {
			{ID: "REX-4", Title: "In progress", Priority: "High", IssueType: "Bug", Status: "Doing", HasWorktree: true},
		},
		"Review": {},
		"Done": {
			{ID: "REX-5", Title: "Finished", Priority: "Low", IssueType: "Story", Status: "Done"},
		},
	}
	return cards[column], nil
}

func (f *fakeBoardService) GetCard(_ context.Context, _ string) (*service.CardDetail, error) {
	return nil, nil
}
func (f *fakeBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error { return nil }
func (f *fakeBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
}
func (f *fakeBoardService) DeleteTask(_ context.Context, _ string) error { return nil }
func (f *fakeBoardService) CreateTask(_ context.Context, _, _, _, _ string, _ *int) (*service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) UpdateTaskDescription(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeBoardService) UpdateTaskTitle(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeBoardService) ListCardsByWorkspace(_ context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return f.ListCards(context.Background(), column)
}
func (f *fakeBoardService) UpdateTaskWorkspace(_ context.Context, _ string, _ *int) error {
	return nil
}
func (f *fakeBoardService) ListWorkspaces(_ context.Context) ([]service.Workspace, error) {
	return nil, nil
}
func (f *fakeBoardService) ArchiveDoneCards(_ context.Context) (int, error) { return 0, nil }
func (f *fakeBoardService) ArchiveTask(_ context.Context, _ string) error   { return nil }
func (f *fakeBoardService) CountDoneCards(_ context.Context) (int, error)   { return 0, nil }

func newTestModel() Model {
	m := New(&fakeBoardService{}, theme.NewIcons("unicode"))
	// Simulate Init loading data
	m = m.loadData()
	m.width = 120
	m.height = 40
	m.maxVisible = 100 // large enough that all cards are visible for existing tests
	return m
}

func TestLoadDataPreservesWorktreeIndicator(t *testing.T) {
	m := newTestModel()
	cards := m.cards["Doing"]
	if len(cards) != 1 || !cards[0].HasWorktree {
		t.Fatalf("doing cards = %+v, want worktree indicator", cards)
	}
	if view := m.View(); !strings.Contains(view, m.icons.Worktree) {
		t.Fatalf("board does not render worktree icon %q: %s", m.icons.Worktree, view)
	}
}

func TestInitialCursorPosition(t *testing.T) {
	m := newTestModel()
	if m.cursorCol != 0 {
		t.Errorf("initial cursorCol = %d, want 0", m.cursorCol)
	}
	if m.cursorRow != 0 {
		t.Errorf("initial cursorRow = %d, want 0", m.cursorRow)
	}
}

func TestNavigateRight(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.cursorCol != 1 {
		t.Errorf("after l: cursorCol = %d, want 1", m.cursorCol)
	}
}

func TestNavigateLeft(t *testing.T) {
	m := newTestModel()
	m.cursorCol = 1
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if m.cursorCol != 0 {
		t.Errorf("after h from col 1: cursorCol = %d, want 0", m.cursorCol)
	}
}

func TestNavigateLeftAtBoundary(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if m.cursorCol != 0 {
		t.Errorf("h at col 0 should stay at 0, got %d", m.cursorCol)
	}
}

func TestNavigateRightAtBoundary(t *testing.T) {
	m := newTestModel()
	m.cursorCol = 3 // last column (Done)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.cursorCol != 3 {
		t.Errorf("l at last col should stay, got %d", m.cursorCol)
	}
}

func TestNavigateDown(t *testing.T) {
	m := newTestModel()
	// Backlog has 3 cards, start at row 0
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 1 {
		t.Errorf("after j: cursorRow = %d, want 1", m.cursorRow)
	}
}

func TestNavigateUp(t *testing.T) {
	m := newTestModel()
	m.cursorRow = 2
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursorRow != 1 {
		t.Errorf("after k from row 2: cursorRow = %d, want 1", m.cursorRow)
	}
}

func TestNavigateDownAtBoundary(t *testing.T) {
	m := newTestModel()
	m.cursorRow = 2 // last card in Backlog
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 2 {
		t.Errorf("j at last row should stay, got %d", m.cursorRow)
	}
}

func TestNavigateUpAtBoundary(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursorRow != 0 {
		t.Errorf("k at row 0 should stay, got %d", m.cursorRow)
	}
}

func TestJumpToFirst(t *testing.T) {
	m := newTestModel()
	m.cursorRow = 2
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.cursorRow != 0 {
		t.Errorf("g should jump to first, got row %d", m.cursorRow)
	}
}

func TestJumpToLast(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.cursorRow != 2 {
		t.Errorf("G should jump to last (2), got row %d", m.cursorRow)
	}
}

func TestColumnJumpByNumber(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if m.cursorCol != 2 {
		t.Errorf("pressing 3 should go to col 2, got %d", m.cursorCol)
	}
}

func TestColumnJumpBeyondCount(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	// 4 columns, pressing 5 should stay
	if m.cursorCol != 0 {
		t.Errorf("pressing 5 with 4 cols should stay at 0, got %d", m.cursorCol)
	}
}

func TestCursorClampOnColumnChange(t *testing.T) {
	m := newTestModel()
	m.cursorRow = 2 // row 2 in Backlog (3 cards)
	// Move to Doing (1 card) — should clamp to row 0
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.cursorCol != 1 {
		t.Errorf("should be in col 1, got %d", m.cursorCol)
	}
	if m.cursorRow != 0 {
		t.Errorf("row should clamp to 0 (Doing has 1 card), got %d", m.cursorRow)
	}
}

func TestEmptyColumnNavigation(t *testing.T) {
	m := newTestModel()
	m.cursorCol = 2 // Review (empty)
	m.cursorRow = 0
	// j/k/g/G should have no effect in empty column
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 0 {
		t.Errorf("j in empty column should stay at 0, got %d", m.cursorRow)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.cursorRow != 0 {
		t.Errorf("G in empty column should stay at 0, got %d", m.cursorRow)
	}
}

func TestWindowResize(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	if m.width != 200 {
		t.Errorf("width = %d, want 200", m.width)
	}
	if m.height != 50 {
		t.Errorf("height = %d, want 50", m.height)
	}
}

func TestNavigateToMovesToCard(t *testing.T) {
	m := newTestModel()
	// REX-4 is in Doing (col 1), row 0
	m.NavigateTo("REX-4")
	if m.cursorCol != 1 {
		t.Errorf("cursorCol = %d, want 1 (Doing)", m.cursorCol)
	}
	if m.cursorRow != 0 {
		t.Errorf("cursorRow = %d, want 0", m.cursorRow)
	}
}

func TestNavigateToSecondCardInColumn(t *testing.T) {
	m := newTestModel()
	// REX-2 is in Backlog (col 0), row 1
	m.NavigateTo("REX-2")
	if m.cursorCol != 0 {
		t.Errorf("cursorCol = %d, want 0", m.cursorCol)
	}
	if m.cursorRow != 1 {
		t.Errorf("cursorRow = %d, want 1", m.cursorRow)
	}
}

func TestNavigateToUnknownCardNoChange(t *testing.T) {
	m := newTestModel()
	m.cursorCol = 2
	m.cursorRow = 0
	m.NavigateTo("UNKNOWN-99")
	if m.cursorCol != 2 {
		t.Errorf("cursorCol should stay at 2, got %d", m.cursorCol)
	}
}

func TestDeleteKeyEmitsOpenDeleteMsg(t *testing.T) {
	m := newTestModel()
	// Cursor is on REX-1 in Backlog
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd == nil {
		t.Fatal("expected cmd from d key")
	}
	msg := cmd()
	result, ok := msg.(OpenDeleteMsg)
	if !ok {
		t.Fatalf("expected OpenDeleteMsg, got %T", msg)
	}
	if result.CardKey != "REX-1" {
		t.Errorf("cardKey = %q, want REX-1", result.CardKey)
	}
}

func TestDeleteKeyNoCardNoop(t *testing.T) {
	m := newTestModel()
	m.cursorCol = 2 // Review (empty)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Error("d key in empty column should not produce a cmd")
	}
}

func TestImportKeyEmitsOpenImportMsg(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("expected cmd from i key")
	}
	msg := cmd()
	if _, ok := msg.(OpenImportMsg); !ok {
		t.Fatalf("expected OpenImportMsg, got %T", msg)
	}
}

func TestViewNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view == "" {
		t.Error("view should not be empty")
	}
}

func TestViewHighlightsSelectedCardWithoutScrolling(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 100

	view := m.View()
	if !hasSelectedFrame(view) {
		t.Errorf("view should include selected-card styling when active column is not windowed, got: %q", view)
	}
}

func TestViewHighlightsSelectedCardWithScrolling(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 2
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})

	view := m.View()
	if !hasSelectedFrame(view) {
		t.Errorf("view should include selected-card styling when active column is windowed, got: %q", view)
	}
}

func TestVerticalScrollScrollingDown(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 2 // small viewport
	// Backlog has 3 cards. With maxVisible=2, only cards [0:2] or [1:3] are visible.
	// Start at row 0.
	if m.cursorRow != 0 {
		t.Fatalf("start row = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Fatalf("start offset = %d, want 0", m.rowOffset)
	}

	// Move cursor to row 1 — stays within visible window, no scroll needed
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 1 {
		t.Errorf("after j: cursorRow = %d, want 1", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("after j: rowOffset = %d, want 0", m.rowOffset)
	}

	// Move cursor to row 2 — falls outside visible window, view scrolls
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 2 {
		t.Errorf("after second j: cursorRow = %d, want 2", m.cursorRow)
	}
	if m.rowOffset != 1 {
		t.Errorf("after second j: rowOffset = %d, want 1", m.rowOffset)
	}

	// Verify that View() excludes scrolled-off cards
	view := m.View()
	if strings.Contains(view, "REX-1") {
		t.Error("view should not contain first card (REX-1) after scrolling down")
	}
	if !strings.Contains(view, "REX-2") {
		t.Error("view should contain second card (REX-2)")
	}
	if !strings.Contains(view, "REX-3") {
		t.Error("view should contain third card (REX-3)")
	}
}

func TestVerticalScrollScrollingUp(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 2
	// Start scrolled to bottom: window shows cards [1,2], cursor at row 2
	m.cursorRow = 2
	m.rowOffset = 1

	// First k moves cursor to row 1 — still inside the visible window,
	// so the viewport does NOT scroll yet.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursorRow != 1 {
		t.Errorf("after k: cursorRow = %d, want 1", m.cursorRow)
	}
	if m.rowOffset != 1 {
		t.Errorf("after k: rowOffset = %d, want 1 (still inside window)", m.rowOffset)
	}

	// Second k would move cursor to row 0, which is outside the visible
	// window [1,2], so the viewport scrolls up.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursorRow != 0 {
		t.Errorf("after second k: cursorRow = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("after second k: rowOffset = %d, want 0", m.rowOffset)
	}
}

func TestVerticalScrollJumpToLast(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 2
	// G jumps to last card (row 2) and scrolls to show it at bottom of window
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.cursorRow != 2 {
		t.Errorf("after G: cursorRow = %d, want 2", m.cursorRow)
	}
	if m.rowOffset != 1 {
		t.Errorf("after G: rowOffset = %d, want 1", m.rowOffset)
	}
}

func TestVerticalScrollJumpToFirst(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 2
	m.cursorRow = 2
	m.rowOffset = 1

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.cursorRow != 0 {
		t.Errorf("after g: cursorRow = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("after g: rowOffset = %d, want 0", m.rowOffset)
	}
}

func TestVerticalScrollColumnChangeResetsOffset(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 1
	m.cursorRow = 2
	m.rowOffset = 2

	// Move right to Doing (1 card) — clampRow() should set everything to 0
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.cursorCol != 1 {
		t.Errorf("after l: cursorCol = %d, want 1", m.cursorCol)
	}
	if m.cursorRow != 0 {
		t.Errorf("after l: cursorRow = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("after l: rowOffset = %d, want 0", m.rowOffset)
	}
}

func TestVerticalScrollResetOnDataLoad(t *testing.T) {
	m := newTestModel()
	// Use a short terminal so not all Backlog cards fit.
	// This makes computeMaxVisible() run on real cards instead of the fallback.
	m.height = 5
	m, _ = m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	if m.maxVisible == 0 {
		t.Fatal("maxVisible should be computed")
	}

	// Scroll to the last card — with a small viewport this also scrolls the column.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.cursorRow != 2 {
		t.Fatalf("cursorRow = %d, want 2", m.cursorRow)
	}
	origOffset := m.rowOffset

	// Re-loading same data should preserve scroll position.
	m, _ = m.Update(DataLoadedMsg{columns: m.columns, cards: m.cards})
	if m.cursorRow != 2 {
		t.Errorf("after data load: cursorRow = %d, want 2", m.cursorRow)
	}
	if m.rowOffset != origOffset {
		t.Errorf("after data load: rowOffset = %d, want %d", m.rowOffset, origOffset)
	}

	// Now empty the active column: row should clamp to 0, offset should reset.
	m.cards["Backlog"] = nil
	m, _ = m.Update(DataLoadedMsg{columns: m.columns, cards: m.cards})
	if m.cursorRow != 0 {
		t.Errorf("after empty column: cursorRow = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("after empty column: rowOffset = %d, want 0", m.rowOffset)
	}
}

func TestVerticalScrollSingleCard(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 1
	m.cursorCol = 1 // Doing column has 1 card
	m.cursorRow = 0
	m.rowOffset = 0

	// j should not move past the single card
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursorRow != 0 {
		t.Errorf("j in single-card column: cursorRow = %d, want 0", m.cursorRow)
	}
	if m.rowOffset != 0 {
		t.Errorf("j in single-card column: rowOffset = %d, want 0", m.rowOffset)
	}
}

func TestWindowSizeSetsMaxVisible(t *testing.T) {
	m := newTestModel()
	m.maxVisible = 100 // start large
	m.height = 5       // short terminal — only 1 card fits after header
	m, _ = m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	if m.maxVisible != 1 {
		t.Fatalf("maxVisible = %d, want 1 (only 1 card fits in height=5)", m.maxVisible)
	}
}
