package board

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// awayBoardService is the board with REX-4 away on a worker: it stays in
// Doing, the column it left.
type awayBoardService struct{ fakeBoardService }

func (f *awayBoardService) ListCards(ctx context.Context, column string) ([]service.Card, error) {
	cards, err := f.fakeBoardService.ListCards(ctx, column)
	for i := range cards {
		if cards[i].ID == "REX-4" {
			cards[i].Away = &service.AwayTask{TaskID: "REX-4", WorkerName: "vm-1", State: "in_progress", LeftColumn: "Doing"}
		}
	}
	return cards, err
}

func (f *awayBoardService) ListCardsByWorkspace(ctx context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return f.ListCards(ctx, column)
}

func TestAwayCardSaysWhichWorkerItIsOnInTheColumnItLeft(t *testing.T) {
	m := New(&awayBoardService{}, theme.NewIcons("unicode"))
	m, _ = m.Update(m.Init()())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	card := m.SelectedCard()
	if card == nil || card.Key != "REX-4" || card.Away == nil || card.Away.WorkerName != "vm-1" {
		t.Fatalf("selected card in Doing = %+v, want REX-4 away on vm-1", card)
	}
	if got := m.AwayTask("REX-4"); got == nil || got.WorkerName != "vm-1" {
		t.Fatalf("AwayTask(REX-4) = %+v", got)
	}
	if m.AwayTask("REX-1") != nil {
		t.Fatal("REX-1 is not away")
	}

	view := m.View()
	for _, want := range []string{"away on vm-1", "in progress"} {
		if !strings.Contains(view, want) {
			t.Errorf("the board should say %q, got: %q", want, view)
		}
	}
}

func TestAwayRefusalTellsThePersonWhereToBringItBack(t *testing.T) {
	got := AwayRefusal("REX-4", "vm-1")
	if got != "REX-4 is away on vm-1: return or recall it from the web UI" {
		t.Fatalf("AwayRefusal = %q", got)
	}
}

func TestAwayCardKeepsTheWidthAskedWithALongWorkerName(t *testing.T) {
	card := CardData{Key: "REX-4", Title: "Away", Away: &service.AwayTask{WorkerName: "a-worker-with-a-very-long-name", State: "in_progress"}}
	for _, selected := range []bool{false, true} {
		for i, line := range strings.Split(RenderCard(card, 30, selected, "Doing", testIcons), "\n") {
			if w := lipgloss.Width(line); w != 30 {
				t.Errorf("selected=%v: line %d is %d wide, want 30: %q", selected, i, w, line)
			}
		}
	}
}
