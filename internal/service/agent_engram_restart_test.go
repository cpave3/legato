package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
)

func (f restartFixture) withEngrams(t *testing.T) *fakeEngramCLI {
	t.Helper()
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{{Name: "mneme"}}, boot: engram.BootResult{Packet: "# mneme\n"}}
	engrams, _ := bootService(cli)
	f.svc.(interface{ SetEngrams(AgentEngrams) }).SetEngrams(engrams)
	return cli
}

func TestRestartingAnEngramAgentKeepsItsEngramAndSession(t *testing.T) {
	f := newRestartFixture(t)
	cli := f.withEngrams(t)
	ctx := context.Background()
	createTask(t, f.store, "task-r")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.GetAgentSessionByTaskID(ctx, "task-r")
	f.reportConversation(t, "task-r", cwd)

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-r", nil); err != nil {
		t.Fatal(err)
	}
	after, _ := f.store.GetAgentSessionByTaskID(ctx, "task-r")
	name, session, err := f.store.ExecutionEngram(ctx, after.ExecutionID)
	if err != nil || name != "mneme" || session != before.ExecutionID {
		t.Fatalf("restarted execution engram = %q %q %v, want mneme in session %s", name, session, err, before.ExecutionID)
	}
	if cli.boots != 1 {
		t.Fatalf("booted %d times: a resumed restart gets no packet", cli.boots)
	}
	if len(cli.ended) != 0 {
		t.Fatalf("a resumed restart ended the engram session: %v", cli.ended)
	}
	if env := f.tmux.envVarsFor("legato-task-r"); env["ENGRAM_SESSION"] != before.ExecutionID || env["ENGRAM_IDENTITY"] != "mneme" {
		t.Fatalf("restarted env = %v", env)
	}
}

func TestKillingAnEngramAgentEndsItsEngramSession(t *testing.T) {
	f := newRestartFixture(t)
	cli := f.withEngrams(t)
	ctx := context.Background()
	createTask(t, f.store, "task-k")
	if err := f.svc.SpawnAgent(ctx, "task-k", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir(), Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	session, _ := f.store.GetAgentSessionByTaskID(ctx, "task-k")
	if err := f.svc.KillAgent(ctx, "task-k"); err != nil {
		t.Fatal(err)
	}
	if len(cli.ended) != 1 {
		t.Fatalf("ended %d engram sessions, want 1", len(cli.ended))
	}
	env := envOf(cli.ended[0])
	if env["ENGRAM_IDENTITY"] != "mneme" || env["ENGRAM_SESSION"] != session.ExecutionID {
		t.Fatalf("session end env = %v", env)
	}
}

func TestAnAgentWithoutAnEngramEndsNoEngramSession(t *testing.T) {
	f := newRestartFixture(t)
	cli := f.withEngrams(t)
	ctx := context.Background()
	createTask(t, f.store, "task-n")
	if err := f.svc.SpawnAgent(ctx, "task-n", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.KillAgent(ctx, "task-n"); err != nil {
		t.Fatal(err)
	}
	if len(cli.ended) != 0 {
		t.Fatalf("ended %v", cli.ended)
	}
}

func TestAnEngramAgentWhosePaneEndedEndsItsEngramSession(t *testing.T) {
	f := newRestartFixture(t)
	cli := f.withEngrams(t)
	ctx := context.Background()
	createTask(t, f.store, "task-d")
	if err := f.svc.SpawnAgent(ctx, "task-d", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir(), Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	f.tmux.Kill("legato-task-d")
	if err := f.svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if len(cli.ended) != 1 {
		t.Fatalf("ended %d engram sessions, want 1", len(cli.ended))
	}
}
