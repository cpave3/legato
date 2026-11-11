package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/statusbar"
)

// interruptedAgents is the agent service as the interrupted agents need it,
// or nil when it keeps none.
func (a App) interruptedAgents() service.InterruptedAgents {
	svc, _ := a.agentSvc.(service.InterruptedAgents)
	return svc
}

// loadInterrupted reads the agents a crash interrupted, for the agents view.
func (a App) loadInterrupted() tea.Cmd {
	svc := a.interruptedAgents()
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		list, err := svc.ListInterruptedAgents(context.Background())
		if err != nil {
			return nil
		}
		return agents.InterruptedRefreshedMsg{Agents: list}
	}
}

// interruptedDone reports how an action on interrupted agents went, then
// refreshes the running and the interrupted agents.
func (a App) interruptedDone(progress string, act func(service.InterruptedAgents) tea.Msg) (tea.Model, tea.Cmd) {
	svc := a.interruptedAgents()
	if svc == nil {
		return a, func() tea.Msg { return statusbar.ErrorMsg{Text: "This Legato does not keep interrupted agents"} }
	}
	agentSvc := a.agentSvc
	return a, tea.Batch(
		func() tea.Msg { return statusbar.ProgressMsg{Text: progress} },
		func() tea.Msg {
			outcome := act(svc)
			list, _ := svc.ListInterruptedAgents(context.Background())
			running, _ := agentSvc.ListAgents(context.Background())
			return tea.BatchMsg{
				func() tea.Msg { return outcome },
				func() tea.Msg { return agents.InterruptedRefreshedMsg{Agents: list} },
				func() tea.Msg { return agents.AgentsRefreshedMsg{Agents: running} },
			}
		},
	)
}

func (a App) handleResumeInterrupted(msg agents.ResumeInterruptedMsg) (tea.Model, tea.Cmd) {
	progress, done := "Resuming "+msg.TaskID+"…", "Resumed "+msg.TaskID+", telling it it was interrupted"
	if msg.Fresh {
		progress, done = "Starting a new agent on "+msg.TaskID+"…", "Started a new agent on "+msg.TaskID+": it had no conversation to resume"
	}
	return a.interruptedDone(progress, func(svc service.InterruptedAgents) tea.Msg {
		if err := svc.ResumeInterruptedAgent(context.Background(), msg.TaskID, msg.Fresh); err != nil {
			return statusbar.ErrorMsg{Text: "Could not resume " + msg.TaskID + ": " + err.Error()}
		}
		return statusbar.InfoMsg{Text: done}
	})
}

func (a App) handleDismissInterrupted(msg agents.DismissInterruptedMsg) (tea.Model, tea.Cmd) {
	return a.interruptedDone("Dismissing "+msg.TaskID+"…", func(svc service.InterruptedAgents) tea.Msg {
		if err := svc.DismissInterruptedAgent(context.Background(), msg.TaskID); err != nil {
			return statusbar.ErrorMsg{Text: "Could not dismiss " + msg.TaskID + ": " + err.Error()}
		}
		return statusbar.InfoMsg{Text: "Dismissed " + msg.TaskID}
	})
}

func (a App) handleResumeAllInterrupted() (tea.Model, tea.Cmd) {
	return a.interruptedDone("Resuming the interrupted agents…", func(svc service.InterruptedAgents) tea.Msg {
		results := svc.ResumeAllInterruptedAgents(context.Background())
		var failed []string
		for _, r := range results {
			if r.Err != nil {
				failed = append(failed, r.TaskID+": "+r.Err.Error())
			}
		}
		if len(failed) > 0 {
			return statusbar.ErrorMsg{Text: fmt.Sprintf("Resumed %d of %d; %s", len(results)-len(failed), len(results), strings.Join(failed, "; "))}
		}
		return statusbar.InfoMsg{Text: fmt.Sprintf("Resumed %d interrupted agent(s)", len(results))}
	})
}
