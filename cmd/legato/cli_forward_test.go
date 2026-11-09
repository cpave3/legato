package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

// fakeLegato is a binary that prints what it was run with.
func fakeLegato(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legato")
	script := `#!/bin/sh
echo "args=$*"
echo "task=$LEGATO_TASK_ID exec=$LEGATO_EXECUTION_ID broker=${LEGATO_BROKER_TOKEN:-none} sandboxed=${LEGATO_SANDBOXED:-no} sub=${LEGATO_SUBTASK_ID:-none}"
echo "cwd=$(pwd)"
[ "$1" = "plan" ] && cat
echo "to stderr" >&2
[ "$1" = "plan" ] && exit 3
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheHostRunsAForwardedCommandAsTheSession(t *testing.T) {
	runtime := t.TempDir()
	// Outside /tmp, which the host maps to the session's folder.
	work, err := os.MkdirTemp("/var/tmp", "lgfw")
	if err != nil {
		t.Skip("no /var/tmp")
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	scratch := t.TempDir()
	op := cliRunOp(fakeLegato(t), service.SandboxScratch{Root: scratch, RuntimeDir: runtime}, nil)
	raw, _ := json.Marshal(map[string]any{
		"args": []string{"plan", "submit", "/tmp/bundle", "--file=/tmp/a.md", "--task", "t1"},
		"cwd":  work, "stdin": []byte("from stdin"),
		"env": map[string]string{"LEGATO_SUBTASK_ID": "st-1", "LEGATO_TASK_ID": "someone-else", "LEGATO_BROKER_TOKEN": "x"},
	})
	t.Setenv("LEGATO_BROKER_TOKEN", "host-has-none")
	got, err := op(context.Background(), cli.BrokerCaller{TaskID: "t1", ExecutionID: "ex-1"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	out := got.(cliRunResult)
	stdout := string(out.Stdout)
	// The session's /tmp is its scratch folder on disk.
	tmp := filepath.Join(scratch, "ex-1", "tmp")
	for _, want := range []string{
		"args=plan submit " + tmp + "/bundle --file=" + tmp + "/a.md --task t1",
		"task=t1 exec=ex-1 broker=none sandboxed=no sub=st-1",
		"cwd=" + work,
		"from stdin",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if out.Exit != 3 || !strings.Contains(string(out.Stderr), "to stderr") {
		t.Fatalf("exit %d stderr %q", out.Exit, out.Stderr)
	}
}

func TestOnlyAgentCommandsAreForwarded(t *testing.T) {
	for _, args := range [][]string{{"task", "show"}, {"plan", "submit", "x"}, {"review", "answer", "s1"}, {"swarm", "progress", "st", "msg"}, {"credential", "request", "gh"}, {"agent", "message", "t2", "hi"}, {"learn", "search", "x"}, {"workflow", "step", "complete"}} {
		if !forwardable(args) {
			t.Errorf("%v is not forwarded", args)
		}
	}
	for _, args := range [][]string{{"device", "reset"}, {"secret", "add", "k"}, {"audit"}, {"hooks", "install"}, {"auth", "token"}, {"serve"}, {"agent", "state", "t1"}, {"credential", "exec", "gh"}, {"task", "show", "--help"}, {}} {
		if forwardable(args) {
			t.Errorf("%v is forwarded", args)
		}
	}
}

func TestASandboxedCommandRunsOnTheHostThroughTheBroker(t *testing.T) {
	db, _, _, token := brokerFixture(t)
	runtime := shortDir(t)
	ops := map[string]cli.BrokerOp{"cli.run": cliRunOp(fakeLegato(t), service.SandboxScratch{Root: filepath.Join(runtime, "scratch"), RuntimeDir: runtime}, nil)}
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{Ops: ops}})
	t.Cleanup(host.Stop)
	t.Setenv("LEGATO_SANDBOXED", "1")
	t.Setenv("LEGATO_BROKER_SOCKET", host.SocketPath())
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"task", "show", "task1"}); return code })
	if code != 0 || !strings.Contains(out, "args=task show task1") || !strings.Contains(out, "task=task1 exec=ex-1") {
		t.Fatalf("exit %d, out:\n%s", code, out)
	}
	// A host-only command is not forwarded, and cannot open the store here.
	if code := runCLI([]string{"device", "list"}); code == 0 {
		t.Fatal("device list ran in the sandbox")
	}
}

// The Reeve's session runs only commands that read; a change goes to the
// person through the legato tool or propose_changes.
func TestTheReevesSessionRunsOnlyReadCommandsThroughTheBroker(t *testing.T) {
	op := cliRunOp(fakeLegato(t), service.SandboxScratch{Root: t.TempDir(), RuntimeDir: t.TempDir()},
		func(_ context.Context, taskID string) bool { return taskID == "rv-1" })
	run := func(taskID string, args ...string) error {
		raw, _ := json.Marshal(map[string]any{"args": args})
		_, err := op(context.Background(), cli.BrokerCaller{TaskID: taskID, ExecutionID: "ex-1"}, raw)
		return err
	}
	if err := run("rv-1", "task", "update", "other", "--title", "Bypassed"); err == nil || !strings.Contains(err.Error(), "propose_changes") {
		t.Fatalf("a change from the Reeve ran: %v", err)
	}
	if err := run("rv-1", "task", "show", "other"); err != nil {
		t.Fatalf("a read from the Reeve was refused: %v", err)
	}
	if err := run("t1", "task", "update", "t1", "--title", "Fine"); err != nil {
		t.Fatalf("another agent's change was refused: %v", err)
	}
}
