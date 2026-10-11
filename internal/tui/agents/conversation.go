package agents

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// ConversationEntries is how many entries of a conversation are read for a
// tile. It is more than a tile has lines: long runs of tool calls are cut to
// their newest, and what was said before them is worth reaching.
const ConversationEntries = 40

// shownOfRun is how many tool calls of one run a tile shows when it has more
// to show than room.
const shownOfRun = 3

// conversationGutter is the column a line's mark stands in, and the space
// after it. What an agent said has no mark and is set in from it.
const conversationGutter = 2

// markup is what Markdown puts around words. A tile shows the words.
var markup = strings.NewReplacer("**", "", "`", "")

// plainLines turns a message into plain lines no wider than width, at most
// limit of them: its text without heading marks, code fences, emphasis and
// blank lines. A message that is cut ends with an ellipsis.
func plainLines(text string, width, limit int) []string {
	if width < 1 || limit < 1 {
		return nil
	}
	var lines []string
	for _, source := range strings.Split(text, "\n") {
		source = strings.TrimSpace(source)
		if strings.HasPrefix(source, "```") {
			continue
		}
		source = markup.Replace(strings.TrimSpace(strings.TrimLeft(source, "#")))
		if source == "" {
			continue
		}
		for _, wrapped := range strings.Split(ansi.Wrap(source, width, ""), "\n") {
			if len(lines) == limit {
				lines[limit-1] = ansi.Truncate(lines[limit-1], width-1, "") + "…"
				return lines
			}
			lines = append(lines, wrapped)
		}
	}
	return lines
}

// markedText renders a message as lines: a mark in the gutter of the first,
// and every line after it set in under the text.
func markedText(mark string, markColor lipgloss.Color, text string, textColor lipgloss.Color, width, limit int) []string {
	lines := plainLines(text, width-conversationGutter, limit)
	for i, line := range lines {
		lead := strings.Repeat(" ", conversationGutter)
		if i == 0 && mark != "" {
			lead = colored(markColor, mark) + " "
		}
		lines[i] = lead + colored(textColor, line)
	}
	return lines
}

// toolName is a tool's own name: a plugin's tool without the plugin's
// prefix.
func toolName(name string) string {
	if i := strings.LastIndex(name, "__"); i >= 0 && i+2 < len(name) {
		return name[i+2:]
	}
	return name
}

// toolLine renders one tool call: how it stands, the tool, and what the call
// is for. state is what the agent is doing, which says what a call with no
// result yet is waiting for.
func (m Model) toolLine(entry service.ConversationEntry, state string, width int) string {
	// A call that is over is quiet; the one in flight is what to look at.
	mark, markColor, text, textColor := "●", theme.Green, entry.Text, theme.TextSecondary
	switch entry.State {
	case service.ToolFailed:
		mark, markColor = m.icons.CIFail, theme.Red
	case service.ToolRunning:
		textColor = theme.TextPrimary
		switch state {
		case "working":
			mark, markColor = theme.Spinner(m.spinner), theme.Yellow
		case "waiting":
			// The call waits to be allowed. What is to be allowed is the
			// command, not the agent's words for it.
			mark, markColor = m.icons.AgentWaiting, theme.Amber
			if entry.Command != "" {
				text = entry.Command
			}
		case "background":
			mark, markColor = m.icons.AgentBackground, theme.Cyan
		default:
			// Nothing is running it any more: it was cut short.
			mark, markColor, textColor = m.icons.AgentIdle, theme.TextFaint, theme.TextSecondary
		}
	}

	name := toolName(entry.Tool)
	line := colored(markColor, mark) + " " + lipgloss.NewStyle().Foreground(textColor).Bold(true).Render(truncateID(name, width-conversationGutter))
	if room := width - lipgloss.Width(line) - 1; text != "" && room >= 4 {
		line += " " + colored(textColor, truncateID(text, room))
	}
	return line
}

// requestLines renders what an agent waits on the person for: something to
// allow, a plan to approve, or a question. It returns nil when the agent's
// endpoint reports no open request.
func requestLines(tail service.ConversationTail, width int) []string {
	primary := func(text string) string { return colored(theme.TextPrimary, text) }
	indent := strings.Repeat(" ", conversationGutter)

	if permission := tail.Permission; permission != nil {
		if permission.Plan != "" {
			lines := []string{primary("Asks you to approve a plan:")}
			for _, line := range plainLines(permission.Plan, width-conversationGutter, 3) {
				lines = append(lines, indent+colored(theme.TextSecondary, line))
			}
			return lines
		}
		heading := "Wants to run:"
		if permission.Tool != "" && permission.Tool != "bash" {
			heading = "Wants to use " + toolName(permission.Tool) + ":"
		}
		lines := []string{primary(truncateID(heading, width))}
		for _, line := range plainLines(firstNonEmpty(permission.Command, permission.Reason), width-2, 3) {
			lines = append(lines, theme.Fill(" "+colored(theme.Yellow, line), width, theme.Inset))
		}
		return lines
	}

	if question := tail.Question; question != nil {
		lines := []string{primary("Asks:")}
		for _, line := range plainLines(question.Text, width-conversationGutter, 3) {
			lines = append(lines, indent+colored(theme.Yellow, line))
		}
		if len(question.Options) > 0 {
			options := truncateID(strings.Join(question.Options, " · "), width-conversationGutter)
			lines = append(lines, indent+colored(theme.TextSecondary, options))
		}
		return lines
	}
	return nil
}

// conversationLines renders the end of an agent's conversation in at most
// room lines of width columns: what it waits for, and before that the newest
// things said and done. The newest thing the agent said gets the most room;
// older messages get a line or two each. A run of tool calls is cut to its
// newest few, unless the tile has room to spare.
func (m Model) conversationLines(tail service.ConversationTail, state string, width, room int) []string {
	if room < 1 || width <= conversationGutter {
		return nil
	}
	lines := m.conversationWithRuns(tail, state, width, room, shownOfRun)
	for shown := shownOfRun + 1; len(lines) < room && shown <= len(tail.Entries); shown++ {
		lines = m.conversationWithRuns(tail, state, width, room, shown)
	}
	return lines
}

// conversationWithRuns renders a conversation with at most shownOfEachRun
// tool calls of every run.
func (m Model) conversationWithRuns(tail service.ConversationTail, state string, width, room, shownOfEachRun int) []string {
	// Blocks are gathered from the newest back, until the room is used.
	var blocks [][]string
	remaining := room
	keep := func(lines []string) {
		if len(lines) > remaining {
			lines = lines[:remaining]
		}
		blocks = append(blocks, lines)
		remaining -= len(lines)
	}

	if request := requestLines(tail, width); request != nil {
		keep(request)
	}
	// shown and hidden count the tool calls of the run being walked.
	shown, hidden := 0, 0
	endRun := func(reachedStart bool) {
		if hidden > 0 && remaining > 0 {
			// A run that began before what was read has calls nobody counted.
			counted := !(reachedStart && len(tail.Entries) >= ConversationEntries)
			keep([]string{earlierCalls(hidden, counted)})
		}
		shown, hidden = 0, 0
	}
	newestSaid := true
	for i := len(tail.Entries) - 1; i >= 0 && remaining > 0; i-- {
		entry := tail.Entries[i]
		if entry.Kind == service.EntryTool {
			if shown < shownOfEachRun {
				keep([]string{m.toolLine(entry, state, width)})
				shown++
			} else {
				hidden++
			}
			continue
		}
		if endRun(false); remaining == 0 {
			break
		}
		if entry.Kind == service.EntryUser {
			keep(markedText("›", theme.Accent, entry.Text, theme.TextSecondary, width, min(2, remaining)))
			continue
		}
		limit, color := 2, theme.TextSecondary
		if newestSaid {
			limit, color = max(3, room/2), theme.TextPrimary
			newestSaid = false
		}
		keep(markedText("", "", entry.Text, color, width, min(limit, remaining)))
	}
	// With room left, the walk reached the start of what was read.
	endRun(true)
	if len(blocks) == 0 {
		return []string{colored(theme.TextFaint, "No messages yet.")}
	}

	var lines []string
	for i := len(blocks) - 1; i >= 0; i-- {
		lines = append(lines, blocks[i]...)
	}
	return lines
}

// earlierCalls is the line that stands for the tool calls of a run that are
// not shown.
func earlierCalls(hidden int, counted bool) string {
	text := "earlier calls"
	switch {
	case counted && hidden == 1:
		text = "1 earlier call"
	case counted:
		text = fmt.Sprintf("%d earlier calls", hidden)
	}
	return colored(theme.TextFaint, "⋮ "+text)
}

// contextBar draws how full an agent's context is, or is "" when that is not
// known. It turns amber, then red, as the context fills.
func contextBar(tail service.ConversationTail) string {
	if tail.ContextWindow <= 0 {
		return ""
	}
	share := min(max(tail.ContextUsed*100/tail.ContextWindow, 0), 100)
	const cells = 5
	filled := min((share+100/cells-1)*cells/100, cells)
	color := theme.TextSecondary
	switch {
	case share >= 85:
		color = theme.Red
	case share >= 70:
		color = theme.Amber
	}
	return colored(theme.TextFaint, "ctx ") +
		colored(color, strings.Repeat("▰", filled)) + colored(theme.TextFaint, strings.Repeat("▱", cells-filled)) +
		" " + colored(color, fmt.Sprintf("%d%%", share))
}
