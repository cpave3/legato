package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTitleBarNamesTheViewAndWhatItShows(t *testing.T) {
	bar := TitleBar(100, "board", []string{"8 tasks", "3 with agents"}, []string{"all workspaces", "synced", "web :3080"})

	lines := strings.Split(bar, "\n")
	if len(lines) != TitleBarHeight {
		t.Fatalf("title bar is %d lines, want %d", len(lines), TitleBarHeight)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 100 {
			t.Errorf("line %d is %d wide, want 100", i, w)
		}
	}
	for _, want := range []string{"♩ legato", "board", "8 tasks · 3 with agents", "all workspaces · synced · web :3080"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("title bar should contain %q, got: %q", want, lines[0])
		}
	}
	if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "web :3080") {
		t.Errorf("what the view is set to should be at the far end, got: %q", lines[0])
	}
	if strings.Trim(lines[1], "─") != "" {
		t.Errorf("the second line should be a rule, got: %q", lines[1])
	}
}

func TestTitleBarGivesUpItsLastPiecesFirstWhenItIsNarrow(t *testing.T) {
	bar := strings.Split(TitleBar(58, "board", []string{"8 tasks"}, []string{"all workspaces", "synced", "web :3080"}), "\n")[0]

	if !strings.Contains(bar, "8 tasks") || !strings.Contains(bar, "all workspaces") {
		t.Errorf("the view's own facts and its first setting should stay, got: %q", bar)
	}
	if strings.Contains(bar, "web :3080") {
		t.Errorf("the last piece should be the first to go, got: %q", bar)
	}
	if w := lipgloss.Width(bar); w != 58 {
		t.Errorf("the bar is %d wide, want 58", w)
	}
}
