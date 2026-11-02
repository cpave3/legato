package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// recordingCloner makes a folder for each clone asked for.
type recordingCloner struct {
	root     string
	requests []JobCloneRequest
	err      error
}

func (r *recordingCloner) JobClone(_ context.Context, req JobCloneRequest) (store.JobClone, error) {
	r.requests = append(r.requests, req)
	if r.err != nil {
		return store.JobClone{}, r.err
	}
	return store.JobClone{ExecutionID: req.ExecutionID, TaskID: req.TaskID, Repo: req.Repo, Branch: "feature/x",
		Path: filepath.Join(r.root, "jobs", req.ExecutionID, "widget")}, nil
}

func TestSpawningWithARepositoryWorksInAJobCloneOfItsOwnExecution(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	cloner := &recordingCloner{root: t.TempDir()}
	svc.(*agentService).SetRepositories(cloner)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnAgent(ctx, "task1", 80, 24, AgentSpawnOptions{Repo: "acme/widget", Ref: "feature/x"}); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if len(cloner.requests) != 1 {
		t.Fatalf("clones asked for: %+v", cloner.requests)
	}
	req := cloner.requests[0]
	executionID := mt.envVars["legato-task1"]["LEGATO_EXECUTION_ID"]
	if req.ExecutionID != executionID || req.TaskID != "task1" || req.Repo != "acme/widget" || req.Ref != "feature/x" {
		t.Fatalf("clone request %+v; the session's execution is %s", req, executionID)
	}
	want := filepath.Join(cloner.root, "jobs", executionID, "widget")
	if got := mt.spawnDirs["legato-task1"]; got != want {
		t.Fatalf("the agent works in %s, want the clone %s", got, want)
	}
	execution, err := db.GetExecution(ctx, executionID)
	if err != nil || execution.WorkingDir != want {
		t.Fatalf("execution %+v, %v", execution, err)
	}
}

func TestSpawningWithARepositoryFailsWithoutACache(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	err := svc.SpawnAgent(ctx, "task1", 80, 24, AgentSpawnOptions{Repo: "acme/widget"})
	if err == nil || !strings.Contains(err.Error(), "github credential") {
		t.Fatalf("error = %v", err)
	}
	if mt.sessions["legato-task1"] {
		t.Fatal("a tmux session was started")
	}
}

func TestSpawningWithARepositoryThatCannotBeClonedStartsNothing(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	svc.(*agentService).SetRepositories(&recordingCloner{root: t.TempDir(), err: errors.New("acme/widget: not found")})
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task1", 80, 24, AgentSpawnOptions{Repo: "acme/widget"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
	if mt.sessions["legato-task1"] {
		t.Fatal("a tmux session was started")
	}
}

func TestSpawningWithARepositoryAndAWorkingDirectoryIsRefused(t *testing.T) {
	svc, db, _ := newTestAgentService(t)
	svc.(*agentService).SetRepositories(&recordingCloner{root: t.TempDir()})
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task1", 80, 24, AgentSpawnOptions{Repo: "acme/widget", WorkingDir: "/srv"}); err == nil {
		t.Fatal("a spawn with both a repository and a directory was taken")
	}
}

// A job clone ends on a branch of its own: legato/<task>, which the GitHub
// credential lets agents push, unless the spawn names one.
func TestAJobCloneIsOnABranchOfTheTasksOwn(t *testing.T) {
	svc, db, _ := newTestAgentService(t)
	cloner := &recordingCloner{root: t.TempDir()}
	svc.(*agentService).SetRepositories(cloner)
	ctx := context.Background()
	for _, id := range []string{"Task1", "task2"} {
		if err := db.CreateTask(ctx, store.Task{ID: id, Title: "Job", Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SpawnAgent(ctx, "Task1", 80, 24, AgentSpawnOptions{Repo: "acme/widget", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task2", 80, 24, AgentSpawnOptions{Repo: "acme/widget", Ref: "main", Branch: "fix/flaky-sync"}); err != nil {
		t.Fatal(err)
	}
	first, second := cloner.requests[0], cloner.requests[1]
	if first.Branch != "" || first.FreshBranch != "legato/task1" || second.Branch != "fix/flaky-sync" {
		t.Fatalf("requests = %+v, %+v", first, second)
	}
}

// An agent can be started in a job clone another task made, for the next
// branch of a stack.
func TestSpawningInAJobCloneJoinsIt(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	cloner := &recordingCloner{root: t.TempDir()}
	svc.(*agentService).SetRepositories(cloner)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task2", Title: "Part two", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task2", 80, 24, AgentSpawnOptions{Clone: 7, Branch: "stack/two"}); err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if len(cloner.requests) != 1 || cloner.requests[0].CloneID != 7 || cloner.requests[0].Branch != "stack/two" || cloner.requests[0].TaskID != "task2" {
		t.Fatalf("requests = %+v", cloner.requests)
	}
	executionID := mt.envVars["legato-task2"]["LEGATO_EXECUTION_ID"]
	if got := mt.spawnDirs["legato-task2"]; got != filepath.Join(cloner.root, "jobs", executionID, "widget") {
		t.Fatalf("the agent works in %s", got)
	}

	if err := svc.SpawnAgent(ctx, "task3", 80, 24, AgentSpawnOptions{Clone: 7, WorkingDir: "/srv"}); err == nil {
		t.Fatal("a spawn with both a job clone and a directory was taken")
	}
}

// A spawn in the background needs its task first, to answer with it.
func TestAnEphemeralTaskIsMadeBeforeItsAgent(t *testing.T) {
	svc, db, _ := newTestAgentService(t)
	creator, ok := svc.(interface {
		CreateEphemeralTask(context.Context, string) (string, error)
	})
	if !ok {
		t.Fatal("the agent service cannot make an ephemeral task on its own")
	}
	taskID, err := creator.CreateEphemeralTask(context.Background(), "Flaky sync")
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), taskID)
	if err != nil || task.Title != "Flaky sync" || !task.Ephemeral {
		t.Fatalf("task = %+v, %v", task, err)
	}
}
