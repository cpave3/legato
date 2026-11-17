package mcpstdio_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpstdio"
	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	mcpstdiotest.Serve()
	os.Exit(m.Run())
}

type lines struct {
	mu  sync.Mutex
	all []string
}

func (l *lines) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, s)
}

func (l *lines) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.all {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

func fakeSpec(t *testing.T, extra ...string) (mcpstdio.Spec, *lines) {
	t.Helper()
	path, marker := mcpstdiotest.Command()
	got := &lines{}
	return mcpstdio.Spec{
		Command: path,
		Dir:     t.TempDir(),
		Env:     append([]string{marker, "PATH=" + os.Getenv("PATH")}, extra...),
		Stderr:  got.add,
		Grace:   200 * time.Millisecond,
	}, got
}

func connect(t *testing.T, p *mcpstdio.Process) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, p.Transport(), nil)
	if err != nil {
		t.Fatalf("connecting: %v (stderr: %s)", err, p.StderrTail())
	}
	return session
}

func call(t *testing.T, s *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("calling %s: %v", tool, err)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func alive(pid int) bool {
	if runtime.GOOS == "linux" {
		// A zombie still answers signal 0; read its state instead.
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return false
		}
		fields := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+1:]))
		return len(fields) > 0 && fields[0] != "Z"
	}
	return syscall.Kill(pid, 0) == nil
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d is still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAServerIsStartedAndSpokenToOverItsPipes(t *testing.T) {
	spec, stderr := fakeSpec(t, "GREETING=hello")
	p, err := mcpstdio.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	s := connect(t, p)
	defer s.Close()
	if got := call(t, s, "echo", map[string]any{"text": "hi"}); got != "hi" {
		t.Fatalf("echo = %q", got)
	}
	if got := call(t, s, "env", map[string]any{"text": "GREETING"}); got != "hello" {
		t.Fatalf("GREETING = %q", got)
	}
	if got := call(t, s, "cwd", nil); got != spec.Dir {
		t.Fatalf("cwd = %q, want %q", got, spec.Dir)
	}
	// Only the environment given reaches the server.
	if got := call(t, s, "env", map[string]any{"text": "HOME"}); got != "<unset>" {
		t.Fatalf("HOME = %q; the server got more than its environment", got)
	}
	if !stderr.has("fake server starting") {
		t.Fatalf("stderr lines = %v", stderr.all)
	}
}

func TestStoppingEndsTheServerAndEveryProcessItStarted(t *testing.T) {
	spec, _ := fakeSpec(t)
	p, err := mcpstdio.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	s := connect(t, p)
	server, _ := strconv.Atoi(call(t, s, "pid", nil))
	helper, _ := strconv.Atoi(call(t, s, "spawn", map[string]any{"pid_file": filepath.Join(t.TempDir(), "pid")}))
	if !alive(helper) {
		t.Fatal("the helper did not start")
	}
	p.Stop()
	waitGone(t, server)
	if runtime.GOOS == "linux" {
		waitGone(t, helper)
	} else {
		_ = syscall.Kill(helper, syscall.SIGKILL)
	}
	select {
	case <-p.Done():
	default:
		t.Fatal("Done is open after Stop")
	}
}

func TestAServerThatCrashesTakesItsHelpersWithIt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descendants are found through /proc")
	}
	spec, stderr := fakeSpec(t)
	p, err := mcpstdio.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	s := connect(t, p)
	helper, _ := strconv.Atoi(call(t, s, "spawn", map[string]any{"pid_file": filepath.Join(t.TempDir(), "pid")}))
	// Let the process watch see the helper before the crash.
	time.Sleep(mcpstdio.WatchEvery + 200*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "crash"}); err == nil {
		t.Fatal("a crashed call answered")
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done stayed open after the server exited")
	}
	waitGone(t, helper)
	if !stderr.has("fake server crashing") {
		t.Fatalf("stderr lines = %v", stderr.all)
	}
	if !strings.Contains(p.StderrTail(), "fake server crashing") {
		t.Fatalf("tail = %q", p.StderrTail())
	}
}

func TestABareCommandIsFoundInTheEnvironmentsPATHOnly(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "mytool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := mcpstdio.Resolve("mytool", []string{"PATH=/nonexistent:" + dir})
	if err != nil || got != tool {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, tool)
	}
	// Legato's own PATH is not searched: the environment is the whole truth.
	t.Setenv("PATH", dir)
	_, err = mcpstdio.Resolve("mytool", []string{"PATH=/nonexistent"})
	if !errors.Is(err, mcpstdio.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	for _, want := range []string{"mytool", "/nonexistent", "absolute path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

func TestAnAbsoluteCommandMustExistAndARelativePathIsRefused(t *testing.T) {
	if _, err := mcpstdio.Resolve("/nonexistent/node", nil); !errors.Is(err, mcpstdio.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := mcpstdio.Resolve("bin/node", nil); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err = %v", err)
	}
	notExec := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(notExec, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpstdio.Resolve(notExec, nil); err == nil {
		t.Fatal("a file that cannot be run was accepted")
	}
}

func TestStartSaysWhenTheCommandIsMissing(t *testing.T) {
	_, err := mcpstdio.Start(mcpstdio.Spec{Command: "node", Dir: t.TempDir(), Env: []string{"PATH=/nonexistent"}})
	if !errors.Is(err, mcpstdio.ErrNotFound) || !strings.Contains(err.Error(), "node") {
		t.Fatalf("err = %v", err)
	}
}

func TestAServerThatExitsAtOnceLeavesItsErrorOutput(t *testing.T) {
	spec, _ := fakeSpec(t, "FAKE_MCP_FAIL=no node_modules")
	p, err := mcpstdio.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Connect(ctx, p.Transport(), nil); err == nil {
		t.Fatal("connected to a server that exited")
	}
	<-p.Done()
	if !strings.Contains(p.StderrTail(), "cannot start: no node_modules") {
		t.Fatalf("tail = %q", p.StderrTail())
	}
}
