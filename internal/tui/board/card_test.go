package board

import (
	"strings"
	"testing"
)

func TestCardRenderContainsKey(t *testing.T) {
	card := CardData{Key: "REX-1234", Summary: "Fix the bug", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing")
	if !strings.Contains(out, "REX-1234") {
		t.Errorf("card should contain issue key, got: %q", out)
	}
}

func TestCardRenderContainsType(t *testing.T) {
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing")
	if !strings.Contains(out, "Bug") {
		t.Errorf("card should contain issue type, got: %q", out)
	}
}

func TestCardSummaryTruncation(t *testing.T) {
	long := "This is a very long summary that definitely exceeds the width"
	truncated := truncateSummary(long, 20)
	if len(truncated) > 20 {
		t.Errorf("truncated summary too long: %d > 20", len(truncated))
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("truncated summary should end with ellipsis, got: %q", truncated)
	}
}

func TestCardSummaryNoTruncationWhenShort(t *testing.T) {
	short := "Short"
	result := truncateSummary(short, 20)
	if result != short {
		t.Errorf("short summary should not be truncated, got: %q", result)
	}
}

func TestCardRenderNonEmpty(t *testing.T) {
	priorities := []string{"High", "Medium", "Low", ""}
	for _, p := range priorities {
		t.Run(p, func(t *testing.T) {
			card := CardData{Key: "REX-1", Summary: "Test", Priority: p, IssueType: "Bug"}
			out := RenderCard(card, 30, false, "Doing")
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
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, true, "Doing")
	if out == "" {
		t.Error("selected card should not be empty")
	}
	if !strings.Contains(out, "REX-1") {
		t.Errorf("selected card should contain key, got: %q", out)
	}
}

func TestCardWarningIndicator(t *testing.T) {
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug", Warning: true}
	out := RenderCard(card, 30, false, "Doing")
	if !strings.Contains(out, "!") {
		t.Errorf("warning card should contain '!' indicator, got: %q", out)
	}
}

func TestCardNoWarningByDefault(t *testing.T) {
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing")
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
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug", AgentActive: true}
	out := RenderCard(card, 30, false, "Doing")
	if !strings.Contains(out, "▶") {
		t.Errorf("agent-active card should contain '▶' indicator, got: %q", out)
	}
}

func TestCardNoAgentIndicatorByDefault(t *testing.T) {
	card := CardData{Key: "REX-1", Summary: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing")
	if strings.Contains(out, "▶") {
		t.Error("card without agent should not have ▶ indicator")
	}
}

func TestCardDoneColumnRender(t *testing.T) {
	card := CardData{Key: "REX-1", Summary: "Finished", Priority: "Low", IssueType: "Story"}
	out := RenderCard(card, 30, false, "Done")
	if out == "" {
		t.Error("done card should not be empty")
	}
	if !strings.Contains(out, "REX-1") {
		t.Errorf("done card should contain key, got: %q", out)
	}
}
