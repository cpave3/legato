package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// An agent's list entry says the directory it was started in: its
// execution's working directory (LEG-220).
func TestListedAgentsSayTheDirectoryTheyStartedIn(t *testing.T) {
	svc, db, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, db, "task-dir")
	dir := t.TempDir()
	if err := svc.SpawnAgent(ctx, "task-dir", 80, 24, AgentSpawnOptions{WorkingDir: dir, AgentKind: "shell"}); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	if agents[0].WorkingDir != dir {
		t.Fatalf("WorkingDir = %q, want %q", agents[0].WorkingDir, dir)
	}
	if agents[0].CloneRepo != "" || agents[0].CloneBranch != "" {
		t.Fatalf("an agent in a plain folder has a clone: %+v", agents[0])
	}
}

// An agent in a job clone says the clone's repository and branch beside its
// directory.
func TestListedAgentsInAJobCloneSayItsRepositoryAndBranch(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	cloner := &recordingCloner{root: t.TempDir()}
	svc.(*agentService).SetRepositories(cloner)
	ctx := context.Background()
	createTask(t, db, "task-clone")
	if err := svc.SpawnAgent(ctx, "task-clone", 80, 24, AgentSpawnOptions{Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	executionID := mt.envVars["legato-task-clone"]["LEGATO_EXECUTION_ID"]
	path := filepath.Join(cloner.root, "jobs", executionID, "widget")
	if _, err := db.CreateJobClone(ctx, store.JobClone{ExecutionID: executionID, TaskID: "task-clone", Repo: "acme/widget", Branch: "legato/task-clone", Path: path, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	got := agents[0]
	if got.WorkingDir != path || got.CloneRepo != "acme/widget" || got.CloneBranch != "legato/task-clone" {
		t.Fatalf("agent = dir %q, clone %q on %q", got.WorkingDir, got.CloneRepo, got.CloneBranch)
	}
}
