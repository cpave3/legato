package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// fakeCache records the repositories it was asked to make ready.
type fakeCache struct {
	ensured []string
	err     error
}

func (f *fakeCache) Ensure(_ context.Context, repo string) (string, error) {
	f.ensured = append(f.ensured, repo)
	return "/data/repos/" + repo + ".git", f.err
}

func TestRepoEnsureWithoutRepositoryPassesWorkingDirOn(t *testing.T) {
	result, err := NewRepoEnsureExecutor(nil).Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "", "ref": "", "working_dir": "/srv/jobs",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Outputs["working_dir"] != "/srv/jobs" || result.Outputs["repo"] != "" {
		t.Fatalf("result = %+v", result)
	}
}

// The clone is made when the agent starts, for its execution; this step
// makes the cache ready, so a repository Legato cannot read fails here.
func TestRepoEnsureWithRepositoryReadiesTheCacheAndPassesItOn(t *testing.T) {
	cache := &fakeCache{}
	result, err := NewRepoEnsureExecutor(cache).Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "acme/widget", "ref": "feature/x", "working_dir": "",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.ensured) != 1 || cache.ensured[0] != "acme/widget" {
		t.Fatalf("ensured %v", cache.ensured)
	}
	if result.Status != "succeeded" || result.Outputs["repo"] != "acme/widget" || result.Outputs["ref"] != "feature/x" || result.Outputs["working_dir"] != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRepoEnsureWithRepositoryAndWorkingDirIsRefused(t *testing.T) {
	_, err := NewRepoEnsureExecutor(&fakeCache{}).Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "acme/widget", "ref": "", "working_dir": "/srv/jobs",
	}})
	if err == nil {
		t.Fatal("a job with both a repository and a directory was taken")
	}
}

func TestRepoEnsureWithRepositoryFailsWithoutACache(t *testing.T) {
	_, err := NewRepoEnsureExecutor(nil).Start(context.Background(), ResolvedWorkflowStep{StepID: "checkout", With: map[string]any{
		"repo": "acme/widget", "ref": "", "working_dir": "",
	}})
	if err == nil || !strings.Contains(err.Error(), "acme/widget") || !strings.Contains(err.Error(), "github credential") {
		t.Fatalf("err = %v, want a refusal naming the repository and the credential", err)
	}
}

func TestJobWorkflowGivesTheAgentTheRepository(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewRepoEnsureExecutor(&fakeCache{}), agent})
	if _, err := workflows.Start(context.Background(), BuiltInJobWorkflow(), map[string]string{"prompt": "Fix it", "repo": "acme/widget", "ref": "feature/x", "branch": "legato/fix"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(agent.starts) != 1 {
		t.Fatalf("agent starts = %d", len(agent.starts))
	}
	with := agent.starts[0].With
	if with["repo"] != "acme/widget" || with["ref"] != "feature/x" || with["branch"] != "legato/fix" || with["working_dir"] != "" {
		t.Fatalf("agent step with = %#v", with)
	}
}

func TestJobWorkflowRunsAgentWithPromptInGivenDirectory(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewRepoEnsureExecutor(nil), agent})

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
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewRepoEnsureExecutor(nil), agent})
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
