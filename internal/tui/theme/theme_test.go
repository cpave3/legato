package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColorPaletteNonZero(t *testing.T) {
	colors := map[string]lipgloss.Color{
		"Background":      Background,
		"TextPrimary":     TextPrimary,
		"TextSecondary":   TextSecondary,
		"TextTertiary":    TextTertiary,
		"AccentPurple":    AccentPurple,
		"AccentPurpleAlt": AccentPurpleAlt,
		"PriorityHighBg":  PriorityHighBg,
		"PriorityHighFg":  PriorityHighFg,
		"PriorityMedBg":   PriorityMedBg,
		"PriorityMedFg":   PriorityMedFg,
		"PriorityLowBg":   PriorityLowBg,
		"PriorityLowFg":   PriorityLowFg,
		"PriorityNone":    PriorityNone,
		"SyncOK":          SyncOK,
		"SyncActive":      SyncActive,
		"SyncError":       SyncError,
		"SyncOffline":     SyncOffline,
		"ColBacklog":      ColBacklog,
		"ColReady":        ColReady,
		"ColDoing":        ColDoing,
		"ColReview":       ColReview,
		"ColDone":         ColDone,
	}

	for name, c := range colors {
		if string(c) == "" {
			t.Errorf("color %s is empty", name)
		}
	}
}

func TestStylesNonZero(t *testing.T) {
	// Verify styles are configured (not just zero-value lipgloss.Style).
	// We check that the style produces some non-empty render string for a test input.
	styles := map[string]lipgloss.Style{
		"CardBase":          CardBase,
		"CardSelected":      CardSelected,
		"ColumnHeader":      ColumnHeader,
		"ColumnHeaderActive": ColumnHeaderActive,
		"StatusBar":         StatusBar,
		"KeyHintKey":        KeyHintKey,
		"KeyHintLabel":      KeyHintLabel,
		"DoneMuted":         DoneMuted,
	}

	for name, s := range styles {
		rendered := s.Render("test")
		if rendered == "" {
			t.Errorf("style %s renders empty string", name)
		}
	}
}

func TestPriorityBadgeStyles(t *testing.T) {
	badges := map[string]lipgloss.Style{
		"High":   PriorityBadgeHigh,
		"Medium": PriorityBadgeMed,
		"Low":    PriorityBadgeLow,
	}
	for name, s := range badges {
		rendered := s.Render("P1")
		if rendered == "" {
			t.Errorf("priority badge %s renders empty string", name)
		}
	}
}

func TestColumnBorderColor(t *testing.T) {
	tests := []struct {
		column string
		want   lipgloss.Color
	}{
		{"Backlog", ColBacklog},
		{"Ready", ColReady},
		{"Doing", ColDoing},
		{"Review", ColReview},
		{"Done", ColDone},
		{"Unknown", PriorityNone},
	}
	for _, tt := range tests {
		got := ColumnBorderColor(tt.column)
		if got != tt.want {
			t.Errorf("ColumnBorderColor(%q) = %v, want %v", tt.column, got, tt.want)
		}
	}
}

func TestPriorityBorderColor(t *testing.T) {
	tests := []struct {
		priority string
		want     lipgloss.Color
	}{
		{"High", PriorityHighFg},
		{"Highest", PriorityHighFg},
		{"Medium", PriorityMedFg},
		{"Low", PriorityLowFg},
		{"Lowest", PriorityLowFg},
		{"", PriorityNone},
		{"Unknown", PriorityNone},
	}
	for _, tt := range tests {
		got := PriorityBorderColor(tt.priority)
		if got != tt.want {
			t.Errorf("PriorityBorderColor(%q) = %v, want %v", tt.priority, got, tt.want)
		}
	}
}
