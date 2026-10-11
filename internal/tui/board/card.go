package board

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cpave3/legato/internal/tui/theme"
)

// CardData holds the data needed to render a card.
type CardData struct {
	Key              string
	Title            string
	Priority         string
	IssueType        string
	Provider         string // "jira", "github", or "" for local
	Warning          bool
	AgentActive      bool          // true if any agent session is running for this task
	AgentState       string        // "working", "waiting", "error", or "" (idle/no agent)
	WorkingDuration  time.Duration // cumulative working time
	WaitingDuration  time.Duration // cumulative waiting time
	WorkspaceName    string        // workspace name (populated in "All" view)
	WorkspaceColor   string        // workspace color hex (populated in "All" view)
	PRCheckStatus    string        // pass, fail, pending, or ""
	PRReviewDecision string        // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	PRCommentCount   int           // total PR comments
	PRIsDraft        bool          // PR is a draft
	PRNumber         int           // PR number (0 = no PR linked)
	SwarmStats       SwarmStats    // sub-task aggregate; zero value = no swarm
	ReviewUnreviewed int           // unreviewed review-tour steps
	ReviewReady      bool          // agent marked the tour ready for review
	HasWorktree      bool          // task has an associated worktree
}

// SwarmStats holds aggregate sub-task counts for a swarm parent card.
type SwarmStats struct {
	Total    int
	Done     int
	InReview int
	Building int
	Queued   int
	Rejected int
}

// HasSwarm reports whether the card is a swarm parent (has any sub-tasks).
func (s SwarmStats) HasSwarm() bool { return s.Total > 0 }

// formatDuration formats a duration as a human-readable string.
// Returns "", "<1m", "Xm", or "Xh Ym".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	totalMinutes := int(d.Minutes())
	if totalMinutes < 1 {
		return "<1m"
	}
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func colored(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

var metaSeparator = colored(theme.TextFaint, " · ")

// agentStrip is the line at the foot of a card that reports its agent.
type agentStrip struct {
	state string // names the color and the tint; "" for a quiet strip
	mark  string
	text  string
	right string
}

// stripFor returns the strip of a card, and false for a card with nothing to
// report: no agent now and none before.
func stripFor(card CardData, icons theme.Icons) (agentStrip, bool) {
	switch card.AgentState {
	case "working":
		return agentStrip{"working", icons.AgentWorking, "running", formatDuration(card.WorkingDuration)}, true
	case "waiting":
		return agentStrip{"waiting", icons.AgentWaiting, "needs you", formatDuration(card.WaitingDuration)}, true
	case "background":
		// Its turn ended with a shell or subagent of its own still running.
		return agentStrip{"background", icons.AgentBackground, "background", formatDuration(card.WorkingDuration)}, true
	case "error":
		return agentStrip{"error", icons.Warning, "error", ""}, true
	case "mixed":
		return agentStrip{"mixed", icons.AgentMixed, "mixed", ""}, true
	}
	if card.ReviewReady {
		return agentStrip{"done", icons.AgentDone, "ready for review", ""}, true
	}
	if card.AgentActive {
		return agentStrip{"", icons.AgentIdle, "idle", formatDuration(card.WorkingDuration)}, true
	}
	return agentStrip{}, false
}

func (s agentStrip) render(width int) string {
	left := " " + colored(theme.StateColor(s.state), s.mark) + " " + colored(theme.TextPrimary, s.text)
	right := ""
	if s.right != "" {
		right = colored(theme.TextSecondary, s.right) + " "
	}
	return theme.Fill(theme.Spread(left, right, width), width, theme.StateTint(s.state))
}

// historyLine reports the time agents spent on a card that has none now.
func historyLine(card CardData, icons theme.Icons) string {
	var parts []string
	if d := formatDuration(card.WorkingDuration); d != "" {
		parts = append(parts, colored(theme.TextFaint, icons.AgentWorked+" ")+colored(theme.TextSecondary, d))
	}
	if d := formatDuration(card.WaitingDuration); d != "" {
		parts = append(parts, colored(theme.TextFaint, icons.AgentWaiting+" ")+colored(theme.TextSecondary, d))
	}
	return strings.Join(parts, metaSeparator)
}

// pullRequestParts names the state of a card's pull request: "draft PR", or
// its checks and its review, then how many comments it has.
func pullRequestParts(card CardData, icons theme.Icons) []string {
	if card.PRNumber == 0 {
		return nil
	}
	var parts []string
	if card.PRIsDraft {
		parts = append(parts, colored(theme.TextSecondary, icons.PRDraft+" draft PR"))
	} else {
		switch card.PRCheckStatus {
		case "pass":
			parts = append(parts, colored(theme.Green, icons.CIPass+" CI"))
		case "fail":
			parts = append(parts, colored(theme.Red, icons.CIFail+" CI"))
		case "pending":
			parts = append(parts, colored(theme.Yellow, icons.CIPending+" CI"))
		}
		switch card.PRReviewDecision {
		case "APPROVED":
			parts = append(parts, colored(theme.Green, icons.PRApproved+" review"))
		case "CHANGES_REQUESTED":
			parts = append(parts, colored(theme.Red, icons.PRChanges+" changes"))
		}
	}
	if card.PRCommentCount == 1 {
		parts = append(parts, colored(theme.TextSecondary, "1 comment"))
	} else if card.PRCommentCount > 1 {
		parts = append(parts, colored(theme.TextSecondary, fmt.Sprintf("%d comments", card.PRCommentCount)))
	}
	return parts
}

// taskParts names what kind of task a card is and where it belongs.
func taskParts(card CardData) []string {
	var parts []string
	if card.IssueType != "" {
		color := theme.TextSecondary
		if strings.EqualFold(card.IssueType, "bug") {
			color = theme.Red
		}
		parts = append(parts, colored(color, strings.ToLower(card.IssueType)))
	}
	if card.WorkspaceName != "" {
		color := theme.TextSecondary
		if card.WorkspaceColor != "" {
			color = lipgloss.Color(card.WorkspaceColor)
		}
		parts = append(parts, colored(color, card.WorkspaceName))
	}
	if card.SwarmStats.HasSwarm() {
		parts = append(parts, colored(theme.Accent, fmt.Sprintf("%d/%d ◊", card.SwarmStats.Done, card.SwarmStats.Total)))
	}
	if card.ReviewUnreviewed > 0 {
		parts = append(parts, colored(theme.Yellow, fmt.Sprintf("⏵%d", card.ReviewUnreviewed)))
	}
	return parts
}

// sourceLabel says where a task comes from.
func sourceLabel(card CardData, icons theme.Icons) string {
	switch card.Provider {
	case "jira":
		return colored(theme.TextSecondary, icons.Jira+" jira")
	case "github":
		return colored(theme.TextSecondary, icons.GitHub+" github")
	case "":
		return colored(theme.TextFaint, "local")
	default:
		return colored(theme.TextSecondary, card.Provider)
	}
}

// priorityMark is the sign a card opens with: an arrow in the priority's
// color, or a faint dot for a task with none.
func priorityMark(priority string) string {
	switch priority {
	case "High", "Highest", "Medium":
		return colored(theme.PriorityColor(priority), "▲")
	case "Low", "Lowest":
		return colored(theme.PriorityColor(priority), "▼")
	default:
		return colored(theme.TextFaint, "·")
	}
}

// RenderCard renders one card as a block exactly width columns wide.
//
// The first and the last line of the block are the card's frame. They are
// drawn only for the selected card and are blank otherwise, so a card takes
// the same room either way; a column lays the last line of one card over the
// first line of the next.
func RenderCard(card CardData, width int, selected bool, column string, icons theme.Icons) string {
	// One column at each side for the frame, and one inside it as padding.
	fill := width - 2
	if fill < 6 {
		fill = 6
	}
	text := fill - 2
	done := strings.EqualFold(column, "Done")

	key := priorityMark(card.Priority) + " " + colored(theme.TextSecondary, card.Key)
	if card.HasWorktree {
		key += " " + colored(theme.Accent, icons.Worktree)
	}
	if card.Warning {
		key += " " + lipgloss.NewStyle().Foreground(theme.Red).Bold(true).Render(icons.Warning)
	}
	keyLine := theme.Spread(key, sourceLabel(card, icons), text)

	titleStyle := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	if done {
		titleStyle = lipgloss.NewStyle().Foreground(theme.TextSecondary).Strikethrough(true)
	}
	titleLine := titleStyle.Render(truncateTitle(card.Title, text))

	// What the task is and the state of its pull request share a line when
	// both fit; otherwise the pull request has the next line to itself.
	lines := []string{keyLine, titleLine}
	task, pr := taskParts(card), pullRequestParts(card, icons)
	if joined := strings.Join(append(append([]string{}, task...), pr...), metaSeparator); lipgloss.Width(joined) <= text || len(task) == 0 || len(pr) == 0 {
		lines = append(lines, joined)
	} else {
		lines = append(lines, strings.Join(task, metaSeparator), strings.Join(pr, metaSeparator))
	}

	bg := theme.Card
	if selected {
		bg = theme.CardActive
	}
	rows := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		rows = append(rows, theme.Fill(" "+line, fill, bg))
	}
	if strip, ok := stripFor(card, icons); ok {
		rows = append(rows, strip.render(fill))
	} else if history := historyLine(card, icons); history != "" {
		rows = append(rows, theme.Fill(" "+history, fill, bg))
	}

	if !selected {
		blank := strings.Repeat(" ", fill+2)
		out := make([]string, 0, len(rows)+2)
		out = append(out, blank)
		for _, row := range rows {
			out = append(out, " "+row+" ")
		}
		return strings.Join(append(out, blank), "\n")
	}

	frame := lipgloss.NewStyle().Foreground(theme.Accent)
	side := frame.Render("│")
	out := make([]string, 0, len(rows)+2)
	out = append(out, frame.Render("╭"+strings.Repeat("─", fill)+"╮"))
	for _, row := range rows {
		out = append(out, side+row+side)
	}
	return strings.Join(append(out, frame.Render("╰"+strings.Repeat("─", fill)+"╯")), "\n")
}

// truncateTitle cuts a title to maxWidth columns, ending it with an ellipsis
// when something was cut.
func truncateTitle(s string, maxWidth int) string {
	if maxWidth < 1 {
		return ""
	}
	return ansi.Truncate(s, maxWidth, "…")
}
