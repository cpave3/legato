package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestRepoEnsureWithoutRepositoryPassesWorkingDirOn(t *testing.T) {
	result, err := NewRepoEnsureExecutor().Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "", "ref": "", "working_dir": "/srv/jobs",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Outputs["working_dir"] != "/srv/jobs" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRepoEnsureWithRepositoryFailsUntilCheckoutsExist(t *testing.T) {
	_, err := NewRepoEnsureExecutor().Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "acme/widget", "ref": "", "working_dir": "",
	}})
	if err == nil || !strings.Contains(err.Error(), "acme/widget") {
		t.Fatalf("err = %v, want a refusal naming the repository", err)
	}
}

func TestJobWorkflowRunsAgentWithPromptInGivenDirectory(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewRepoEnsureExecutor(), agent})

	view, err := workflows.Start(context.Background(), BuiltInJobWorkflow(), map[string]string{"prompt": "Fix the flaky test", "working_dir": "/srv/jobs"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(agent.starts) != 1 {
		t.Fatalf("agent starts = %d, run = %+v", len(agent.starts), view.Run)
	}
	with := agent.starts[0].With
	if !strings.Contains(with["prompt"].(string), "Fix the flaky test") || with["working_dir"] != "/srv/jobs" || with["agent"] != "" || with["keep_agent"] != true {
		t.Fatalf("agent step with = %#v", with)
	}
	if view.Run.WorkflowName != "legato-job" {
		t.Fatalf("workflow name = %q", view.Run.WorkflowName)
	}
}

func TestJobWorkflowRejectsUnknownAgentAtStart(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewRepoEnsureExecutor(), agent})
	workflows.SetAgentKinds([]string{"claude-code", "codex"})

	_, err = workflows.Start(context.Background(), BuiltInJobWorkflow(), map[string]string{"prompt": "Fix it", "agent": "claud-code"})
	if err == nil || !strings.Contains(err.Error(), "claud-code") || !strings.Contains(err.Error(), "claude-code") {
		t.Fatalf("err = %v, want the unknown agent and the known ones named", err)
	}
	runs, err := db.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 || len(agent.starts) != 0 {
		t.Fatalf("runs = %d, agent starts = %d; want nothing started", len(runs), len(agent.starts))
	}
	if _, err := workflows.Start(context.Background(), BuiltInJobWorkflow(), map[string]string{"prompt": "Fix it", "agent": "codex"}); err != nil {
		t.Fatalf("known agent: %v", err)
	}
}
