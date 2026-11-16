package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// hostNetworkFixture is an agent service with Legato's sandbox and its
// filtered Docker socket on, as a restartFixture.
func hostNetworkFixture(t *testing.T) (restartFixture, *SandboxDocker) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	svc, s, mt, docker := dockerSandboxedService(t, &fakeBackend{}, &fakeDockerCleaner{})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	return restartFixture{svc: svc, store: s, tmux: mt, bus: &recordingPublisher{}}, docker
}

func (f restartFixture) execution(t *testing.T, taskID string) *store.AgentSession {
	t.Helper()
	session, err := f.store.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestAnAgentMayBeStartedWithTheHostNetworkForItsContainers(t *testing.T) {
	f, docker := hostNetworkFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "on")
	createTask(t, f.store, "off")
	if err := f.svc.SpawnAgent(ctx, "on", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir(), DockerHostNetwork: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "off", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for task, want := range map[string]bool{"on": true, "off": false} {
		session := f.execution(t, task)
		if session.DockerHostNetwork != want || docker.HostNetwork(session.ExecutionID) != want {
			t.Errorf("%s: saved %v, proxy told %v; want %v", task, session.DockerHostNetwork, docker.HostNetwork(session.ExecutionID), want)
		}
	}
	agents, err := f.svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.DockerHostNetwork != (a.TaskID == "on") {
			t.Errorf("%s listed with DockerHostNetwork %v", a.TaskID, a.DockerHostNetwork)
		}
	}
}

// Asking for it where it cannot apply is refused, not ignored: the caller
// would otherwise believe the agent has it.
func TestTheHostNetworkIsRefusedWhereItCannotApply(t *testing.T) {
	f, _ := hostNetworkFixture(t)
	ctx := context.Background()
	reeve, err := f.store.CreateReeveTask(ctx, "The Reeve")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		task string
		opt  AgentSpawnOptions
	}{
		"outside the sandbox": {"t-1", AgentSpawnOptions{Unsandboxed: true}},
		"the Reeve":           {reeve, AgentSpawnOptions{}},
		"a swarm worker":      {"t-2", AgentSpawnOptions{ParentTaskID: "t-parent", SubtaskID: "st-1", Role: "worker"}},
		"a workflow's agent":  {"t-3", AgentSpawnOptions{WorkflowRunID: "run-1"}},
		"an unattended agent": {"t-4", AgentSpawnOptions{Unattended: true}},
		"a read-only agent":   {"t-5", AgentSpawnOptions{ReadOnlyCheckout: true}},
	} {
		if tc.task != reeve {
			createTask(t, f.store, tc.task)
		}
		tc.opt.DockerHostNetwork = true
		tc.opt.WorkingDir = t.TempDir()
		err := f.svc.SpawnAgent(ctx, tc.task, 0, 0, tc.opt)
		if !errors.Is(err, ErrDockerHostNetwork) {
			t.Errorf("%s: err = %v, want ErrDockerHostNetwork", name, err)
		}
		if f.tmux.sessionAlive("legato-" + tc.task) {
			t.Errorf("%s: an agent was started", name)
		}
	}
	// With the sandbox but no Docker in it there is nothing to allow.
	svc, s, mt := sandboxedService(t, &fakeBackend{})
	createTask(t, s, "t-6")
	if err := svc.SpawnAgent(ctx, "t-6", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir(), DockerHostNetwork: true}); !errors.Is(err, ErrDockerHostNetwork) || mt.sessionAlive("legato-t-6") {
		t.Fatalf("without Docker in the sandbox: %v", err)
	}
}

func TestAReloadKeepsTheHostNetworkOrChangesIt(t *testing.T) {
	f, docker := hostNetworkFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-r")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, DockerHostNetwork: true}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, "task-r", cwd)

	// A plain reload keeps it.
	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-r", nil); err != nil {
		t.Fatal(err)
	}
	if s := f.execution(t, "task-r"); !s.DockerHostNetwork || !docker.HostNetwork(s.ExecutionID) {
		t.Fatalf("after a reload: saved %v, proxy %v", s.DockerHostNetwork, docker.HostNetwork(s.ExecutionID))
	}

	// Turning it off starts a new execution without it, and says so.
	f.reportConversation(t, "task-r", cwd)
	before := f.execution(t, "task-r").ExecutionID
	f.clearSent()
	if err := f.svc.(DockerHostNetworkSwitcher).RestartAgentSettingDockerHostNetwork(ctx, "task-r", nil, false); err != nil {
		t.Fatal(err)
	}
	s := f.execution(t, "task-r")
	if s.ExecutionID == before || s.DockerHostNetwork || docker.HostNetwork(s.ExecutionID) {
		t.Fatalf("after turning it off: execution %s (was %s), saved %v", s.ExecutionID, before, s.DockerHostNetwork)
	}
	if note := sentLineEventually(t, f.tmux, "task-r", "[legato sandbox] "); !strings.Contains(note, "host network") {
		t.Fatalf("note = %q, want it to say the host network was taken away", note)
	}

	// And on again.
	f.reportConversation(t, "task-r", cwd)
	if err := f.svc.(DockerHostNetworkSwitcher).RestartAgentSettingDockerHostNetwork(ctx, "task-r", nil, true); err != nil {
		t.Fatal(err)
	}
	if s := f.execution(t, "task-r"); !s.DockerHostNetwork {
		t.Fatal("turning it on did not keep it")
	}

	// Leaving the sandbox leaves the filtered socket, and the option with it.
	f.reportConversation(t, "task-r", cwd)
	if _, err := f.svc.(SandboxSwitcher).RestartAgentSwitchingSandbox(ctx, "task-r", nil); err != nil {
		t.Fatal(err)
	}
	if s := f.execution(t, "task-r"); s.Sandboxed || s.DockerHostNetwork {
		t.Fatalf("outside the sandbox: sandboxed %v, host network %v", s.Sandboxed, s.DockerHostNetwork)
	}
	f.reportConversation(t, "task-r", cwd)
	if err := f.svc.(DockerHostNetworkSwitcher).RestartAgentSettingDockerHostNetwork(ctx, "task-r", nil, true); !errors.Is(err, ErrDockerHostNetwork) {
		t.Fatalf("turning it on outside the sandbox: %v", err)
	}
}

func TestResumingAnInterruptedAgentKeepsTheHostNetwork(t *testing.T) {
	f, _ := hostNetworkFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	f.interruptedAgent(t, "task-i", cwd, true, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, DockerHostNetwork: true})
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); err != nil {
		t.Fatal(err)
	}
	if s := f.execution(t, "task-i"); !s.DockerHostNetwork {
		t.Fatal("the resumed agent lost the host network")
	}
}
