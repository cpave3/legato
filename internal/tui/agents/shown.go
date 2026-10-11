package agents

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// Shown is what one agent has shown through its Legato tools: cards and
// artifacts that only the web UI can draw. The TUI says how much there is
// and where to see it.
type Shown struct {
	Cards       int
	Artifacts   int
	LatestTitle string
	// URL opens the agent in the web UI on the newest thing it showed. It is
	// empty when there is no web UI to link to.
	URL string
}

// Count is how many things the agent has shown.
func (s Shown) Count() int { return s.Cards + s.Artifacts }

// summary names what there is, and nothing there is none of.
func (s Shown) summary() string {
	var parts []string
	if s.Cards > 0 {
		parts = append(parts, plural(s.Cards, "card"))
	}
	if s.Artifacts > 0 {
		parts = append(parts, plural(s.Artifacts, "artifact"))
	}
	return strings.Join(parts, " · ")
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// ShownRefreshedMsg carries what each agent has shown, by task ID.
type ShownRefreshedMsg struct {
	Shown map[string]Shown
}

// OpenShownMsg asks to open what the selected agent showed in the web UI.
// It is sent for an agent that has shown nothing, or with no URL, too: the
// app then says why nothing opened.
type OpenShownMsg struct {
	TaskID string
	URL    string
	Count  int
}

// SetShown replaces what each agent has shown.
func (m *Model) SetShown(shown map[string]Shown) {
	m.shown = shown
}

// renderShownBar renders the line under the header that says what the
// selected agent has shown and links to it, or "" when it has shown nothing.
//
// The link is never cut short: a partial address can be neither opened nor
// copied. It takes a line of its own when it does not fit beside the rest,
// and is left out when it does not fit at all; the open key still works.
func (m Model) renderShownBar(width int) string {
	a := m.SelectedAgent()
	if a == nil {
		return ""
	}
	shown := m.shown[a.TaskID]
	if shown.Count() == 0 {
		return ""
	}
	inner := width - 2 // one column of padding on each side
	if inner < 1 {
		return ""
	}

	dim := lipgloss.NewStyle().Foreground(theme.TextTertiary)
	text := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	link := lipgloss.NewStyle().Foreground(theme.AccentPurpleAlt).Underline(true)
	key := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true)

	summary := shown.summary()
	// What follows the summary, most useful last to be given up.
	lead := "Shown in web UI: "
	if lipgloss.Width(lead+summary) > inner {
		lead = ""
	}
	first := text.Render(lead) + lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true).Render(summary)
	used := lipgloss.Width(lead + summary)

	if shown.URL == "" {
		if note := " · web UI is off"; used+lipgloss.Width(note) <= inner {
			first += dim.Render(note)
		}
		return shownBarStyle(width).Render(first)
	}

	// The open key is named at the end of the first line. Between it and
	// the summary, the newest item's title takes what room is left.
	const hint = "  o open"
	hintWidth := 0
	if used+lipgloss.Width(hint) <= inner {
		hintWidth = lipgloss.Width(hint)
	}
	urlWidth := lipgloss.Width(shown.URL)
	sameLine := used+hintWidth+2+urlWidth <= inner
	if shown.LatestTitle != "" {
		const label = " · latest: "
		room := inner - used - hintWidth - lipgloss.Width(label)
		if sameLine {
			room -= 2 + urlWidth
		}
		if room >= 8 {
			first += dim.Render(label + truncateID(shown.LatestTitle, room))
		}
	}
	if hintWidth > 0 {
		first += "  " + key.Render("o") + dim.Render(" open")
	}
	switch {
	case sameLine:
		return shownBarStyle(width).Render(first + "  " + link.Render(shown.URL))
	case urlWidth <= inner:
		return shownBarStyle(width).Render(first + "\n" + link.Render(shown.URL))
	default:
		return shownBarStyle(width).Render(first)
	}
}

func shownBarStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(width).Padding(0, 1)
}
