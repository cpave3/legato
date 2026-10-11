package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColumnBorderColor(t *testing.T) {
	tests := []struct {
		column string
		want   lipgloss.Color
	}{
		{"Backlog", ColBacklog},
		{"Triage", ColTriage},
		{"Ready", ColReady},
		{"Doing", ColDoing},
		{"Review", ColReview},
		{"Done", ColDone},
		{"done", ColDone},
		{"Unmapped", ColOther},
	}
	for _, tt := range tests {
		got := ColumnBorderColor(tt.column)
		if got != tt.want {
			t.Errorf("ColumnBorderColor(%q) = %v, want %v", tt.column, got, tt.want)
		}
	}
}

func TestPriorityColor(t *testing.T) {
	tests := []struct {
		priority string
		want     lipgloss.Color
	}{
		{"High", Red},
		{"Highest", Red},
		{"Medium", Amber},
		{"Low", Blue},
		{"Lowest", Blue},
		{"", TextFaint},
		{"Unknown", TextFaint},
	}
	for _, tt := range tests {
		got := PriorityColor(tt.priority)
		if got != tt.want {
			t.Errorf("PriorityColor(%q) = %v, want %v", tt.priority, got, tt.want)
		}
	}
}

func TestEveryAgentStateThatAsksForSomethingHasItsOwnColor(t *testing.T) {
	seen := map[lipgloss.Color]string{}
	for _, state := range []string{"working", "waiting", "background", "error", "done"} {
		color := StateColor(state)
		if other, taken := seen[color]; taken {
			t.Errorf("%q and %q share the color %v", state, other, color)
		}
		seen[color] = state
		if StateTint(state) == Card {
			t.Errorf("%q has no tint of its own", state)
		}
	}
	if StateColor("") != TextFaint {
		t.Errorf("an idle agent should be faint, got %v", StateColor(""))
	}
}
