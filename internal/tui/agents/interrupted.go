package agents

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/theme"
)

// statusInterrupted marks an agent of the list that a crash interrupted: it
// was running when Legato last stopped, and its tmux session was gone when
// it started again.
const statusInterrupted = "interrupted"

// InterruptedRefreshedMsg carries the interrupted agents, which the list
// keeps beside the running ones.
type InterruptedRefreshedMsg struct {
	Agents []service.InterruptedAgent
}

// ResumeInterruptedMsg asks the app to start an interrupted agent again as
// it was, resuming its conversation, or with Fresh a new agent on its task.
type ResumeInterruptedMsg struct {
	TaskID string
	Fresh  bool
}

// DismissInterruptedMsg asks the app to mark an interrupted agent ended.
type DismissInterruptedMsg struct {
	TaskID string
}

// ResumeAllInterruptedMsg asks the app to resume every interrupted agent
// that can be.
type ResumeAllInterruptedMsg struct{}

// withInterrupted is the running agents and the interrupted ones.
func (m Model) withInterrupted(running []service.AgentSession) []service.AgentSession {
	out := make([]service.AgentSession, 0, len(running)+len(m.interrupted))
	for _, a := range running {
		if a.Status != statusInterrupted {
			out = append(out, a)
		}
	}
	for _, a := range m.interrupted {
		out = append(out, service.AgentSession{
			TaskID: a.TaskID, Title: a.Title, AgentKind: a.AgentKind, Engram: a.Engram,
			Status: statusInterrupted, Sandboxed: a.Sandboxed, EndedAt: nonZero(a.InterruptedAt),
		})
	}
	return out
}

// interruptedInfo is what the list knows of an interrupted agent.
func (m Model) interruptedInfo(taskID string) (service.InterruptedAgent, bool) {
	for _, a := range m.interrupted {
		if a.TaskID == taskID {
			return a, true
		}
	}
	return service.InterruptedAgent{}, false
}

// selectedInterrupted is the selected agent when a crash interrupted it.
func (m Model) selectedInterrupted() (service.InterruptedAgent, bool) {
	a := m.SelectedAgent()
	if a == nil || a.Status != statusInterrupted {
		return service.InterruptedAgent{}, false
	}
	return m.interruptedInfo(a.TaskID)
}

// renderInterrupted takes the terminal's place for an interrupted agent:
// what happened and what can be done.
func (m Model) renderInterrupted(a service.InterruptedAgent, width, height int) string {
	lines := []string{
		colored(theme.Amber, "Interrupted"),
		"",
		"This agent was running when Legato stopped, and its tmux session was gone when Legato started again: the machine or tmux went down.",
		"",
	}
	if a.WorkingDir != "" {
		lines = append(lines, "Folder: "+a.WorkingDir)
	}
	if len(a.Resources) > 0 {
		lines = append(lines, "Resources: "+strings.Join(a.Resources, ", "))
	}
	lines = append(lines, "")
	switch {
	case a.Resumable:
		lines = append(lines, "R resumes its conversation as it was. X dismisses it. A resumes every interrupted agent.")
	case a.CanStartNew:
		lines = append(lines, "It cannot be resumed: "+a.Reason+".", "R starts a new agent on its task instead. X dismisses it.")
	default:
		lines = append(lines, "It cannot be resumed: "+a.Reason+".", "X dismisses it.")
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Padding(1, 2).
		Foreground(theme.TextSecondary).Render(strings.Join(lines, "\n"))
}

func nonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
