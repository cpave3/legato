package board

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// fakeBoardService implements the subset of BoardService needed by the board model.
type fakeBoardService struct {
	// descriptions holds what GetCard answers, by task ID.
	descriptions map[string]string
}

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

func (f *fakeBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	description, ok := f.descriptions[id]
	if !ok {
		return nil, nil
	}
	return &service.CardDetail{ID: id, DescriptionMD: description}, nil
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
func (f *fakeBoardService) ListArchivedTasks(_ context.Context) ([]service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) RestoreTask(_ context.Context, _ string) error { return nil }
func (f *fakeBoardService) CountDoneCards(_ context.Context) (int, error) { return 0, nil }

func newTestModel() Model {
	m := New(&fakeBoardService{}, theme.NewIcons("unicode"))
	// Simulate Init loading data
	m = m.loadData()
	m.width = 120
	m.height = 40
	m.maxVisible = 100 // large enough that all cards are visible for existing tests
	return m
}

func TestWorkspaceMoveKeyEmitsSelectedTask(t *testing.T) {
	m := newTestModel()
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if cmd == nil {
		t.Fatal("v returned no command")
	}
	msg, ok := cmd().(OpenMoveWorkspaceMsg)
	if !ok || msg.CardKey != "REX-1" {
		t.Fatalf("message = %#v, want selected task REX-1", cmd())
	}
}

func TestTargetedPRKeysEmitSelectedTaskActions(t *testing.T) {
	m := newTestModel()
	for _, test := range []struct {
		key      tea.KeyType
		name     string
		wantType any
	}{{tea.KeyCtrlR, "ctrl+r", RefreshPRMsg{}}, {tea.KeyCtrlP, "ctrl+p", RediscoverPRMsg{}}} {
		_, cmd := m.handleKey(tea.KeyMsg{Type: test.key})
		if cmd == nil {
			t.Fatalf("%s returned no command", test.name)
		}
		switch test.wantType.(type) {
		case RefreshPRMsg:
			if msg, ok := cmd().(RefreshPRMsg); !ok || msg.CardKey == "" {
				t.Fatalf("%s message = %#v", test.name, cmd())
			}
		case RediscoverPRMsg:
			if msg, ok := cmd().(RediscoverPRMsg); !ok || msg.CardKey == "" {
				t.Fatalf("%s message = %#v", test.name, cmd())
			}
		}
	}
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

// sizedModel is a board that was told its size the way the app tells it.
func sizedModel(width, height int) Model {
	m := New(&fakeBoardService{descriptions: map[string]string{
		"REX-1": "## Steps\n\nOpen a listing on a phone.\nThe selector is off screen.",
	}}, theme.NewIcons("unicode"))
	m = m.loadData()
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func TestViewFillsExactlyTheSizeItWasGiven(t *testing.T) {
	for _, size := range []struct{ width, height int }{{160, 40}, {100, 30}, {60, 20}} {
		m := sizedModel(size.width, size.height)

		lines := strings.Split(m.View(), "\n")

		if len(lines) != size.height {
			t.Errorf("%dx%d: view is %d lines tall", size.width, size.height, len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != size.width {
				t.Errorf("%dx%d: line %d is %d wide: %q", size.width, size.height, i, w, line)
				break
			}
		}
	}
}

func TestViewOpensWithWhatIsOnTheBoard(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetActiveAgents(map[string]bool{"REX-4": true})

	header := strings.Split(m.View(), "\n")[0]

	for _, want := range []string{"♩ legato", "board", "5 tasks", "1 with an agent"} {
		if !strings.Contains(header, want) {
			t.Errorf("header should contain %q, got: %q", want, header)
		}
	}
}

func TestHeaderShowsTheFilterAndTheStatusItIsGiven(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetWorkspaces([]service.Workspace{{ID: 7, Name: "Work", Color: "#4A9EEF"}})
	m.SetStatus("● synced 2m ago", "web :3080")

	if header := strings.Split(m.View(), "\n")[0]; !strings.Contains(header, "all workspaces") || !strings.Contains(header, "synced 2m ago") || !strings.Contains(header, "web :3080") {
		t.Errorf("header = %q", header)
	}

	m.SetWorkspaceView(store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: 7})
	if header := strings.Split(m.View(), "\n")[0]; !strings.Contains(header, "Work") || strings.Contains(header, "all workspaces") {
		t.Errorf("header for one workspace = %q", header)
	}
}

func TestEmptyColumnsAreCollapsedUntilAsked(t *testing.T) {
	m := sizedModel(160, 40)

	collapsed := m.View()
	if strings.Contains(collapsed, "REVIEW") {
		t.Errorf("the empty Review column should be collapsed, got: %q", collapsed)
	}
	if !strings.Contains(collapsed, "hide empty: on") {
		t.Errorf("the header should say empty columns are hidden, got: %q", strings.Split(collapsed, "\n")[0])
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	open := m.View()
	if !strings.Contains(open, "REVIEW") || !strings.Contains(open, "hide empty: off") {
		t.Errorf("z should open the empty column, got: %q", open)
	}
}

func TestColumnsWithCardsGetTheRoomOfTheCollapsedOnes(t *testing.T) {
	// Too narrow for a panel, and just wide enough for four open columns.
	m := sizedModel(98, 40)
	title := "A title of twenty-one"
	m.cards["Backlog"][0].Title = title

	if !strings.Contains(m.View(), title) {
		t.Errorf("with the empty column collapsed the title should fit")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if strings.Contains(m.View(), title) {
		t.Errorf("with every column open the title should be cut")
	}
}

func TestBoardWithNothingOnItKeepsItsColumnsOpen(t *testing.T) {
	m := sizedModel(160, 40)
	for name := range m.cards {
		m.cards[name] = nil
	}

	if view := m.View(); !strings.Contains(view, "BACKLOG") || !strings.Contains(view, "REVIEW") {
		t.Errorf("an empty board should still name its columns, got: %q", view)
	}
}

func TestSelectedTaskIsDescribedBesideTheBoard(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetAgentStates(map[string]string{"REX-1": "waiting"})

	view := m.View()

	for _, want := range []string{"high · bug · REX-1", "column", "backlog", "source", "needs you", "Open a listing on a phone.", "ACTIONS", "Move task"} {
		if !strings.Contains(view, want) {
			t.Errorf("the panel should contain %q, got: %q", want, view)
		}
	}
	if strings.Contains(view, "## Steps") {
		t.Errorf("the description should be shown without its markup, got: %q", view)
	}
}

func TestPanelFollowsTheSelection(t *testing.T) {
	m := sizedModel(160, 40)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	view := m.View()

	if !strings.Contains(view, "medium · story · REX-2") {
		t.Errorf("the panel should describe the newly selected task, got: %q", view)
	}
	if strings.Contains(view, "Open a listing on a phone.") {
		t.Errorf("the panel kept the description of the task that was selected before")
	}
}

func TestNarrowBoardHasNoPanel(t *testing.T) {
	if view := sizedModel(90, 40).View(); strings.Contains(view, "ACTIONS") {
		t.Errorf("a narrow board should give all its width to the columns, got: %q", view)
	}
}

func TestPanelSaysWhenNoTaskIsSelected(t *testing.T) {
	m := sizedModel(160, 40)
	m.cursorCol = 2 // Review, which is empty

	if view := m.View(); !strings.Contains(view, "No task selected") {
		t.Errorf("got: %q", view)
	}
}

func TestOnlyAsManyCardsAsFitAreDrawn(t *testing.T) {
	// Two rows of header, one of padding, two of column header: 15 rows are
	// left for cards, and each takes four, plus one to close the last.
	m := sizedModel(160, 20)

	if m.maxVisible != 3 {
		t.Fatalf("maxVisible = %d, want 3", m.maxVisible)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 16})
	if m.maxVisible != 2 {
		t.Fatalf("maxVisible = %d, want 2", m.maxVisible)
	}
}

func TestLeftAndRightStepOverACollapsedColumn(t *testing.T) {
	m := sizedModel(160, 40)
	m.cursorCol = 1 // Doing; Review, to its right, is empty

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if got := m.columns[m.cursorCol]; got != "Done" {
		t.Fatalf("l from Doing went to %s, want Done", got)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if got := m.columns[m.cursorCol]; got != "Doing" {
		t.Fatalf("h from Done went to %s, want Doing", got)
	}

	// With every column open there is nothing to step over.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if got := m.columns[m.cursorCol]; got != "Review" {
		t.Fatalf("l from Doing went to %s, want Review", got)
	}
}

func TestANumberKeyStillReachesACollapsedColumn(t *testing.T) {
	m := sizedModel(160, 40)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})

	if got := m.columns[m.cursorCol]; got != "Review" {
		t.Fatalf("3 went to %s, want Review", got)
	}
}

func TestDescriptionKeepsItsHeadingsApartFromItsText(t *testing.T) {
	lines := describe("## Steps\n\nOpen a listing.\n\n```sh\nmake run\n```\n", 40)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want the heading, the sentence and the command: %+v", len(lines), lines)
	}
	if lines[0] != (descriptionLine{text: "Steps", heading: true}) {
		t.Errorf("first line = %+v, want the heading without its marks", lines[0])
	}
	if lines[1].heading || lines[2].text != "make run" {
		t.Errorf("text lines = %+v", lines[1:])
	}
}

func TestTheStripOfARunningAgentTurns(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetActiveAgents(map[string]bool{"REX-4": true})
	m.SetAgentStates(map[string]string{"REX-4": "working"})
	if !m.HasWorkingAgent() {
		t.Fatal("REX-4 has a running agent")
	}

	for _, frame := range []int{0, 3} {
		m.SetSpinner(frame)
		if view := m.View(); !strings.Contains(view, theme.Spinner(frame)+" running") {
			t.Errorf("frame %d: the strip should be marked %q: %q", frame, theme.Spinner(frame), view)
		}
	}
}

func TestTheTimeAnAgentSpentDoesNotMove(t *testing.T) {
	m := sizedModel(160, 40)
	m.SetDurations(map[string]DurationData{"REX-1": {Working: time.Hour}})
	still := m.View()

	m.SetSpinner(3)

	if m.View() != still {
		t.Error("no agent runs, so nothing should move")
	}
	if m.HasWorkingAgent() {
		t.Error("time spent is not an agent at work")
	}
}
