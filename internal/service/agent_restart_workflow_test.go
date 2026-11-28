package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// startWorkflowAgent is a run whose agent.run step works on LEG-7 with an
// agent the workflow owns, which has reported its conversation.
func (f restartFixture) startWorkflowAgent(t *testing.T) (*WorkflowService, store.WorkflowStepAttempt) {
	t.Helper()
	sf := newSuspendFixtureOn(t, f, false, true)
	return sf.workflows, sf.show(t).Attempts[0]
}

// LEG-689: a workflow's agent can be reloaded on purpose (after a change to
// its configuration, say): the same attempt goes on, waiting on the new
// agent, which resumes its conversation and is told the step is still open.
func TestReloadingAWorkflowAgentKeepsItsStepAttempt(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	workflows, attempt := f.startWorkflowAgent(t)
	before, err := f.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, before.TaskID, nil); err != nil {
		t.Fatalf("RestartAgent of a workflow agent: %v", err)
	}

	view, err := workflows.Show(ctx, attempt.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attempts) != 1 {
		t.Fatalf("attempts = %+v; want the one attempt kept", view.Attempts)
	}
	got := view.Attempts[0]
	after, err := f.store.GetAgentSessionByTaskID(ctx, before.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != attempt.ID || got.Status != "waiting_agent" || got.AgentSessionID == nil || *got.AgentSessionID != after.ID || after.ID == before.ID {
		t.Fatalf("attempt after reload = %+v (new session %d, old %d); want the same attempt waiting on the new agent", got, after.ID, before.ID)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run = %+v; want it running", view.Run)
	}
	if after.WorkflowRunID != attempt.RunID || after.ExecutionKind != store.ExecutionWorkflowAgent {
		t.Fatalf("reloaded session = %+v; want it the workflow's still", after)
	}
	env := f.tmux.envVarsFor(after.TmuxSession)
	if env["LEGATO_WORKFLOW_ATTEMPT_ID"] != attempt.ID || env["LEGATO_WORKFLOW_RUN_ID"] != attempt.RunID {
		t.Fatalf("env = %v; want the open attempt's completion variables", env)
	}
	sentLineEventually(t, f.tmux, before.TaskID, "env -u CLAUDE_CONFIG_DIR claude --resume conversation-1")
	sentLineEventually(t, f.tmux, before.TaskID, "[legato reloaded] ")
	for _, line := range f.tmux.sentLinesFor(after.TmuxSession) {
		if strings.HasPrefix(line, "[legato reloaded] ") && !strings.Contains(line, "implement") {
			t.Fatalf("reload note %q does not name the open step", line)
		}
	}
}

// A reload whose new agent cannot start fails the step, which Retry starts
// again, rather than leaving it suspended on nothing.
func TestAReloadThatCannotStartFailsTheWorkflowStep(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	workflows, attempt := f.startWorkflowAgent(t)
	before, err := f.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil {
		t.Fatal(err)
	}
	f.tmux.mu.Lock()
	f.tmux.runInPaneErr = errors.New("tmux respawn-pane: lost server")
	f.tmux.mu.Unlock()

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, before.TaskID, nil); err == nil {
		t.Fatal("a reload that could not start reported success")
	}

	view, err := workflows.Show(ctx, attempt.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Attempts[0]; got.Status != "failed" || !strings.Contains(got.Error, "lost server") {
		t.Fatalf("attempt = %+v; want it failed with why", got)
	}
}
