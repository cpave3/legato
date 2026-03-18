package board

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
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
			{ID: "REX-1", Summary: "First", Priority: "High", IssueType: "Bug", Status: "Backlog"},
			{ID: "REX-2", Summary: "Second", Priority: "Medium", IssueType: "Story", Status: "Backlog"},
			{ID: "REX-3", Summary: "Third", Priority: "Low", IssueType: "Task", Status: "Backlog"},
		},
		"Doing": {
			{ID: "REX-4", Summary: "In progress", Priority: "High", IssueType: "Bug", Status: "Doing"},
		},
		"Review": {},
		"Done": {
			{ID: "REX-5", Summary: "Finished", Priority: "Low", IssueType: "Story", Status: "Done"},
		},
	}
	return cards[column], nil
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

func newTestModel() Model {
	m := New(&fakeBoardService{})
	// Simulate Init loading data
	m = m.loadData()
	m.width = 120
	m.height = 40
	return m
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

func TestViewNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view == "" {
		t.Error("view should not be empty")
	}
}
