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
	calls  []string
}

func (f *fakeWorkflowAgentService) SpawnWorkflowAgent(_ context.Context, taskID, name string, opts AgentSpawnOptions) (*store.AgentSession, error) {
	f.taskID, f.name, f.opts = taskID, name, opts
	f.calls = append(f.calls, "spawn:"+taskID)
	return &store.AgentSession{ID: 7, TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: opts.AgentKind, Status: "running"}, nil
}
func (f *fakeWorkflowAgentService) KillAgent(_ context.Context, taskID string) error {
	f.calls = append(f.calls, "stop:"+taskID)
	return nil
}
func (f *fakeWorkflowAgentService) FinishWorkflowAgent(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (f *fakeWorkflowAgentService) PromptAgent(context.Context, string, string) error { return nil }

func TestAgentStopExecutorStopsTaskAgentIdempotently(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	executor := NewAgentStopExecutor(agents)
	for range 2 {
		result, err := executor.Start(context.Background(), ResolvedWorkflowStep{TaskID: "workflow-task", With: map[string]any{"task": map[string]any{"id": "task-1"}}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "succeeded" {
			t.Fatalf("result = %+v", result)
		}
	}
	if strings.Join(agents.calls, ",") != "stop:task-1,stop:task-1" {
		t.Fatalf("calls = %v", agents.calls)
	}
}

func TestAgentRunExecutorLaunchesExplicitWorkflowAssignment(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	executor := NewAgentRunExecutor(agents, "/opt/legato current/legato")
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{
		RunID: "wr-1", AttemptID: "wa-1", TaskID: "workflow-task", RelatedTaskID: "task-1", StepID: "review", Type: "agent.run",
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
	if agents.opts.ExtraEnv["LEGATO_TASK_ID"] != "workflow-task" || agents.opts.ExtraEnv["LEGATO_WORKFLOW_RUN_ID"] != "wr-1" || agents.opts.WorkingDir != "/tmp/wt" || agents.opts.AgentKind != "chimera" {
		t.Fatalf("opts = %+v", agents.opts)
	}
	if !strings.Contains(agents.opts.Assignment, "Workflow task ID: workflow-task") {
		t.Fatalf("assignment lacks workflow task ID: %s", agents.opts.Assignment)
	}
	if agents.opts.ExecutionKind != store.ExecutionWorkflowAgent || agents.opts.AttributionTaskID != "task-1" || agents.opts.WorkflowAttemptID != "wa-1" {
		t.Fatalf("execution attribution = %+v", agents.opts)
	}
	if !strings.Contains(agents.opts.Assignment, "'/opt/legato current/legato' workflow step complete") {
		t.Fatalf("assignment lacks exact callback binary: %s", agents.opts.Assignment)
	}
}

func TestAgentRunExecutorStartsTheAgentInAClone(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	_, err := NewAgentRunExecutor(agents).Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", TaskID: "workflow-task", StepID: "agent",
		With: map[string]any{"prompt": "Fix it", "repo": "acme/widget", "ref": "feature/x", "branch": "legato/fix", "working_dir": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if agents.opts.Repo != "acme/widget" || agents.opts.Ref != "feature/x" || agents.opts.Branch != "legato/fix" || agents.opts.WorkingDir != "" {
		t.Fatalf("opts = %+v", agents.opts)
	}
}

func TestAgentRunExecutorStartsTheAgentAsItsEngram(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	executor := NewAgentRunExecutor(agents)
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{
		RunID: "wr-1", AttemptID: "wa-1", TaskID: "workflow-task", StepID: "work", Type: "agent.run",
		With: map[string]any{"prompt": "work", "engram": " mneme "},
	}); err != nil {
		t.Fatal(err)
	}
	if agents.opts.Engram != "mneme" || !agents.opts.EngramRequired {
		t.Fatalf("opts = %+v", agents.opts)
	}
	// Absent: no engram.
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", TaskID: "workflow-task", With: map[string]any{"prompt": "work"}}); err != nil {
		t.Fatal(err)
	}
	if agents.opts.Engram != "" {
		t.Fatalf("an agent with no engram set started as %q", agents.opts.Engram)
	}
}

func TestAgentRunExecutorRefusesAnEngramThatIsNotText(t *testing.T) {
	executor := NewAgentRunExecutor(&fakeWorkflowAgentService{})
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", TaskID: "workflow-task", With: map[string]any{"prompt": "work", "engram": map[string]any{"name": "mneme"}}}); err == nil {
		t.Fatal("an engram that resolved to an object was dropped silently")
	}
}
