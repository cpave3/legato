package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// testBrokerEnv stands in for the host broker's session env, its socket in
// a folder of the test's own (the sandbox makes the socket's folder).
func testBrokerEnv(t *testing.T) fixedSessionEnv {
	t.Helper()
	return fixedSessionEnv{"LEGATO_BROKER_SOCKET": filepath.Join(t.TempDir(), "broker", "broker.sock"), "LEGATO_BROKER_TOKEN": "lgb_test"}
}

func withoutBroker(svc AgentService) {
	svc.(interface{ SetBrokerEnv(SessionEnvProvider) }).SetBrokerEnv(nil)
}

// LEG-668: in the sandbox every legato command goes through the broker, so
// a sandboxed agent without one cannot even end its workflow step. It is
// refused before any tmux session exists, with the execution failed.
func TestASandboxedSpawnWithoutTheBrokerIsRefusedBeforeTmux(t *testing.T) {
	svc, s, mt := sandboxedService(t, &fakeBackend{})
	withoutBroker(svc)
	createTask(t, s, "task-1")
	err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkflowRunID: "wr-1"})
	if !errors.Is(err, ErrSandboxNoBroker) {
		t.Fatalf("err = %v, want ErrSandboxNoBroker", err)
	}
	if !strings.Contains(err.Error(), "retry") {
		t.Fatalf("the refusal does not say what to do: %v", err)
	}
	if mt.sessions["legato-task-1"] {
		t.Fatal("a tmux session was made")
	}
	running, err := s.ListRunningExecutions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 0 {
		t.Fatalf("running executions = %+v, want the refused one ended", running)
	}
}

// The broker is required only in the sandbox: an agent outside it reads the
// store itself.
func TestAnUnsandboxedSpawnNeedsNoBroker(t *testing.T) {
	svc, s, mt := sandboxedService(t, &fakeBackend{})
	withoutBroker(svc)
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if !mt.sessions["legato-task-1"] {
		t.Fatal("no tmux session")
	}
}

// catchUpFixture is a daily 09:00 schedule whose run starts an agent, on a
// sandboxed agent service. Its first due time runs (with the broker), then
// Legato is "down" until noon three days on, where the clock is left: a
// start then has missed due times to catch up. It returns the coordinator
// and the first run's ID.
func catchUpFixture(t *testing.T, agents AgentService, s *store.Store) (*WorkflowTriggerCoordinator, string) {
	t.Helper()
	dir := t.TempDir()
	definition := `schema_version: 1
name: errors
title: Errors
inputs:
  at: {type: string, required: true}
on:
  schedule:
    every: daily
    at: "09:00"
    catch_up: once
    inputs:
      at: ${{ schedule.time }}
steps:
  - id: check
    title: Check
    type: agent.run
    with: {prompt: check the errors}
`
	if err := os.WriteFile(filepath.Join(dir, "errors.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))})
	c := NewWorkflowTriggerCoordinator(s, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	c.SetHostZone(time.UTC)
	now := time.Date(2026, 11, 20, 9, 0, 5, 0, time.UTC)
	c.SetClock(func() time.Time { return now })
	if err := c.TickSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	runs := workflowRuns(t, s)
	if len(runs) != 1 {
		t.Fatalf("runs after the first due time = %d", len(runs))
	}
	now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	return c, runs[0].ID
}

func workflowRuns(t *testing.T, s *store.Store) []store.WorkflowRun {
	t.Helper()
	runs, err := s.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

// LEG-668: a missed schedule caught up when Legato starts launches its
// agent with the broker's env.
func TestAScheduleCaughtUpAtStartLaunchesItsAgentWithTheBroker(t *testing.T) {
	backend := &fakeBackend{}
	agents, s, mt := sandboxedService(t, backend)
	c, _ := catchUpFixture(t, agents, s)
	before := map[string]bool{}
	for name := range mt.sessions {
		before[name] = true
	}
	c.StartEventLoop(context.Background(), nil)()
	var launched []string
	for name := range mt.sessions {
		if !before[name] {
			launched = append(launched, name)
		}
	}
	if len(launched) != 1 {
		t.Fatalf("new sessions = %v, want the caught-up run's agent", launched)
	}
	env := mt.envVarsFor(launched[0])
	given, _ := agents.(*agentService).brokerSessionEnv().SessionEnv(context.Background(), "", "")
	for name, want := range given {
		if got := env[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// Before the broker is wired, the caught-up run's step fails where the
// operator sees it, instead of leaving an agent idle with no broker.
func TestAScheduleCaughtUpBeforeTheBrokerFailsItsStep(t *testing.T) {
	backend := &fakeBackend{}
	agents, s, mt := sandboxedService(t, backend)
	c, first := catchUpFixture(t, agents, s)
	withoutBroker(agents)
	sessions := len(mt.sessions)
	c.StartEventLoop(context.Background(), nil)()
	if len(mt.sessions) != sessions {
		t.Fatalf("sessions = %v, want none new", mt.sessions)
	}
	var late string
	for _, run := range workflowRuns(t, s) {
		if run.ID != first {
			late = run.ID
		}
	}
	if late == "" {
		t.Fatal("no caught-up run")
	}
	view, err := s.GetWorkflowRun(context.Background(), late)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Steps) == 0 || view.Steps[0].Status != "failed" || !strings.Contains(view.Steps[0].Error, "broker") {
		t.Fatalf("steps = %+v, want the first failed for the missing broker", view.Steps)
	}
}
