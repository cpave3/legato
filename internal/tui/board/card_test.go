package board

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/tui/theme"
)

var testIcons = theme.NewIcons("unicode")

func TestCardRenderContainsKey(t *testing.T) {
	card := CardData{Key: "REX-1234", Title: "Fix the bug", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "REX-1234") {
		t.Errorf("card should contain issue key, got: %q", out)
	}
}

func TestCardRenderContainsType(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "Bug") {
		t.Errorf("card should contain issue type, got: %q", out)
	}
}

func TestCardTitleTruncation(t *testing.T) {
	long := "This is a very long summary that definitely exceeds the width"
	truncated := truncateTitle(long, 20)
	if len(truncated) > 20 {
		t.Errorf("truncated summary too long: %d > 20", len(truncated))
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("truncated summary should end with ellipsis, got: %q", truncated)
	}
}

func TestCardTitleNoTruncationWhenShort(t *testing.T) {
	short := "Short"
	result := truncateTitle(short, 20)
	if result != short {
		t.Errorf("short summary should not be truncated, got: %q", result)
	}
}

func TestCardRenderNonEmpty(t *testing.T) {
	priorities := []string{"High", "Medium", "Low", ""}
	for _, p := range priorities {
		t.Run(p, func(t *testing.T) {
			card := CardData{Key: "REX-1", Title: "Test", Priority: p, IssueType: "Bug"}
			out := RenderCard(card, 30, false, "Doing", testIcons)
			if out == "" {
				t.Error("card should not be empty")
			}
			// Should have multiple lines (key, summary, type)
			lines := strings.Split(out, "\n")
			if len(lines) < 3 {
				t.Errorf("card should have at least 3 lines, got %d", len(lines))
			}
		})
	}
}

func TestCardSelectedRender(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, true, "Doing", testIcons)
	if out == "" {
		t.Error("selected card should not be empty")
	}
	if !strings.Contains(out, "REX-1") {
		t.Errorf("selected card should contain key, got: %q", out)
	}
}

func TestCardWarningIndicator(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug", Warning: true}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "!") {
		t.Errorf("warning card should contain '!' indicator, got: %q", out)
	}
}

func TestCardNoWarningByDefault(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	// The key line should NOT start with !
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "! REX-1") {
			t.Error("card without warning should not have ! prefix")
		}
	}
}

func TestCardAgentActiveIndicator(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug", AgentActive: true}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "▶") {
		t.Errorf("agent-active card should contain '▶' indicator, got: %q", out)
	}
}

func TestCardNoAgentIndicatorByDefault(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if strings.Contains(out, "▶") {
		t.Error("card without agent should not have ▶ indicator")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-1 * time.Second, ""},
		{30 * time.Second, "<1m"},
		{59 * time.Second, "<1m"},
		{1 * time.Minute, "1m"},
		{45 * time.Minute, "45m"},
		{59 * time.Minute, "59m"},
		{60 * time.Minute, "1h"},
		{90 * time.Minute, "1h 30m"},
		{135 * time.Minute, "2h 15m"},
		{3 * time.Hour, "3h"},
	}
	for _, tt := range tests {
		t.Run(tt.d.String(), func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestCardWithAgentStateLine(t *testing.T) {
	card := CardData{
		Key:             "task1",
		Title:           "Test task",
		Priority:        "High",
		AgentActive:     true,
		AgentState:      "working",
		WorkingDuration: 45 * time.Minute,
	}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "RUNNING") {
		t.Error("card with working agent should contain RUNNING")
	}
	if !strings.Contains(out, "45m") {
		t.Errorf("card should contain duration '45m', got: %q", out)
	}
}

func TestCardWithWaitingAgentState(t *testing.T) {
	card := CardData{
		Key:             "task1",
		Title:           "Test task",
		AgentActive:     true,
		AgentState:      "waiting",
		WaitingDuration: 2*time.Hour + 15*time.Minute,
	}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "WAITING") {
		t.Error("card with waiting agent should contain WAITING")
	}
	if !strings.Contains(out, "2h 15m") {
		t.Errorf("card should contain '2h 15m', got: %q", out)
	}
}

func TestCardWithDurationHistoryNoAgent(t *testing.T) {
	card := CardData{
		Key:             "task1",
		Title:           "Test task",
		WorkingDuration: 90 * time.Minute,
		WaitingDuration: 20 * time.Minute,
	}
	out := RenderCard(card, 40, false, "Doing", testIcons)
	// Duration line shows icon + duration: "⟳ 1h 30m · ◆ 20m"
	if !strings.Contains(out, "1h 30m") {
		t.Errorf("card should contain '1h 30m', got: %q", out)
	}
	if !strings.Contains(out, "20m") {
		t.Errorf("card should contain '20m', got: %q", out)
	}
}

func TestCardWithoutAgentInfoHasNoAgentLine(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test task", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	lines := strings.Split(out, "\n")
	// Without agent info, card should be shorter (3 content lines + border)
	for _, line := range lines {
		if strings.Contains(line, "RUNNING") || strings.Contains(line, "WAITING") || strings.Contains(line, "IDLE") {
			t.Error("card without agent info should not have agent line")
		}
	}
}

func TestCardWithDurationIsTaller(t *testing.T) {
	noDuration := CardData{Key: "task1", Title: "Test", Priority: "High", IssueType: "Bug"}
	withDuration := CardData{Key: "task2", Title: "Test", Priority: "High", IssueType: "Bug", WorkingDuration: 10 * time.Minute}

	outNoDuration := RenderCard(noDuration, 30, false, "Doing", testIcons)
	outWithDuration := RenderCard(withDuration, 30, false, "Doing", testIcons)

	noDurationLines := strings.Count(outNoDuration, "\n")
	withDurationLines := strings.Count(outWithDuration, "\n")

	if withDurationLines <= noDurationLines {
		t.Errorf("card with duration should be taller (%d lines) than without (%d lines)", withDurationLines, noDurationLines)
	}
}

func TestUniformCardHeightInColumn(t *testing.T) {
	cards := []CardData{
		{Key: "task1", Title: "No agent", Priority: "High", IssueType: "Bug"},
		{Key: "task2", Title: "Has agent", Priority: "Low", AgentActive: true, AgentState: "working"},
	}
	out := RenderColumn("Doing", cards, 30, true, 0, testIcons)
	// Both cards should render; the column should have uniform height
	if !strings.Contains(out, "task1") || !strings.Contains(out, "task2") {
		t.Error("column should contain both cards")
	}
}

func TestCardDoneColumnRender(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Finished", Priority: "Low", IssueType: "Story"}
	out := RenderCard(card, 30, false, "Done", testIcons)
	if out == "" {
		t.Error("done card should not be empty")
	}
	if !strings.Contains(out, "REX-1") {
		t.Errorf("done card should contain key, got: %q", out)
	}
}
