package service

import (
	"context"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"strings"
	"testing"
)

type fakeWorkflowAgentService struct {
	taskID string
	name   string
	opts   AgentSpawnOptions
}

func (f *fakeWorkflowAgentService) SpawnWorkflowAgent(_ context.Context, taskID, name string, opts AgentSpawnOptions) (*store.AgentSession, error) {
	f.taskID, f.name, f.opts = taskID, name, opts
	return &store.AgentSession{ID: 7, TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: opts.AgentKind, Status: "running"}, nil
}
func (f *fakeWorkflowAgentService) KillAgent(context.Context, string) error { return nil }
func (f *fakeWorkflowAgentService) FinishWorkflowAgent(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (f *fakeWorkflowAgentService) PromptAgent(context.Context, string, string) error { return nil }

func TestAgentRunExecutorLaunchesExplicitWorkflowAssignment(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	executor := NewAgentRunExecutor(agents, "/opt/legato current/legato")
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{
		RunID: "wr-1", AttemptID: "wa-1", TaskID: "task-1", StepID: "review", Type: "agent.run",
		With:    map[string]any{"name": "PR Review - 123", "agent": "chimera", "tier": "large", "working_dir": "/tmp/wt", "prompt": "Run 3-tier-review"},
		Outputs: map[string]workflowengine.Output{"review": {Type: "file", Path: "review.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "waiting_agent" || agents.opts.Assignment == "" {
		t.Fatalf("result=%+v opts=%+v", result, agents.opts)
	}
	if agents.name != "PR Review - 123" {
		t.Fatalf("name = %q, want %q", agents.name, "PR Review - 123")
	}
	if agents.opts.ExtraEnv["LEGATO_WORKFLOW_RUN_ID"] != "wr-1" || agents.opts.WorkingDir != "/tmp/wt" || agents.opts.AgentKind != "chimera" {
		t.Fatalf("opts = %+v", agents.opts)
	}
	if !strings.Contains(agents.opts.Assignment, "'/opt/legato current/legato' workflow step complete") {
		t.Fatalf("assignment lacks exact callback binary: %s", agents.opts.Assignment)
	}
}
