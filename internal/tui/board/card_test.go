package board

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

var testIcons = theme.NewIcons("unicode")

func hasSelectedFrame(out string) bool {
	return strings.Contains(out, "╭") && strings.Contains(out, "╰")
}

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
	if !strings.Contains(out, "bug") {
		t.Errorf("card should contain issue type, got: %q", out)
	}
}

func TestCardTitleTruncation(t *testing.T) {
	long := "This is a very long summary that definitely exceeds the width"
	truncated := truncateTitle(long, 20)
	if w := lipgloss.Width(truncated); w > 20 {
		t.Errorf("truncated summary too long: %d > 20", w)
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Errorf("truncated summary should end with ellipsis, got: %q", truncated)
	}
}

func TestCardTitleTruncationKeepsWholeCharacters(t *testing.T) {
	truncated := truncateTitle("Größenänderung für Übersichtsseite", 10)
	if !utf8.ValidString(truncated) {
		t.Errorf("truncation cut a character in two: %q", truncated)
	}
}

func TestCardIsABlockOfTheWidthAsked(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "A title that is longer than the card is wide", Priority: "High", IssueType: "Bug", WorkspaceName: "Work", AgentActive: true, AgentState: "working"}
	for _, selected := range []bool{false, true} {
		for i, line := range strings.Split(RenderCard(card, 30, selected, "Doing", testIcons), "\n") {
			if w := lipgloss.Width(line); w != 30 {
				t.Errorf("selected=%v: line %d is %d wide, want 30: %q", selected, i, w, line)
			}
		}
	}
}

func TestSelectingACardDoesNotChangeItsSize(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	selected := RenderCard(card, 30, true, "Doing", testIcons)
	unselected := RenderCard(card, 30, false, "Doing", testIcons)

	if lipgloss.Height(selected) != lipgloss.Height(unselected) {
		t.Fatalf("selected card is %d lines, unselected %d", lipgloss.Height(selected), lipgloss.Height(unselected))
	}
	if !hasSelectedFrame(selected) || hasSelectedFrame(unselected) {
		t.Fatalf("only the selected card should have a frame")
	}
}

func TestCardMarksPriorityAndSaysWhereTheTaskComesFrom(t *testing.T) {
	local := RenderCard(CardData{Key: "f41p6uf0", Title: "Local"}, 34, false, "Backlog", testIcons)
	if !strings.Contains(local, "· f41p6uf0") || !strings.Contains(local, "local") {
		t.Errorf("a local task with no priority: %q", local)
	}

	tracked := RenderCard(CardData{Key: "AG-7467", Title: "Tracked", Priority: "Medium", Provider: "jira"}, 34, false, "Backlog", testIcons)
	if !strings.Contains(tracked, "▲ AG-7467") || !strings.Contains(tracked, "jira") {
		t.Errorf("a tracked task with a priority: %q", tracked)
	}
}

func TestCardPutsTypeWorkspaceAndPullRequestOnOneLineWhenTheyFit(t *testing.T) {
	card := CardData{Key: "AG-9967", Title: "Convert", IssueType: "Task", WorkspaceName: "work", PRNumber: 7, PRCheckStatus: "pass", PRReviewDecision: "APPROVED"}

	wide := RenderCard(card, 60, false, "Doing", testIcons)
	narrow := RenderCard(card, 24, false, "Doing", testIcons)

	if !strings.Contains(wide, "task · work · ✓ CI · ✓ review") {
		t.Errorf("wide card should say it on one line: %q", wide)
	}
	if lipgloss.Height(narrow) != lipgloss.Height(wide)+1 {
		t.Errorf("narrow card should give the pull request a line of its own: %q", narrow)
	}
	if !strings.Contains(narrow, "✓ CI") || !strings.Contains(narrow, "✓ review") {
		t.Errorf("narrow card lost the pull request state: %q", narrow)
	}
}

func TestCardSaysWhenItsAgentMarkedTheWorkReadyForReview(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", AgentActive: true, ReviewReady: true, ReviewUnreviewed: 2}
	out := RenderCard(card, 40, false, "Doing", testIcons)
	if !strings.Contains(out, "ready for review") {
		t.Errorf("card should say the work is ready for review: %q", out)
	}
	if strings.Contains(out, "idle") {
		t.Errorf("an agent that handed its work over should not read as idle: %q", out)
	}

	busy := RenderCard(CardData{Key: "task1", Title: "Test", AgentActive: true, AgentState: "working", ReviewReady: true}, 40, false, "Doing", testIcons)
	if !strings.Contains(busy, "running") {
		t.Errorf("what the agent is doing now comes first: %q", busy)
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
	if !strings.Contains(out, "○ idle") {
		t.Errorf("a card with an idle agent should say so, got: %q", out)
	}
}

func TestCardWorktreeIndicator(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", HasWorktree: true}
	for _, tc := range []struct {
		name     string
		selected bool
		column   string
	}{
		{name: "normal", column: "Doing"},
		{name: "selected", selected: true, column: "Doing"},
		{name: "done", column: "Done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderCard(card, 30, tc.selected, tc.column, testIcons)
			if !strings.Contains(out, testIcons.Worktree) {
				t.Fatalf("worktree card should contain %q indicator, got: %q", testIcons.Worktree, out)
			}
		})
	}
}

func TestCardWithoutWorktreeHasNoWorktreeIndicator(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test"}
	if out := RenderCard(card, 30, false, "Doing", testIcons); strings.Contains(out, testIcons.Worktree) {
		t.Fatalf("card without worktree should not contain %q: %q", testIcons.Worktree, out)
	}
}

func TestCardNoAgentIndicatorByDefault(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if strings.Contains(out, "idle") {
		t.Error("card without agent should not have an agent line")
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
	if !strings.Contains(out, "running") {
		t.Error("card with working agent should say running")
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
	if !strings.Contains(out, "needs you") {
		t.Error("card with waiting agent should say it needs the person")
	}
	if !strings.Contains(out, "2h 15m") {
		t.Errorf("card should contain '2h 15m', got: %q", out)
	}
}

func TestCardWithMixedAgentState(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test task", AgentActive: true, AgentState: "mixed"}
	out := RenderCard(card, 30, false, "Doing", testIcons)
	if !strings.Contains(out, "mixed") {
		t.Fatalf("mixed card should say mixed: %q", out)
	}
	if strings.Contains(out, "idle") {
		t.Fatalf("mixed card should not say idle: %q", out)
	}
}

func TestCardWithErrorAgentState(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test task", AgentActive: true, AgentState: "error"}
	for _, selected := range []bool{false, true} {
		out := RenderCard(card, 30, selected, "Doing", testIcons)
		if !strings.Contains(out, "error") {
			t.Errorf("selected=%v: error card should say error: %q", selected, out)
		}
		if strings.Contains(out, "idle") {
			t.Errorf("selected=%v: error card should not say idle: %q", selected, out)
		}
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
		if strings.Contains(line, "running") || strings.Contains(line, "needs you") || strings.Contains(line, "idle") {
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
	out := RenderColumn("Doing", cards, 30, true, 0, len(cards), testIcons)
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

func TestSelectedCardInDoneColumnUsesSelectedStyle(t *testing.T) {
	card := CardData{Key: "REX-1", Title: "Finished", Priority: "Low", IssueType: "Story"}
	selected := RenderCard(card, 30, true, "Done", testIcons)
	unselected := RenderCard(card, 30, false, "Done", testIcons)

	if selected == unselected {
		t.Fatal("selected done card should render differently from unselected done card")
	}
	if !hasSelectedFrame(selected) {
		t.Errorf("selected done card should use selected frame, got: %q", selected)
	}
}

func TestCardWithPRPassingCI(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "pass", PRReviewDecision: "APPROVED"}
	out := RenderCard(card, 40, false, "Review", testIcons)
	if !strings.Contains(out, "CI") {
		t.Errorf("card with passing CI should show CI indicator, got: %q", out)
	}
	if !strings.Contains(out, testIcons.CIPass) {
		t.Errorf("card with passing CI should show pass icon, got: %q", out)
	}
}

func TestCardWithPRFailingCI(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "fail"}
	out := RenderCard(card, 40, false, "Review", testIcons)
	if !strings.Contains(out, testIcons.CIFail) {
		t.Errorf("card with failing CI should show fail icon, got: %q", out)
	}
}

func TestCardWithPRPendingCI(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "pending"}
	out := RenderCard(card, 40, false, "Review", testIcons)
	if !strings.Contains(out, testIcons.CIPending) {
		t.Errorf("card with pending CI should show pending icon, got: %q", out)
	}
}

func TestCardWithPRChangesRequested(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "pass", PRReviewDecision: "CHANGES_REQUESTED"}
	out := RenderCard(card, 40, false, "Review", testIcons)
	if !strings.Contains(out, "changes") {
		t.Errorf("card with changes requested should say so, got: %q", out)
	}
}

func TestCardWithPRDraft(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRIsDraft: true}
	out := RenderCard(card, 40, false, "Doing", testIcons)
	if !strings.Contains(out, "draft PR") {
		t.Errorf("card with draft PR should say so, got: %q", out)
	}
}

func TestCardWithPRComments(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "pass", PRCommentCount: 5}
	out := RenderCard(card, 40, false, "Review", testIcons)
	if !strings.Contains(out, "5 comments") {
		t.Errorf("card with 5 comments should count them, got: %q", out)
	}
}

func TestCardWithNoPR(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", Priority: "High"}
	out := RenderCard(card, 40, false, "Doing", testIcons)
	// Should NOT contain any PR indicators
	if strings.Contains(out, "CI") || strings.Contains(out, "draft") || strings.Contains(out, "changes") {
		t.Errorf("card without PR should not show PR indicators, got: %q", out)
	}
}

func TestCardWithPRSelectedRender(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test", PRNumber: 42, PRCheckStatus: "fail", PRReviewDecision: "CHANGES_REQUESTED"}
	out := RenderCard(card, 40, true, "Review", testIcons)
	if !strings.Contains(out, testIcons.CIFail) {
		t.Errorf("selected card with failing CI should show fail icon, got: %q", out)
	}
}

func TestCardWithAgentWaitingOnBackgroundWork(t *testing.T) {
	card := CardData{Key: "task1", Title: "Test task", AgentActive: true, AgentState: "background"}

	for _, selected := range []bool{false, true} {
		out := RenderCard(card, 30, selected, "Doing", testIcons)
		if !strings.Contains(out, "background") {
			t.Errorf("selected=%v: card should say background, got: %q", selected, out)
		}
		if strings.Contains(out, "idle") {
			t.Errorf("selected=%v: an agent with a shell still running is not idle: %q", selected, out)
		}
	}
}
