package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// launchStepFixture is a run of one agent.run step, its agent started by
// the real agent service in a fake tmux, with a tool that launches.
type launchStepFixture struct {
	agents    AgentService
	store     *store.Store
	tmux      *mockTmux
	workflows *WorkflowService
	runner    *WorkflowAgentRunner
	def       *workflowengine.Definition
}

func newLaunchStepFixture(t *testing.T) launchStepFixture {
	t.Helper()
	s := newLaunchTestStore(t)
	mt := newMockTmux()
	agents := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{
		name:     "fake",
		launchFn: func(map[string]string, string, string, string) string { return "fake-tool" },
	}, DisablePeerAwareness: true})
	runner := NewWorkflowAgentRunner(agents, s)
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(runner)})
	def, err := workflowengine.Parse("launch.yaml", []byte(`
schema_version: 1
name: launch
title: Launch
steps:
 - id: run
   title: Run
   type: agent.run
   with: {prompt: Work}
`))
	if err != nil {
		t.Fatal(err)
	}
	return launchStepFixture{agents: agents, store: s, tmux: mt, workflows: workflows, runner: runner, def: def}
}

func (f launchStepFixture) show(t *testing.T, runID string) *store.WorkflowRunView {
	t.Helper()
	view, err := f.workflows.Show(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (f launchStepFixture) launches() int {
	f.tmux.mu.Lock()
	defer f.tmux.mu.Unlock()
	n := 0
	for _, l := range f.tmux.paneLaunches {
		n += len(l)
	}
	return n
}

// LEG-689: a launch that failed left the step waiting on a shell that never
// ran anything, which Retry could not reach. It fails the step, and Retry
// launches afresh.
func TestALaunchThatFailsFailsItsStepAndRetryLaunchesAgain(t *testing.T) {
	f := newLaunchStepFixture(t)
	ctx := context.Background()
	f.tmux.runInPaneErr = errors.New("tmux respawn-pane: lost server")
	view, err := f.workflows.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}
	view = f.show(t, view.Run.ID)
	if got := view.Attempts[0]; got.Status != "failed" || !strings.Contains(got.Error, "lost server") {
		t.Fatalf("attempt = %+v; want it failed with the launch's error", got)
	}
	f.tmux.mu.Lock()
	f.tmux.runInPaneErr = nil
	f.tmux.mu.Unlock()
	view, err = f.workflows.Retry(ctx, view.Run.ID, "run", false)
	if err != nil {
		t.Fatalf("Retry after a failed launch: %v", err)
	}
	if last := view.Attempts[len(view.Attempts)-1]; last.Status != "waiting_agent" || f.launches() != 1 {
		t.Fatalf("after retry: attempt %+v, launches %d; want a fresh launch waiting", last, f.launches())
	}
}

// Stopping the agent of a waiting step fails the step, so Retry reaches it;
// before, the step waited on a session that no longer existed.
func TestStoppingAWaitingStepsAgentFailsTheStep(t *testing.T) {
	f := newLaunchStepFixture(t)
	ctx := context.Background()
	view, err := f.workflows.Start(ctx, f.def, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" {
		t.Fatalf("attempt = %+v", attempt)
	}
	agentID, ok := f.runner.AttemptAgentID(ctx, attempt)
	if !ok {
		t.Fatal("no agent for the attempt")
	}
	if err := f.agents.KillAgent(ctx, agentID); err != nil {
		t.Fatal(err)
	}
	view = f.show(t, view.Run.ID)
	if got := view.Attempts[0]; got.Status != "failed" || got.Error != WorkflowAgentStoppedReason {
		t.Fatalf("attempt after the stop = %+v; want failed: %s", got, WorkflowAgentStoppedReason)
	}
	view, err = f.workflows.Retry(ctx, view.Run.ID, "run", false)
	if err != nil {
		t.Fatalf("Retry after the stop: %v", err)
	}
	if last := view.Attempts[len(view.Attempts)-1]; last.Status != "waiting_agent" || f.launches() != 2 {
		t.Fatalf("after retry: attempt %+v, launches %d; want a fresh launch", last, f.launches())
	}
}

// A workflow stopping its own agent (a cancel, a fail) is not the operator
// stopping it: the run ends as the workflow says.
func TestAWorkflowStoppingItsOwnAgentEndsTheRunItsWay(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		f := newLaunchStepFixture(t)
		ctx := context.Background()
		view, err := f.workflows.Start(ctx, f.def, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.workflows.Cancel(ctx, view.Run.ID, "not needed"); err != nil {
			t.Fatal(err)
		}
		if view = f.show(t, view.Run.ID); view.Run.Status != "cancelled" {
			t.Fatalf("run = %+v; want cancelled", view.Run)
		}
	})
	t.Run("fail", func(t *testing.T) {
		f := newLaunchStepFixture(t)
		ctx := context.Background()
		view, err := f.workflows.Start(ctx, f.def, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.workflows.Fail(ctx, view.Run.ID, "run", view.Attempts[0].ID, "review failed"); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		if got := f.show(t, view.Run.ID).Attempts[0]; got.Status != "failed" || got.Error != "review failed" {
			t.Fatalf("attempt = %+v; want failed with the workflow's reason", got)
		}
	})
}
