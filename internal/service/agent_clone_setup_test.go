package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// seedingCloner makes the clone's folder with files, as a clone of a
// repository that has them. A joined clone was made by another execution.
type seedingCloner struct {
	root   string
	files  map[string]string
	joined bool
}

func (c *seedingCloner) JobClone(_ context.Context, req JobCloneRequest) (store.JobClone, error) {
	execution := req.ExecutionID
	if c.joined {
		execution = "ex-earlier"
	}
	path := filepath.Join(c.root, "jobs", execution, "widget")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return store.JobClone{}, err
	}
	for name, content := range c.files {
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0o644); err != nil {
			return store.JobClone{}, err
		}
	}
	return store.JobClone{ExecutionID: execution, TaskID: req.TaskID, Repo: req.Repo, Ref: req.Ref, Branch: "legato/task-1", Path: path}, nil
}

func spawnInClone(t *testing.T, cloner *seedingCloner, primary string, opts AgentSpawnOptions) (*fakeBackend, []string) {
	t.Helper()
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	svc.(*agentService).SetRepositories(cloner)
	svc.(*agentService).SetPrimaryDirs(func(repo string) string {
		if repo == "acme/widget" {
			return primary
		}
		return ""
	})
	createTask(t, s, "task-1")
	opts.Repo = "acme/widget"
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, opts); err != nil {
		t.Fatal(err)
	}
	return backend, mt.sentLinesFor("legato-task-1")
}

const setupToml = "[setup]\nrun = [\"bun install\"]\ncopy = [\".env.local\"]\nlink = [\"local.settings.json\"]\n"

func TestAFreshCloneIsSetUpInTheSandboxBeforeTheAgentStarts(t *testing.T) {
	primary := t.TempDir()
	_ = os.WriteFile(filepath.Join(primary, ".env.local"), []byte("SECRET=1"), 0o600)
	_ = os.WriteFile(filepath.Join(primary, "local.settings.json"), []byte("{}"), 0o644)
	cloner := &seedingCloner{root: t.TempDir(), files: map[string]string{".legato.toml": setupToml}}

	backend, lines := spawnInClone(t, cloner, primary, AgentSpawnOptions{Ref: "release/2"})

	if len(lines) == 0 {
		t.Fatal("nothing typed")
	}
	parts := strings.Split(lines[0], "; ")
	setup, launch, ok := parts[0], parts[len(parts)-1], len(parts) == 3
	if !ok || !strings.HasPrefix(setup, "SANDBOX --args 3 3<") || !strings.Contains(setup, " -- ") || !strings.Contains(setup, "clone setup") {
		t.Fatalf("setup = %q; want it run in the sandbox, then the launch", lines[0])
	}
	if !strings.Contains(setup, "--base release/2") || !strings.Contains(setup, "--primary "+primary) {
		t.Errorf("setup = %q, want the base and the main checkout", setup)
	}
	if !strings.HasPrefix(launch, "SANDBOX --args 3 3<") || !strings.HasSuffix(launch, " -- fake-tool --in-sandbox") {
		t.Errorf("launch = %q", launch)
	}
	clone := filepath.Join(cloner.root, "jobs")
	matches, _ := filepath.Glob(filepath.Join(clone, "*", "widget", ".env.local"))
	if len(matches) != 1 {
		t.Fatal(".env.local not copied into the clone")
	}
	if target, err := os.Readlink(filepath.Join(filepath.Dir(matches[0]), "local.settings.json")); err != nil || target != filepath.Join(primary, "local.settings.json") {
		t.Errorf("link = %q, %v", target, err)
	}
	// What the clone links to is seen in the sandbox, read-only.
	readable := false
	for _, plan := range backend.plans {
		for _, m := range plan.Mounts {
			readable = readable || m.Path == filepath.Join(primary, "local.settings.json")
		}
	}
	if !readable {
		t.Error("the linked file is not in the sandbox")
	}
}

func TestOnlyAFreshCloneIsSetUp(t *testing.T) {
	cases := map[string]*seedingCloner{
		"a clone joined":       {joined: true, files: map[string]string{".legato.toml": setupToml}},
		"a clone with no file": {files: map[string]string{"README.md": "hi"}},
	}
	for name, cloner := range cases {
		t.Run(name, func(t *testing.T) {
			cloner.root = t.TempDir()
			_, lines := spawnInClone(t, cloner, "", AgentSpawnOptions{})
			if len(lines) == 0 || strings.Contains(lines[0], "clone setup") {
				t.Errorf("launch = %v", lines)
			}
		})
	}
}

func TestTheAgentIsToldWhereTheSetupLogIs(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	cloner := &seedingCloner{root: t.TempDir(), files: map[string]string{".legato.toml": "[setup]\nrun = [\"bun install\"]\n"}}
	svc.(*agentService).SetRepositories(cloner)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := os.ReadFile(mt.envVarsFor("legato-task-1")["LEGATO_ROLE_PROMPT_FILE"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), ".legato.toml") || !strings.Contains(string(prompt), filepath.Join(".git", "legato-setup.log")) {
		t.Errorf("prompt does not mention the setup:\n%s", prompt)
	}
}

// fakeClonePath is the clone a seedingCloner made for a spawn.
func freshClonePath(t *testing.T, root string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(root, "jobs", "ex-*", "widget"))
	if len(matches) != 1 {
		t.Fatalf("clones: %v", matches)
	}
	return matches[0]
}

func TestTheShellRecordsHowTheSetupEndedBeforeTheAgentStarts(t *testing.T) {
	cloner := &seedingCloner{root: t.TempDir(), files: map[string]string{".legato.toml": "[setup]\nrun = [\"bun install\"]\n"}}

	_, lines := spawnInClone(t, cloner, "", AgentSpawnOptions{})

	status := filepath.Join(freshClonePath(t, cloner.root), ".git", "legato-setup.status")
	parts := strings.Split(lines[0], "; ")
	if len(parts) != 3 || !strings.Contains(parts[0], "clone setup") || parts[1] != "echo $? > "+shellWord(status) || !strings.HasSuffix(parts[2], "fake-tool --in-sandbox") {
		t.Fatalf("line = %q; want the setup, its status written by the host shell, then the launch", lines[0])
	}
}

func TestTheFirstMessagesWaitForTheSetup(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	cloner := &seedingCloner{root: t.TempDir(), files: map[string]string{".legato.toml": "[setup]\nrun = [\"bun install\"]\n"}}
	svc.(*agentService).SetRepositories(cloner)
	svc.(*agentService).briefKickoffDelay = time.Millisecond
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Repo: "acme/widget", Brief: "fix the bug"}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if n := len(mt.sentLinesFor("legato-task-1")); n != 1 {
		t.Fatalf("typed %v while the setup ran", mt.sentLinesFor("legato-task-1"))
	}
	status := filepath.Join(freshClonePath(t, cloner.root), ".git", "legato-setup.status")
	if err := os.MkdirAll(filepath.Dir(status), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(status, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(mt.sentLinesFor("legato-task-1")) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the first message never came once the setup ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAwaitingASetupEndsWithTheSetupOrWithTheAgent(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt := sandboxedService(t, backend)
	cloner := &seedingCloner{root: t.TempDir(), files: map[string]string{".legato.toml": "[setup]\nrun = [\"bun install\"]\n"}}
	svc.(*agentService).SetRepositories(cloner)
	createTask(t, s, "task-1")
	createTask(t, s, "task-2")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	awaiter := svc.(*agentService)

	// No setup: nothing to wait for.
	if err := awaiter.AwaitCloneSetup(ctx, "task-2"); err != nil {
		t.Fatalf("a task with no setup: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- awaiter.AwaitCloneSetup(ctx, "task-1") }()
	select {
	case err := <-done:
		t.Fatalf("returned before the setup ended: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// The agent's pane goes away: there is no setup left to wait for.
	mt.Kill("legato-task-1")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("still waiting after the agent's pane went")
	}
}
