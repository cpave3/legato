package agents

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/tui/theme"
)

func TestTokensAreWrittenShort(t *testing.T) {
	for tokens, want := range map[int64]string{
		0:             "0",
		950:           "950",
		12_345:        "12.3k",
		123_456:       "123k",
		999_999:       "1M",
		1_400_000:     "1.4M",
		920_125_629:   "920M",
		1_260_000_000: "1.3B",
	} {
		if got := formatTokens(tokens); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", tokens, got, want)
		}
	}
}

func TestSidebarSaysHowManyTokensWereUsedTodayWhenItKnows(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())
	m.SetSize(140, 40)
	if view := m.View(); strings.Contains(view, "tokens today") {
		t.Fatalf("with no usage known the sidebar should not speak of tokens: %q", view)
	}

	m.SetUsage(&Usage{TokensToday: 1_400_000, InputToday: 1_390_000, OutputToday: 10_000})

	// The total, and under it the two parts it is made of.
	lines := strings.Split(m.View(), "\n")
	foot := lines[len(lines)-4 : len(lines)-1]
	for i, want := range [][]string{{"tokens today", "1.4M"}, {"↑ in", "1.4M"}, {"↓ out", "10k"}} {
		for _, part := range want {
			if !strings.Contains(foot[i], part) {
				t.Errorf("line %d of the sidebar's foot should contain %q, got: %q", i, part, foot[i])
			}
		}
	}
}

func TestSidebarShowsOnlyTheTotalWhenItsPartsAreNotKnown(t *testing.T) {
	m := New(theme.NewIcons("unicode"))
	m.SetAgents(mixedAgents())
	m.SetSize(140, 40)
	m.SetUsage(&Usage{TokensToday: 1_400_000})

	view := m.View()
	if !strings.Contains(view, "tokens today") || strings.Contains(view, "↑ in") || strings.Contains(view, "↓ out") {
		t.Errorf("only the total is known, so only the total should be shown: %q", view)
	}
}

func TestEnsembleShowsTheTokensOfTheDayAndOfEachHour(t *testing.T) {
	m := ensemble(t)
	if view := m.View(); strings.Contains(view, "TOKENS") {
		t.Fatalf("with no usage known the ensemble should not speak of tokens: %q", view)
	}

	hourly := make([]int64, 24)
	hourly[22], hourly[23] = 500, 1000
	m.SetUsage(&Usage{TokensToday: 920_125_629, InputToday: 917_536_695, OutputToday: 2_588_934, Hourly: hourly})

	view := m.View()
	if header := strings.Split(view, "\n")[0]; !strings.Contains(header, "920M tokens today") || !strings.Contains(header, "↑ 918M ↓ 2.6M") {
		t.Errorf("header should say how many tokens were used today, and how many went in and came out, got: %q", header)
	}
	if !strings.Contains(view, "TOKENS · LAST 24H") || !strings.Contains(view, "▁▁▄█") {
		t.Errorf("the activity panel should draw the tokens of each hour: %q", view)
	}
}
