package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
)

// An engram in a store of its own (the Reeve's) is ended, and comes back
// on a restart, in that store.
func TestAnEngramInAStoreOfItsOwnStaysInItThroughKillAndRestart(t *testing.T) {
	f := newRestartFixture(t)
	cli := f.withEngrams(t)
	cli.ids = append(cli.ids, engram.Identity{Name: "reeve-1a2b3c4d"})
	ctx := context.Background()
	createTask(t, f.store, "task-h")
	cwd := t.TempDir()
	home := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-h", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, Engram: "reeve-1a2b3c4d", EngramHome: home}); err != nil {
		t.Fatal(err)
	}
	if env := f.tmux.envVarsFor("legato-task-h"); env["ENGRAM_HOME"] != home {
		t.Fatalf("session ENGRAM_HOME = %q, want %q", env["ENGRAM_HOME"], home)
	}
	f.reportConversation(t, "task-h", cwd)
	cli.home = ""
	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-h", nil); err != nil {
		t.Fatal(err)
	}
	if env := f.tmux.envVarsFor("legato-task-h"); env["ENGRAM_HOME"] != home || env["ENGRAM_IDENTITY"] != "reeve-1a2b3c4d" {
		t.Fatalf("restarted env = %v", env)
	}
	cli.home = ""
	if err := f.svc.KillAgent(ctx, "task-h"); err != nil {
		t.Fatal(err)
	}
	if len(cli.ended) != 1 || envOf(cli.ended[0])["ENGRAM_HOME"] != home {
		t.Fatalf("ended = %v, want one session ended in %s", cli.ended, home)
	}
}

func TestASpawnWithASeedIssuesItsEngramAndBootsIt(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	home := t.TempDir()
	if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "reeve-1a2b3c4d", EngramHome: home, EngramSeed: "You keep this Legato."}); err != nil {
		t.Fatal(err)
	}
	if f.cli.issued["reeve-1a2b3c4d"] != "You keep this Legato." {
		t.Fatalf("issued = %v", f.cli.issued)
	}
	if !strings.HasPrefix(f.prompt(t), "You are running as the engram reeve-1a2b3c4d.") {
		t.Fatalf("prompt = %s", f.prompt(t))
	}
}

// A caller with a launch prompt of its own says what stands in for the
// packet when the boot fails.
func TestAFailedBootPutsTheCallersLineFirst(t *testing.T) {
	f := newEngramSpawnFixture(t, "claude-code")
	f.cli.bootErr = &engram.Error{Code: 1, Message: "store is read-only"}
	err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{
		Engram: "mneme", LaunchPrompt: "## You are the Reeve",
		EngramMissing: func(reason string) string { return "## Your engram\n\nIt did not boot: " + reason },
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := f.prompt(t)
	if !strings.HasPrefix(prompt, "## Your engram\n\nIt did not boot: store is read-only") || !strings.Contains(prompt, "## You are the Reeve") {
		t.Fatalf("prompt = %s", prompt)
	}
}
