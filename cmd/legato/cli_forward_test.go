package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
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
	op := cliRunOp(fakeLegato(t), runtime)
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
	tmp := filepath.Join(runtime, "sandbox", "ex-1", "tmp")
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
	ops := map[string]cli.BrokerOp{"cli.run": cliRunOp(fakeLegato(t), runtime)}
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
