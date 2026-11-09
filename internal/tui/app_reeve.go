package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// ReeveOpener starts, resumes or messages the Reeve's session and says which
// task it runs as (service.ReeveService).
type ReeveOpener interface {
	Open(ctx context.Context, by string) (string, error)
	Ask(ctx context.Context, brief, by string) (string, error)
}

// SetReeve offers R, which opens the Reeve from anywhere, and a in the
// agents view, which asks it about the selected agent.
func (a *App) SetReeve(reeve ReeveOpener) {
	a.reeve = reeve
	a.agentView.SetReeveEnabled(reeve != nil)
}

// reeveOpenedMsg is the Reeve's session, opened or given a brief.
type reeveOpenedMsg struct {
	taskID string
	err    error
}

func (a App) openReeve() tea.Cmd {
	reeve := a.reeve
	if reeve == nil {
		return nil
	}
	return func() tea.Msg {
		taskID, err := reeve.Open(context.Background(), "tui")
		return reeveOpenedMsg{taskID: taskID, err: err}
	}
}

func (a App) askReeve(msg agents.AskReeveMsg) tea.Cmd {
	reeve := a.reeve
	if reeve == nil {
		return nil
	}
	brief := agentReeveBrief(msg)
	return func() tea.Msg {
		taskID, err := reeve.Ask(context.Background(), brief, "tui")
		return reeveOpenedMsg{taskID: taskID, err: err}
	}
}

// showReeve opens the agents view on the Reeve's session.
func (a App) showReeve(msg reeveOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return a, func() tea.Msg { return statusbar.ErrorMsg{Text: "The Reeve: " + msg.err.Error()} }
	}
	a = a.setMode(viewAgents)
	a.agentView.StartPolling()
	a.agentView.SelectByTaskID(msg.taskID)
	cmds := []tea.Cmd{agentTickCmd()}
	if a.agentSvc != nil {
		svc, taskID := a.agentSvc, msg.taskID
		cmds = append(cmds, func() tea.Msg {
			_ = svc.ReconcileSessions(context.Background())
			agentList, _ := svc.ListAgents(context.Background())
			return agents.AgentsRefreshedMsg{Agents: agentList, SelectTask: taskID}
		})
	}
	return a, tea.Batch(cmds...)
}

// agentReeveBrief is what the Reeve is told when asked about an agent: what
// it is, its IDs, and what it is doing.
func agentReeveBrief(msg agents.AskReeveMsg) string {
	title := msg.Title
	if title == "" {
		title = msg.TaskID
	}
	lines := []string{fmt.Sprintf("The person asks you about the agent “%s”, which %s.", title, stateClause(msg.State)), "- task: " + msg.TaskID}
	if msg.AgentKind != "" {
		lines = append(lines, "- agent tool: "+msg.AgentKind)
	}
	lines = append(lines, "", "Find out what is wrong and what to do about it. Read first; any change goes to the person as a change set.")
	return strings.Join(lines, "\n")
}

func stateClause(state string) string {
	switch state {
	case "", "idle":
		return "is idle"
	case "ended":
		return "has ended"
	case "needs you":
		return "needs you"
	default:
		return "is " + state
	}
}
