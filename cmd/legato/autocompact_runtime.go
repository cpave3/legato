package main

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// newAutoCompactor compacts idle native agents past
// agents.compact.auto_percent, read live from the effective settings. It
// runs in the process that serves the broker; nil when the agent service
// cannot compact.
func newAutoCompactor(db *store.Store, agents service.AgentService) *service.AutoCompactor {
	compactor, ok := agents.(service.AgentCompactor)
	if !ok {
		return nil
	}
	reader, canRead := agents.(service.AgentConversationReader)
	return service.NewAutoCompactor(service.AutoCompactorOptions{
		Percent: func(ctx context.Context) int {
			cfg, err := settingsFor(db).Effective(ctx)
			if err != nil || cfg == nil {
				return 0
			}
			return cfg.Agents.Compact.AutoPercent
		},
		Agents: func(ctx context.Context) ([]service.AutoCompactAgent, error) {
			sessions, err := agents.ListAgents(ctx)
			if err != nil || !canRead {
				return nil, err
			}
			var out []service.AutoCompactAgent
			for _, s := range sessions {
				if s.Status != "running" || !agents.HasChimeraEndpoint(ctx, s.TaskID) {
					continue
				}
				agent := service.AutoCompactAgent{TaskID: s.TaskID, Idle: s.Activity == ""}
				// The fill is read only where it could matter.
				if agent.Idle {
					if tail, err := reader.ConversationTail(ctx, s.TaskID, 1); err == nil {
						agent.Used, agent.Window = tail.ContextUsed, tail.ContextWindow
					}
				}
				out = append(out, agent)
			}
			return out, nil
		},
		Compact: func(ctx context.Context, taskID string) error {
			return compactor.CompactAgent(ctx, taskID, "")
		},
	})
}
