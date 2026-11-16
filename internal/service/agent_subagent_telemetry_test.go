package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// An execution records whether its agent tool reports its sub-agents, so a
// report can tell a whole agent tree's time from the top-level agent's.
func TestSpawnRecordsWhetherTheAgentToolReportsItsSubagents(t *testing.T) {
	stateMod := hooks.NewClaudeCodeAdapter("legato")
	stateMod.SetStateModDir(t.TempDir())
	for _, test := range []struct {
		name    string
		adapter AIToolAdapter
		want    bool
	}{
		{"claude code with its state plugin", stateMod, true},
		{"claude code without it", hooks.NewClaudeCodeAdapter("legato"), false},
		{"codex", hooks.NewCodexAdapter("legato"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, err := store.New(filepath.Join(t.TempDir(), "telemetry.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{
				Adapter: test.adapter, DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
			})
			svc.(*agentService).nativeBindTimeout = time.Millisecond
			ctx := context.Background()
			createTask(t, s, "task-t")
			if err := svc.SpawnAgent(ctx, "task-t", 0, 0, AgentSpawnOptions{AgentKind: test.adapter.Name(), WorkingDir: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			session, err := s.GetAgentSessionByTaskID(ctx, "task-t")
			if err != nil {
				t.Fatal(err)
			}
			execution, err := s.GetExecution(ctx, session.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			if execution.SubagentTelemetry != test.want {
				t.Fatalf("subagent telemetry = %v, want %v", execution.SubagentTelemetry, test.want)
			}
		})
	}
}
