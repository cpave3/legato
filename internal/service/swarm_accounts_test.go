package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

type accountSwarmAgents struct {
	AgentService
	spawned []AgentSpawnOptions
}

func (a *accountSwarmAgents) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...AgentSpawnOptions) error {
	a.spawned = append(a.spawned, opts[0])
	return nil
}
func (a *accountSwarmAgents) LastSpawnConflicts() []AgentSpawnConflict { return nil }

func TestSwarmConductorPassesConfiguredAccount(t *testing.T) {
	sw, agents, db, _ := newTestSwarmService(t)
	spy := &accountSwarmAgents{AgentService: agents}
	svc := sw.(*swarmService)
	svc.agents = spy
	svc.cfg.Conductor = SwarmParticipantConfig{Agent: "claude-code", Account: "conductor"}
	seedParentTask(t, db, "parent-account")
	if err := sw.StartSwarm(context.Background(), "parent-account", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := spy.spawned[0].Account; got != "conductor" {
		t.Fatalf("account = %q", got)
	}
}

func TestSwarmWorkerAccountFollowsAdapterNotTier(t *testing.T) {
	for _, tc := range []struct {
		name, agent, tier, want string
		implicitDefault         bool
	}{
		{name: "default", want: "worker"},
		{name: "same adapter", agent: "claude-code", want: "worker"},
		{name: "same default adapter", agent: "claude-code", implicitDefault: true, want: "worker"},
		{name: "tier", tier: "small", want: "worker"},
		{name: "different adapter", agent: "chimera"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sw, agents, db, _ := newTestSwarmServiceWithAdapter(t, hooks.NewClaudeCodeAdapter("legato"), SwarmConfig{MaxConcurrentAgents: 4})
			spy := &accountSwarmAgents{AgentService: agents}
			svc := sw.(*swarmService)
			svc.agents = spy
			svc.cfg.Worker = SwarmParticipantConfig{Agent: "claude-code", Account: "worker"}
			if tc.implicitDefault {
				svc.cfg.Worker.Agent = ""
			}
			seedParentTask(t, db, "parent-account")
			if err := db.CreateSubtask(context.Background(), store.Subtask{ID: "st-account", ParentTaskID: "parent-account", Title: "account", Role: "worker", Status: "queued", AgentKind: tc.agent, Tier: tc.tier}); err != nil {
				t.Fatal(err)
			}
			if err := sw.Dispatch(context.Background(), "st-account"); err != nil {
				t.Fatal(err)
			}
			if got := spy.spawned[0].Account; got != tc.want {
				t.Fatalf("account = %q, want %q", got, tc.want)
			}
		})
	}
}
