package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/dockerproxy"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeDockerCleaner records what it is asked to clean.
type fakeDockerCleaner struct {
	mu      sync.Mutex
	present []string
	cleaned []string
	done    chan string
}

func (f *fakeDockerCleaner) Clean(_ context.Context, execution string) error {
	f.mu.Lock()
	f.cleaned = append(f.cleaned, execution)
	f.mu.Unlock()
	if f.done != nil {
		f.done <- execution
	}
	return nil
}

func (f *fakeDockerCleaner) Executions(context.Context) ([]string, error) {
	return f.present, nil
}

func (f *fakeDockerCleaner) cleanedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cleaned)
}

func dockerSandboxedService(t *testing.T, backend *fakeBackend, cleaner *fakeDockerCleaner) (AgentService, *store.Store, *mockTmux, *SandboxDocker) {
	t.Helper()
	var docker *SandboxDocker
	svc, s, mt := sandboxedService(t, backend, func(settings *SandboxSettings) {
		docker = &SandboxDocker{RuntimeDir: settings.Base.RuntimeDir, Cleaner: cleaner}
		settings.Docker = docker
	})
	svc.(*agentService).docker = docker
	return svc, s, mt, docker
}

func TestASandboxedAgentGetsItsOwnDockerSocket(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt, docker := dockerSandboxedService(t, backend, &fakeDockerCleaner{})
	createTask(t, s, "task-1")
	work := t.TempDir()
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: work}); err != nil {
		t.Fatal(err)
	}
	executionID := mt.envVars["legato-task-1"]["LEGATO_EXECUTION_ID"]
	plan := backend.plans[0]
	socket := docker.SocketPath(executionID)
	if got := plan.Setenv["DOCKER_HOST"]; got != "unix://"+socket {
		t.Fatalf("DOCKER_HOST = %q, want the execution's socket %s", got, socket)
	}
	if slices.Contains(plan.Unsetenv, "DOCKER_HOST") || !slices.Contains(plan.Unsetenv, "DOCKER_CONTEXT") {
		t.Fatalf("unset = %v: DOCKER_HOST must stay set, a Docker context must go", plan.Unsetenv)
	}
	// The folder is mounted, so a socket made again by a new host is seen.
	if m, ok := mountOf(plan, filepath.Dir(socket)); !ok || m.Kind != sandbox.ReadOnly {
		t.Fatalf("socket folder mount = %+v, %v", m, ok)
	}
	// The proxy is told what the sandbox shows.
	view, err := docker.View(executionID)
	if err != nil {
		t.Fatal(err)
	}
	host, readOnly, err := view.Resolve(work)
	if err != nil || readOnly {
		t.Fatalf("the worktree in the proxy's view = %s, read-only %v, %v", host, readOnly, err)
	}
}

func TestAnAgentOutsideTheSandboxKeepsDockerAsItIs(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, mt, docker := dockerSandboxedService(t, backend, &fakeDockerCleaner{})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	executionID := mt.envVars["legato-task-1"]["LEGATO_EXECUTION_ID"]
	if _, ok := mt.envVars["legato-task-1"]["DOCKER_HOST"]; ok {
		t.Fatal("an unsandboxed agent was given a DOCKER_HOST")
	}
	if _, err := os.Stat(docker.viewFile(executionID)); !os.IsNotExist(err) {
		t.Fatalf("an unsandboxed agent has a proxy view: %v", err)
	}
}

func TestWhatAnExecutionMadeInDockerIsRemovedWhenItEnds(t *testing.T) {
	cleaner := &fakeDockerCleaner{done: make(chan string, 1)}
	svc, s, mt, _ := dockerSandboxedService(t, &fakeBackend{}, cleaner)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	executionID := mt.envVars["legato-task-1"]["LEGATO_EXECUTION_ID"]
	if err := svc.KillAgent(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-cleaner.done:
		if got != executionID {
			t.Fatalf("cleaned %s, want %s", got, executionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was cleaned when the agent ended")
	}
}

func TestTheSweepCleansEndedExecutionsAndLeavesOthersAlone(t *testing.T) {
	s := newServiceTestStore(t)
	ctx := context.Background()
	createTask(t, s, "task-1")
	for id, status := range map[string]string{"ex-ended": store.ExecutionSucceeded, "ex-running": store.ExecutionRunning} {
		if err := s.CreateExecution(ctx, store.Execution{ID: id, TaskID: "task-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if status != store.ExecutionRunning {
			if err := s.TerminalizeExecution(ctx, id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	cleaner := &fakeDockerCleaner{present: []string{"ex-ended", "ex-running", "ex-another-legatos"}}
	docker := &SandboxDocker{Cleaner: cleaner}
	if n, err := docker.Sweep(ctx, StoreExecutionStatus(s)); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if got := cleaner.cleanedIDs(); !slices.Equal(got, []string{"ex-ended"}) {
		t.Fatalf("cleaned %v, want only the ended execution this Legato knows", got)
	}
}

func TestTheProxysViewIsTheSandboxsMounts(t *testing.T) {
	work, gone := t.TempDir(), filepath.Join(t.TempDir(), "gone")
	plan := sandbox.Plan{
		Home: "/home/me", HomeSource: "/scratch/ex-1/home",
		Mounts: []sandbox.Mount{
			{Kind: sandbox.Symlink, Path: "/bin", Target: "usr/bin"},
			{Kind: sandbox.ReadOnly, Path: "/usr"},
			{Kind: sandbox.ReadWrite, Path: "/scratch/ex-1/tmp", Dest: "/tmp"},
			{Kind: sandbox.ReadWrite, Path: work},
			{Kind: sandbox.Overlay, Path: "/review"},
			{Kind: sandbox.ReadOnly, Path: gone, Optional: true},
			{Kind: sandbox.HideDir, Path: "/home/me/.ssh"},
		},
	}
	got, _ := json.Marshal(dockerViewOf(plan))
	want, _ := json.Marshal(dockerproxy.View{Mounts: []dockerproxy.Mount{
		{Kind: dockerproxy.ReadWrite, Source: "/scratch/ex-1/home", Dest: "/home/me"},
		{Kind: dockerproxy.Symlink, Dest: "/bin", Target: "usr/bin"},
		{Kind: dockerproxy.ReadOnly, Source: "/usr", Dest: "/usr"},
		{Kind: dockerproxy.ReadWrite, Source: "/scratch/ex-1/tmp", Dest: "/tmp"},
		{Kind: dockerproxy.ReadWrite, Source: work, Dest: work},
		// What a throwaway layer covers is seen, but a container may only
		// read it: it would write the host's copy.
		{Kind: dockerproxy.ReadOnly, Source: "/review", Dest: "/review"},
		{Kind: dockerproxy.HideDir, Dest: "/home/me/.ssh"},
	}})
	if string(got) != string(want) {
		t.Fatalf("view =\n%s\nwant\n%s", got, want)
	}
}

// On macOS the agent's HOME is its scratch home, where it is: a container
// may bind that, never the person's home under its name.
func TestTheProxysViewOfAHomeThatIsNotRemapped(t *testing.T) {
	plan := sandbox.Plan{
		Home: "/Users/me", HomeSource: "/scratch/ex-1/home",
		Setenv: map[string]string{"HOME": "/scratch/ex-1/home"},
		Mounts: []sandbox.Mount{{Kind: sandbox.ReadWrite, Path: "/Users/me/Projects/app"}},
	}
	view := dockerViewOf(plan)
	if len(view.Mounts) == 0 || view.Mounts[0] != (dockerproxy.Mount{Kind: dockerproxy.ReadWrite, Source: "/scratch/ex-1/home", Dest: "/scratch/ex-1/home"}) {
		t.Fatalf("view %+v", view.Mounts)
	}
	for _, m := range view.Mounts {
		if m.Dest == "/Users/me" {
			t.Fatalf("the person's home is in the view: %+v", m)
		}
	}
}
