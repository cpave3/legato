package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEveryPageRoutesBindingsThroughRootUpdate(t *testing.T) {
	tests := []struct {
		name string
		view viewType
		key  tea.KeyMsg
	}{
		{"board", viewBoard, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"detail", viewDetail, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"agents", viewAgents, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"report", viewReport, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"review", viewReview, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"plans", viewPlans, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
		{"attention", viewAttention, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp()
			app.active = tt.view
			updated, _ := app.Update(tt.key)
			if updated.(App).active != tt.view {
				t.Fatalf("root routing changed page from %v to %v", tt.view, updated.(App).active)
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
