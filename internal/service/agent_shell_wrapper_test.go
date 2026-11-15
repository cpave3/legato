package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	enginetmux "github.com/cpave3/legato/internal/engine/tmux"
)

func TestReconcileKeepsAgentBehindForegroundShellWrapper(t *testing.T) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "tmux.sock")
	tmux := func(args ...string) {
		t.Helper()
		cmd := exec.Command(tmuxPath, append([]string{"-S", socket, "-f", "/dev/null"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("scratch tmux %v: %s: %v", args, out, err)
		}
	}
	t.Setenv("TMUX", "")
	tmux("new-session", "-d", "-s", "legato-wrapper", bash+" --noprofile --norc -i")
	t.Cleanup(func() { _ = exec.Command(tmuxPath, "-S", socket, "kill-server").Run() })
	wrapper := filepath.Join(dir, "tmux")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %q -S %q \"$@\"\n", tmuxPath, socket)), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := enginetmux.New(enginetmux.Options{LookPath: func(string) (string, error) { return wrapper, nil }})
	if err != nil {
		t.Fatal(err)
	}
	awaitCommand := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			commands, err := manager.PaneCommands()
			if err != nil {
				t.Fatal(err)
			}
			if commands["legato-wrapper"] == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("foreground = %q; want %q", commands["legato-wrapper"], want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	awaitCommand("bash")
	// Like the sandbox launcher, this foreground shell waits for a child
	// rather than replacing itself with the tool.
	tmux("send-keys", "-t", "legato-wrapper", "sh -c 'sleep 300; :'", "Enter")
	awaitCommand("sh")
	_, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "wrapper")
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "wrapper", TmuxSession: "legato-wrapper", AgentKind: "chimera", Status: "running", Sandboxed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec("UPDATE agent_sessions SET started_at = '2000-01-01 00:00:00'"); err != nil {
		t.Fatal(err)
	}
	agents := NewAgentService(s, manager, dir)
	assertStatus := func(want string) {
		t.Helper()
		sessions, err := agents.ListAgents(ctx)
		if err != nil || len(sessions) != 1 {
			t.Fatalf("agents = %+v, %v", sessions, err)
		}
		if sessions[0].Status != want {
			t.Fatalf("agent status = %q; want %q", sessions[0].Status, want)
		}
	}
	if err := agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	assertStatus("running")
	if err := manager.SendKeysLine("legato-wrapper", "must not reach shell"); err == nil {
		t.Fatal("shell wrapper accepted agent input")
	}
	assertStatus("running")
	tmux("send-keys", "-t", "legato-wrapper", "C-c")
	awaitCommand("bash")
	if err := agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.ListAgents(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("agents after shell return = %+v, %v; want none", sessions, err)
	}
}
