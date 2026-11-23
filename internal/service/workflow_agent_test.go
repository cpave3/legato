package service

import (
	"context"
	"os"
	"path/filepath"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"strings"
	"testing"
	"time"
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

// scratch: true gives the step a folder of its own for the attempt, which
// goes when the step completes; a failed step's stays for a look (LEG-530).
func TestAgentRunScratchFolderLivesForItsStep(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workflow-scratch")
	agents := &fakeWorkflowAgentService{}
	executor := NewAgentRunExecutor(agents)
	executor.SetScratchRoot(root)
	ctx := context.Background()
	start := func(attemptID string) string {
		t.Helper()
		result, err := executor.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-task", StepID: "digest", AttemptID: attemptID, With: map[string]any{"prompt": "digest", "scratch": true}})
		if err != nil {
			t.Fatal(err)
		}
		dir := agents.opts.WorkingDir
		if dir != filepath.Join(root, attemptID) || result.WorkingDir != dir {
			t.Fatalf("working dir = %q (result %q), want a scratch folder of attempt %s", dir, result.WorkingDir, attemptID)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("scratch folder not made: %v", err)
		}
		if !strings.Contains(agents.opts.Assignment, "scratch folder") {
			t.Fatalf("the brief does not say the folder is scratch:\n%s", agents.opts.Assignment)
		}
		return dir
	}

	done := start("wa-1")
	if err := os.WriteFile(filepath.Join(done, "notes.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.Finish(ctx, store.WorkflowStepAttempt{ID: "wa-1", WorkingDir: done}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Fatalf("completed step's scratch folder is still there: %v", err)
	}

	// A failed attempt is never finished: its folder stays, and a retry
	// gets a folder of its own.
	failed := start("wa-2")
	retry := start("wa-3")
	if failed == retry {
		t.Fatal("a retry reused the failed attempt's folder")
	}
	if _, err := os.Stat(failed); err != nil {
		t.Fatalf("failed attempt's folder went: %v", err)
	}

	// Finish never removes a folder that is not a scratch folder.
	other := t.TempDir()
	if err := executor.Finish(ctx, store.WorkflowStepAttempt{ID: "wa-4", WorkingDir: other}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("a working directory that is not scratch was removed: %v", err)
	}

	if _, err := executor.Start(ctx, ResolvedWorkflowStep{AttemptID: "wa-5", With: map[string]any{"prompt": "x", "scratch": true, "working_dir": "/repo"}}); err == nil {
		t.Fatal("scratch with working_dir was accepted")
	}
	unset := NewAgentRunExecutor(agents)
	if _, err := unset.Start(ctx, ResolvedWorkflowStep{AttemptID: "wa-6", With: map[string]any{"prompt": "x", "scratch": true}}); err == nil {
		t.Fatal("scratch without a scratch root was accepted")
	}
}

// A scratch folder kept for a look goes once it is a week old, unless its
// attempt or its kept agent still runs.
func TestKeptScratchFoldersAreSweptAfterAWeek(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, name := range []string{"wa-old", "wa-live", "wa-new"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if name != "wa-new" {
			if err := os.Chtimes(filepath.Join(root, name), old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	executor := NewAgentRunExecutor(&fakeWorkflowAgentService{})
	executor.SetScratchRoot(root, func(_ context.Context, attemptID string) (bool, error) { return attemptID == "wa-live", nil })
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{AttemptID: "wa-next", With: map[string]any{"prompt": "x", "scratch": true}}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"wa-old": false, "wa-live": true, "wa-new": true, "wa-next": true} {
		_, err := os.Stat(filepath.Join(root, name))
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
}
