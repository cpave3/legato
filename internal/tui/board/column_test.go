package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

var testColIcons = theme.NewIcons("unicode")

func TestColumnHeaderContainsName(t *testing.T) {
	cards := []CardData{{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}}
	out := RenderColumn("Doing", cards, 20, true, 0, len(cards), testColIcons)
	if !strings.Contains(strings.ToUpper(out), "DOING") {
		t.Errorf("column header should contain uppercase name, got: %q", out)
	}
}

func TestColumnHeaderContainsCount(t *testing.T) {
	cards := []CardData{
		{Key: "REX-1", Title: "A", Priority: "High", IssueType: "Bug"},
		{Key: "REX-2", Title: "B", Priority: "Low", IssueType: "Story"},
	}
	out := RenderColumn("Ready", cards, 20, false, -1, len(cards), testColIcons)
	if !strings.Contains(out, "2") {
		t.Errorf("column header should contain card count 2, got: %q", out)
	}
}

func TestColumnEmptyRendersHeader(t *testing.T) {
	out := RenderColumn("Backlog", nil, 20, false, -1, 0, testColIcons)
	if !strings.Contains(strings.ToUpper(out), "BACKLOG") {
		t.Errorf("empty column should still show header, got: %q", out)
	}
	if !strings.Contains(out, " 0 ") {
		t.Errorf("empty column should show count 0, got: %q", out)
	}
}

func TestColumnRendersCards(t *testing.T) {
	cards := []CardData{
		{Key: "REX-1", Title: "First", Priority: "High", IssueType: "Bug"},
		{Key: "REX-2", Title: "Second", Priority: "Low", IssueType: "Story"},
	}
	out := RenderColumn("Doing", cards, 30, true, 0, len(cards), testColIcons)
	if !strings.Contains(out, "REX-1") {
		t.Errorf("column should contain first card key, got: %q", out)
	}
	if !strings.Contains(out, "REX-2") {
		t.Errorf("column should contain second card key, got: %q", out)
	}
}

func TestColumnActiveVsInactive(t *testing.T) {
	cards := []CardData{{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}}
	active := RenderColumn("Doing", cards, 30, true, 0, len(cards), testColIcons)
	inactive := RenderColumn("Doing", cards, 30, false, -1, len(cards), testColIcons)
	if active == "" || inactive == "" {
		t.Error("both active and inactive columns should render non-empty")
	}
}

func TestColumnKeepsOneLineBetweenCardsWhicheverIsSelected(t *testing.T) {
	cards := []CardData{
		{Key: "REX-1", Title: "First"},
		{Key: "REX-2", Title: "Second"},
		{Key: "REX-3", Title: "Third"},
	}
	none := RenderColumn("Doing", cards, 30, false, -1, len(cards), testColIcons)
	for selected := range cards {
		out := RenderColumn("Doing", cards, 30, true, selected, len(cards), testColIcons)
		if lipgloss.Height(out) != lipgloss.Height(none) {
			t.Errorf("selecting card %d changed the column from %d to %d lines", selected, lipgloss.Height(none), lipgloss.Height(out))
		}
		if !hasSelectedFrame(out) {
			t.Errorf("card %d selected, but the column shows no frame: %q", selected, out)
		}
	}
}

func TestColumnSaysWhenItHasNoTasks(t *testing.T) {
	out := RenderColumn("Review", nil, 30, true, -1, 0, testColIcons)
	if !strings.Contains(out, "no tasks") {
		t.Errorf("an empty column should say so, got: %q", out)
	}
}

func TestRailNamesACollapsedColumnDownItsLength(t *testing.T) {
	out := RenderRail("Ready", 0, false, 12)

	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("rail is %d lines, want 12", len(lines))
	}
	var letters []string
	for i, line := range lines {
		if w := lipgloss.Width(line); w != RailWidth {
			t.Errorf("line %d is %d wide, want %d: %q", i, w, RailWidth, line)
		}
		if text := strings.TrimSpace(line); text != "" {
			letters = append(letters, text)
		}
	}
	if got := strings.Join(letters, ""); got != "━━━0READY" {
		t.Fatalf("rail reads %q, want its rule, its count and its name", got)
	}
}

func TestRailCutsANameThatIsLongerThanItIsTall(t *testing.T) {
	out := RenderRail("Unmapped", 0, false, 6)
	if lipgloss.Height(out) != 6 {
		t.Fatalf("rail is %d lines, want 6: %q", lipgloss.Height(out), out)
	}
}
