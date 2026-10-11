package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// AgentUIService tells the TUI what an agent has shown through its Legato
// tools. The TUI cannot draw a card or an artifact; it counts them and links
// to the web UI, which can.
type AgentUIService interface {
	Shown(ctx context.Context, taskID string) (service.AgentUIShown, error)
}

// SetAgentUIService enables the line in the agents view that says what the
// selected agent has shown, and the key that opens it. webURL is where the
// web UI is served ("http://localhost:3080"); with none, the line has no link
// and the key says the web UI is off.
func (a *App) SetAgentUIService(svc AgentUIService, webURL string) {
	a.agentUISvc = svc
	a.agentUIWebURL = webURL
}

// agentUIUpdateMsg is delivered when an agent shows, replaces or loses an
// artifact or a card.
type agentUIUpdateMsg struct {
	Payload events.AgentUIChangedPayload
}

func (a App) listenAgentUIUpdates() tea.Cmd {
	ch := a.agentUISub
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		payload, _ := ev.Payload.(events.AgentUIChangedPayload)
		return agentUIUpdateMsg{Payload: payload}
	}
}

// loadAgentShown asks what each agent has shown and where the web UI has the
// newest of it. It returns nil with no service to ask.
func (a App) loadAgentShown(list []service.AgentSession) tea.Cmd {
	if a.agentUISvc == nil {
		return nil
	}
	svc, webURL := a.agentUISvc, a.agentUIWebURL
	taskIDs := make([]string, 0, len(list))
	for _, agent := range list {
		taskIDs = append(taskIDs, agent.TaskID)
	}
	return func() tea.Msg {
		shown := make(map[string]agents.Shown, len(taskIDs))
		for _, taskID := range taskIDs {
			summary, err := svc.Shown(context.Background(), taskID)
			if err != nil || summary.Cards+summary.Artifacts == 0 {
				continue
			}
			shown[taskID] = agents.Shown{
				Cards: summary.Cards, Artifacts: summary.Artifacts, LatestTitle: summary.LatestTitle,
				URL: service.AgentUIWebLink(webURL, taskID, summary.LatestID),
			}
		}
		return agents.ShownRefreshedMsg{Shown: shown}
	}
}

// handleAgentUIUpdate reloads what the agents showed and, for something newly
// shown, says so: nothing else in the TUI would tell the person.
func (a App) handleAgentUIUpdate(msg agentUIUpdateMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if cmd := a.loadAgentShown(a.agentView.Agents()); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if msg.Payload.Kind == "shown" && msg.Payload.TaskID != "" {
		what := "a card"
		if msg.Payload.Placement == service.AgentUIPanel {
			what = "an artifact"
		}
		text := fmt.Sprintf("%s showed %s in the web UI", msg.Payload.TaskID, what)
		if a.active == viewAgents {
			text += " (o opens it)"
		}
		cmds = append(cmds, func() tea.Msg { return statusbar.InfoMsg{Text: text} })
	}
	if a.agentUISub != nil {
		cmds = append(cmds, a.listenAgentUIUpdates())
	}
	return a, tea.Batch(cmds...)
}

// handleOpenShown opens what an agent showed in the browser, or says why
// there is nothing to open.
func (a App) handleOpenShown(msg agents.OpenShownMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Count == 0:
		text := msg.TaskID + " has not shown anything in the web UI"
		return a, func() tea.Msg { return statusbar.InfoMsg{Text: text} }
	case msg.URL == "":
		text := "The web UI is off: set web.enabled: true, or run legato serve, to see what " + msg.TaskID + " showed"
		return a, func() tea.Msg { return statusbar.ErrorMsg{Text: text} }
	}
	open, url := a.openURL, msg.URL
	return a, func() tea.Msg {
		if err := open(url); err != nil {
			// The address is the useful part when no browser can be started
			// (a remote terminal): the person can open it somewhere else.
			return statusbar.ErrorMsg{Text: fmt.Sprintf("Could not open a browser (%v). Open %s", err, url)}
		}
		return statusbar.InfoMsg{Text: "Opened in the web UI"}
	}
}
