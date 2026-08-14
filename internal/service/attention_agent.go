package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

type agentAttentionSource interface {
	ListAgents(context.Context) ([]AgentSession, error)
}

type AgentAttentionAdapter struct {
	agents agentAttentionSource
}

func NewAgentAttentionAdapter(agents agentAttentionSource) *AgentAttentionAdapter {
	return &AgentAttentionAdapter{agents: agents}
}

func (a *AgentAttentionAdapter) Producer() string { return "agents" }

func (a *AgentAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	agents, err := a.agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0)
	for _, agent := range agents {
		if agent.ExecutionID == "" || agent.Status != store.ExecutionRunning || agent.ExecutionKind != store.ExecutionInteractive || agent.WorkflowRunID != "" || agent.ParentTaskID != "" || agent.SubtaskID != "" {
			continue
		}
		signal, reason, urgency := "", "", ""
		switch agent.Activity {
		case store.ActivityWaiting:
			signal, reason, urgency = AttentionSignalAgentWaiting, "Agent is waiting for your input", "normal"
		case store.ActivityError:
			signal, reason, urgency = AttentionSignalAgentError, "Agent encountered an error", "high"
		default:
			continue
		}
		title := agent.Title
		if title == "" {
			title = agent.AttributionTaskID
		}
		items = append(items, AttentionInput{
			SignalKind:     signal,
			SourceKey:      agent.ExecutionID,
			SourceRevision: fmt.Sprintf("%s:%s", agent.ExecutionID, agent.Activity),
			TaskID:         agent.AttributionTaskID,
			Title:          title,
			Reason:         reason,
			Detail:         agent.AgentKind,
			Category:       "agent",
			Urgency:        urgency,
			Navigation:     AttentionNavigation{Kind: "task", Target: agent.AttributionTaskID},
			Actions:        []AttentionAction{{ID: "open", Label: "Open agent", Kind: "navigate"}},
		})
	}
	return items, nil
}
