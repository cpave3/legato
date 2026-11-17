package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// A handed-off agent the operator restarts on the worker comes back as the
// handoff started it: sandboxed, prompts off, allowed to push its branches.
func TestRestartingAHandedOffAgentKeepsItUnattendedAndSandboxed(t *testing.T) {
	ctx := context.Background()
	worker, away, _ := handOffAndSettle(t, service.HandoffMakeChange)
	card := away.WorkerTaskID
	first, err := worker.Store.GetAgentSessionByTaskID(ctx, card)
	if err != nil {
		t.Fatal(err)
	}
	clone := worker.Tmux.Spawned()[0].WorkDir
	if _, err := worker.Store.RecordTaskNativeSession(ctx, card, first.ID, first.ExecutionID, "claude-code", "author-conversation", clone, ""); err != nil {
		t.Fatal(err)
	}

	// A reload that attaches a resource keeps it.
	docs := service.Resource{Name: "design-docs", Paths: []service.ResourcePath{{Path: t.TempDir(), Access: "read"}}}
	restarter := worker.Agents.(service.AgentRestarter)
	if err := restarter.RestartAgent(ctx, card, []service.Resource{docs}); err != nil {
		t.Fatalf("restarting: %v", err)
	}
	if attached, err := worker.Agents.(interface {
		AttachedResources(context.Context, string) ([]string, error)
	}).AttachedResources(ctx, card); err != nil || len(attached) != 1 || attached[0] != "design-docs" {
		t.Fatalf("attached after the restart: %v, %v", attached, err)
	}

	spawns := worker.Tmux.Spawned()
	restarted := spawns[len(spawns)-1]
	if restarted.WorkDir != clone || !strings.Contains(strings.Join(restarted.Env, "\n"), "LEGATO_RESUME_SESSION_ID=author-conversation") {
		t.Fatalf("restarted in %s with env %v", restarted.WorkDir, restarted.Env)
	}
	if typed := strings.Join(worker.Tmux.Typed(restarted.Name), "\n"); !strings.Contains(typed, "--claude-code-prompts-off") {
		t.Fatalf("launch = %q; want its prompts still off", typed)
	}
	session, err := worker.Store.GetAgentSessionByTaskID(ctx, card)
	if err != nil || session.ExecutionID == first.ExecutionID || !session.Sandboxed {
		t.Fatalf("restarted session = %+v, %v", session, err)
	}
	grants, _ := worker.Credentials.Grants(ctx)
	granted := false
	for _, g := range grants {
		granted = granted || g.ExecutionID == session.ExecutionID
	}
	if !granted {
		t.Fatalf("grants %+v; want the restarted session's branches granted again", grants)
	}
	eventually(t, "the reload note", func() bool {
		return strings.Contains(strings.Join(worker.Tmux.Typed(restarted.Name), "\n"), "[legato reloaded]")
	})
}

func TestAHandedOffAgentCannotLeaveTheSandbox(t *testing.T) {
	ctx := context.Background()
	worker, away, _ := handOffAndSettle(t, service.HandoffMakeChange)
	card := away.WorkerTaskID
	spawned := len(worker.Tmux.Spawned())

	_, err := worker.Agents.(service.SandboxSwitcher).RestartAgentSwitchingSandbox(ctx, card, nil)
	if !errors.Is(err, service.ErrAgentNotRestartable) || !strings.Contains(err.Error(), "handed off from workstation") {
		t.Fatalf("switching the sandbox: %v", err)
	}
	if err := worker.Agents.(service.DockerHostNetworkSwitcher).RestartAgentSettingDockerHostNetwork(ctx, card, nil, true); !errors.Is(err, service.ErrAgentNotRestartable) {
		t.Fatalf("turning the host network on: %v", err)
	}
	if len(worker.Tmux.Spawned()) != spawned {
		t.Fatal("the agent was started again")
	}
	if session, err := worker.Store.GetAgentSessionByTaskID(ctx, card); err != nil || !session.Sandboxed {
		t.Fatalf("session %+v, %v; want it still running in the sandbox", session, err)
	}
}

// A handed-off agent with no saved conversation cannot come back as the
// handoff starts it: the restart is refused before the agent is stopped.
func TestAHandedOffAgentThatCannotBeResumedIsLeftRunning(t *testing.T) {
	ctx := context.Background()
	worker, away, _ := handOffAndSettle(t, service.HandoffMakeChange)
	card := away.WorkerTaskID
	before, err := worker.Store.GetAgentSessionByTaskID(ctx, card)
	if err != nil {
		t.Fatal(err)
	}

	err = worker.Agents.(service.AgentRestarter).RestartAgent(ctx, card, nil)
	if !errors.Is(err, service.ErrAgentNotRestartable) {
		t.Fatalf("restarting with no saved conversation: %v", err)
	}
	if after, err := worker.Store.GetAgentSessionByTaskID(ctx, card); err != nil || after.ExecutionID != before.ExecutionID {
		t.Fatalf("session after the refusal %+v, %v; want the same one running", after, err)
	}
}
