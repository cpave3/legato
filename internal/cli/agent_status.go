package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// AgentStatus returns a status string for the given task.
// When format is "tmux" and the task is a swarm participant, it returns
// swarm-aware tmux markup (progress, last event, active workers, scope
// warnings). For solo agents, it falls back to the same output as
// AgentSummary(--exclude taskID).
func AgentStatus(s *store.Store, taskID string, format string) (string, error) {
	if format != "tmux" {
		return "", fmt.Errorf("unsupported format: %q", format)
	}

	ctx := context.Background()

	// Detect swarm participation by reading the agent session row.
	session, err := s.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil || session == nil || session.ParentTaskID == nil || *session.ParentTaskID == "" {
		// Solo agent, no session, or no parent — fall back to legacy summary.
		return AgentSummary(s, taskID)
	}

	parentID := *session.ParentTaskID

	subs, err := s.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return "", fmt.Errorf("listing subtasks: %w", err)
	}

	var total, done, cancelled, active int
	for _, st := range subs {
		total++
		switch st.Status {
		case "done":
			done++
		case "cancelled":
			cancelled++
		case "dispatched", "in_progress", "reporting":
			active++
		}
	}

	// Latest unacked swarm event drives the "last event" block.
	lastKind := ""
	var lastAt time.Time
	var hasLastAt bool
	events, err := s.ListUnackedSwarmEvents(ctx, parentID)
	if err == nil && len(events) > 0 {
		last := events[len(events)-1]
		lastKind = last.Kind
		lastAt, hasLastAt = parseSwarmEventTime(last.CreatedAt)
	}

	return formatTmuxSwarmStatus(total, done, cancelled, active, lastKind, lastAt, hasLastAt), nil
}

func formatTmuxSwarmStatus(total, done, cancelled, active int, lastEventKind string, lastEventAt time.Time, hasLastAt bool) string {
	var parts []string

	// Progress block:  x/y done
	progressColor := "colour250"
	if total > 0 && done+cancelled == total {
		progressColor = "green"
	}
	parts = append(parts, fmt.Sprintf("#[fg=%s]%d/%d done", progressColor, done, total))

	// Scope-conflict warning icon.
	if lastEventKind == "scope_warning" {
		parts = append(parts, "#[fg=red]⚠")
	}

	// Last event + age.
	if lastEventKind != "" && lastEventKind != "scope_warning" && hasLastAt {
		age := time.Since(lastEventAt)
		ageStr := formatShortAge(age)
		eventColor := "colour245"
		switch lastEventKind {
		case "built":
			eventColor = "yellow"
		case "died":
			eventColor = "red"
		case "question":
			eventColor = "cyan"
		}
		parts = append(parts, fmt.Sprintf("#[fg=%s]%s %s", eventColor, lastEventKind, ageStr))
	}

	// Active sibling worker count.
	if active > 0 {
		parts = append(parts, fmt.Sprintf("#[fg=colour245]%d workers", active))
	}

	return strings.Join(parts, " #[fg=colour240]· ")
}

// parseSwarmEventTime tries SQLite DATETIME and RFC3339 formats.
func parseSwarmEventTime(raw string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func formatShortAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
