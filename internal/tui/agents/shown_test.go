package agents

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const shownLink = "http://localhost:3080/agents?task=REX-1238&artifact=ui-0123456789"

// The TUI cannot draw a card or an artifact, so it says how much the selected
// agent has shown and where to see it.
func TestSelectedAgentShowsWhatItShowedAndALinkToIt(t *testing.T) {
	m := newTestModel()
	m, _ = m.Update(ShownRefreshedMsg{Shown: map[string]Shown{
		"REX-1238": {Cards: 2, Artifacts: 1, LatestTitle: "Build status", URL: shownLink},
	}})

	out := m.View()

	for _, want := range []string{"2 cards", "1 artifact", "Build status", shownLink} {
		if !strings.Contains(out, want) {
			t.Fatalf("view does not mention %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1 artifacts") {
		t.Fatalf("view says \"1 artifacts\":\n%s", out)
	}

	// The other agent has shown nothing, so it gets no such line.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if out := m.View(); strings.Contains(out, "artifact") || strings.Contains(out, "http://") {
		t.Fatalf("an agent that has shown nothing has a link:\n%s", out)
	}
}

func TestShownLineCountsOnlyWhatThereIs(t *testing.T) {
	for want, shown := range map[string]Shown{
		"1 card":      {Cards: 1, URL: shownLink},
		"3 artifacts": {Artifacts: 3, URL: shownLink},
	} {
		m := newTestModel()
		m.SetShown(map[string]Shown{"REX-1238": shown})
		out := m.View()
		if !strings.Contains(out, want) {
			t.Errorf("view does not say %q:\n%s", want, out)
		}
		if strings.Contains(out, "0 ") {
			t.Errorf("view counts something there is none of:\n%s", out)
		}
	}
}

// A link that is cut short cannot be opened or copied. In a narrow panel the
// line gives up its other text first, and then the link as a whole.
func TestShownLineNeverCutsItsLinkShort(t *testing.T) {
	for _, width := range []int{200, 120, 110, 100, 90, 80, 70, 60} {
		m := newTestModel()
		m.SetSize(width, 40)
		m.SetShown(map[string]Shown{"REX-1238": {Cards: 2, Artifacts: 1, LatestTitle: "A long title for the newest card the agent showed", URL: shownLink}})

		out := m.View()

		if strings.Contains(out, "http://") && !strings.Contains(out, shownLink) {
			t.Errorf("at width %d the link is cut short:\n%s", width, out)
		}
		if !strings.Contains(out, "2 cards") {
			t.Errorf("at width %d the line does not say what was shown:\n%s", width, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > width {
				t.Errorf("at width %d a line is %d wide: %q", width, lipgloss.Width(line), line)
			}
		}
		if got := lipgloss.Height(out); got > 40 {
			t.Errorf("at width %d the view is %d lines tall, want at most 40", width, got)
		}
	}
}

func TestShownLineSaysWhenThereIsNoWebUIToLinkTo(t *testing.T) {
	m := newTestModel()
	m.SetShown(map[string]Shown{"REX-1238": {Cards: 1}})

	out := m.View()

	if !strings.Contains(out, "1 card") || !strings.Contains(out, "web UI is off") {
		t.Fatalf("view does not say the web UI is off:\n%s", out)
	}
}

func TestOpenKeyAsksToOpenWhatTheSelectedAgentShowed(t *testing.T) {
	m := newTestModel()
	m.SetShown(map[string]Shown{"REX-1238": {Cards: 2, URL: shownLink}})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("expected a command from the 'o' key")
	}
	msg, ok := cmd().(OpenShownMsg)
	if !ok {
		t.Fatalf("message = %T, want OpenShownMsg", cmd())
	}
	if msg.TaskID != "REX-1238" || msg.URL != shownLink || msg.Count != 2 {
		t.Fatalf("message = %#v", msg)
	}

	// For an agent that has shown nothing the key still answers, so the
	// person is told why nothing opened.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("expected a command for an agent that has shown nothing")
	}
	if msg := cmd().(OpenShownMsg); msg.TaskID != "REX-1239" || msg.Count != 0 || msg.URL != "" {
		t.Fatalf("message = %#v", msg)
	}
}
