package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/attention"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/plan"
	"github.com/cpave3/legato/internal/tui/report"
	"github.com/cpave3/legato/internal/tui/review"
)

func TestEveryPageRoutesObservableBindingThroughRootUpdate(t *testing.T) {
	tests := []struct {
		name string
		view viewType
		key  tea.KeyMsg
		want any
	}{
		{"board", viewBoard, tea.KeyMsg{Type: tea.KeyEnter}, board.OpenDetailMsg{}},
		{"detail", viewDetail, tea.KeyMsg{Type: tea.KeyEsc}, detail.BackToBoard{}},
		{"agents", viewAgents, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}, agents.OpenAgentSpawnMsg{}},
		{"report", viewReport, tea.KeyMsg{Type: tea.KeyEsc}, report.ReturnToBoardMsg{}},
		{"review", viewReview, tea.KeyMsg{Type: tea.KeyEsc}, review.ReturnToBoardMsg{}},
		{"plans", viewPlans, tea.KeyMsg{Type: tea.KeyEsc}, plan.ReturnToBoardMsg{}},
		{"attention", viewAttention, tea.KeyMsg{Type: tea.KeyEsc}, attention.ReturnToBoardMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := initTestApp()
			app.active = tt.view
			_, cmd := app.Update(tt.key)
			if cmd == nil {
				t.Fatal("root route produced no observable command")
			}
			if got := cmd(); fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
				t.Fatalf("command = %T, want %T", got, tt.want)
			}
		})
	}
}

func TestCapabilityGatedRootBindingDoesNotDispatch(t *testing.T) {
	app := newTestApp()
	app.reviewSvc = nil
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	if updated.(App).active != viewBoard || cmd != nil {
		t.Fatal("review binding dispatched without review service capability")
	}
}

func TestFocusedOverlayConsumesQuestionMarkThroughRoot(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := updated.(App)
	if got.overlayType != overlaySearch {
		t.Fatalf("focused search overlay replaced by %v", got.overlayType)
	}
	if !strings.Contains(got.activeOverlay.View(), "?") {
		t.Fatal("question mark did not reach focused search input")
	}
}

func TestControlCAlwaysQuits(t *testing.T) {
	for _, view := range []viewType{viewBoard, viewDetail, viewAgents, viewReport, viewReview, viewPlans, viewAttention} {
		app := newTestApp()
		app.active = view
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatalf("view %v: ctrl+c produced no command", view)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("view %v: ctrl+c did not quit", view)
		}
	}
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c with overlay produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c with overlay did not quit")
	}
}
