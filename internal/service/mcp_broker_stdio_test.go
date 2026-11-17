package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type stdioBrokerFixture struct {
	stdioFixture
	broker *MCPBroker
	starts string
	secret *memorySecrets
}

// newStdioBrokerFixture: running agents ex-1 and ex-2 in acme/app and ex-3
// in acme/other, and the fake local server "design", approved, which
// acme/app agents may use.
func newStdioBrokerFixture(t *testing.T, env map[string]string) stdioBrokerFixture {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	ctx := context.Background()
	for i, repo := range []string{"acme/app", "acme/app", "acme/other"} {
		task := []string{"task-1", "task-2", "task-3"}[i]
		createTask(t, db, task)
		if err := db.SetCodeContext(ctx, task, &store.CodeContext{Repo: repo}); err != nil {
			t.Fatal(err)
		}
		if err := db.CreateExecution(ctx, store.Execution{ID: []string{"ex-1", "ex-2", "ex-3"}[i], Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: task, ExecutionTaskID: task, AgentKind: "claude-code", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	secrets := &memorySecrets{values: map[string]string{}}
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Secrets: flowMemorySecrets{secrets}})
	conns.SetApprovals(approvals)
	credBroker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	credBroker.RegisterResolver()
	grants := NewMCPGrants(credBroker, conns, db)
	conns.SetGrants(grants)
	f := stdioBrokerFixture{stdioFixture: stdioFixture{db: db, approvals: approvals, conns: conns}, starts: t.TempDir(), secret: secrets}
	in := fakeStdioInput(t, "design")
	in.Env["FAKE_MCP_STARTS"] = f.starts
	for k, v := range env {
		in.Env[k] = v
	}
	in.Rules = []MCPRule{{Tools: "*", Repository: "acme/app", Approve: "auto"}}
	f.allow(t, f.propose(t, in))
	f.broker = NewMCPBroker(conns, grants, nil)
	f.broker.stdioGrace = 100 * time.Millisecond
	t.Cleanup(f.broker.Close)
	t.Cleanup(func() { f.killAll() })
	return f
}

// started is the process ID of every start of the fake server so far.
func (f stdioBrokerFixture) started(t *testing.T) []int {
	t.Helper()
	entries, err := os.ReadDir(f.starts)
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, e := range entries {
		pid, _ := strconv.Atoi(e.Name())
		pids = append(pids, pid)
	}
	return pids
}

func (f stdioBrokerFixture) killAll() {
	entries, _ := os.ReadDir(f.starts)
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func (f stdioBrokerFixture) text(t *testing.T, execution, tool, args string) string {
	t.Helper()
	r, err := f.broker.CallTool(context.Background(), MCPCaller{ExecutionID: execution}, tool, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s %s: %v", execution, tool, err)
	}
	if r.IsError {
		t.Fatalf("%s %s: tool error %s", execution, tool, r.Raw)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(r.Raw, &out)
	return out.Content[0].Text
}

func processAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	s := string(b)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(fields) > 0 && fields[0] != "Z"
}

func eventuallyGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAGrantedAgentUsesALocalServerAndOthersDoNotSeeIt(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	ctx := context.Background()
	tools, problems, err := f.broker.ListTools(ctx, MCPCaller{ExecutionID: "ex-1"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["design__echo"] || !names["design__image"] || len(problems) != 0 {
		t.Fatalf("tools %v, problems %+v", names, problems)
	}
	if got := f.text(t, "ex-1", "design__echo", `{"text":"hello"}`); got != "hello" {
		t.Fatalf("echo = %q", got)
	}
	r, err := f.broker.CallTool(ctx, MCPCaller{ExecutionID: "ex-1"}, "design__image", nil)
	if err != nil || !strings.Contains(string(r.Raw), `"type":"image"`) {
		t.Fatalf("image = %s, %v", r.Raw, err)
	}
	if tools, _, _ := f.broker.ListTools(ctx, MCPCaller{ExecutionID: "ex-3"}); len(tools) != 0 {
		t.Fatalf("an agent without a grant was offered %+v", tools)
	}
	before := len(f.started(t))
	_, err = f.broker.CallTool(ctx, MCPCaller{ExecutionID: "ex-3"}, "design__echo", json.RawMessage(`{"text":"x"}`))
	var denied *MCPDenied
	if !errors.As(err, &denied) {
		t.Fatalf("ex-3: %v", err)
	}
	if len(f.started(t)) != before {
		t.Fatal("a refused call started the server")
	}
	if got, _ := f.conns.Get(ctx, "design"); got.Connection != "connected" {
		t.Fatalf("contact not recorded: %+v", got)
	}
}

func TestListingToolsLeavesNoServerRunning(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	if _, _, err := f.broker.ListTools(context.Background(), MCPCaller{ExecutionID: "ex-1"}); err != nil {
		t.Fatal(err)
	}
	pids := f.started(t)
	if len(pids) != 1 {
		t.Fatalf("listing started %d servers, want 1", len(pids))
	}
	eventuallyGone(t, pids[0])
	// A second agent's listing is served from the catalog.
	if _, _, err := f.broker.ListTools(context.Background(), MCPCaller{ExecutionID: "ex-2"}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.started(t)); n != 1 {
		t.Fatalf("started %d servers, want 1", n)
	}
}

func TestEachSessionHasItsOwnServerWhichEndsWithIt(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	ctx := context.Background()
	one := f.text(t, "ex-1", "design__pid", `{}`)
	if again := f.text(t, "ex-1", "design__pid", `{}`); again != one {
		t.Fatalf("one session reached two servers: %s, %s", one, again)
	}
	two := f.text(t, "ex-2", "design__pid", `{}`)
	if one == two {
		t.Fatal("two sessions share a server")
	}
	helper, _ := strconv.Atoi(f.text(t, "ex-1", "design__spawn", `{"pid_file":"`+filepath.Join(t.TempDir(), "pid")+`"}`))

	if err := f.db.TerminalizeExecution(ctx, "ex-1", store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	f.broker.Sweep(ctx, f.db)
	pid1, _ := strconv.Atoi(one)
	pid2, _ := strconv.Atoi(two)
	eventuallyGone(t, pid1)
	eventuallyGone(t, helper)
	if !processAlive(pid2) {
		t.Fatal("ending one session stopped another's server")
	}
	f.broker.Close()
	eventuallyGone(t, pid2)
}

func TestACrashIsAToolErrorAndTheNextCallStartsAFreshServer(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	ctx := context.Background()
	first := f.text(t, "ex-1", "design__pid", `{}`)
	_, err := f.broker.CallTool(ctx, MCPCaller{ExecutionID: "ex-1"}, "design__crash", nil)
	if err == nil {
		t.Fatal("a call that crashed the server succeeded")
	}
	if strings.Contains(err.Error(), "/") {
		t.Fatalf("the agent was told a host path: %v", err)
	}
	if second := f.text(t, "ex-1", "design__pid", `{}`); second == first {
		t.Fatal("the call after the crash reached the crashed server")
	}
}

func TestAServersErrorOutputIsLoggedWithItsConnectionAndSession(t *testing.T) {
	var buf safeBuffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	f := newStdioBrokerFixture(t, nil)
	f.text(t, "ex-1", "design__echo", `{"text":"x"}`)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "mcp stdio design ex-1: fake server starting") {
		if time.Now().After(deadline) {
			t.Fatalf("log:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestALocalServerGetsItsOwnEnvironmentAndNotLegatos(t *testing.T) {
	t.Setenv("LEGATO_PRIVATE_TOKEN", "do-not-pass")
	f := newStdioBrokerFixture(t, map[string]string{"API_KEY": "legato://design-key", "MODE": "plain"})
	if err := f.secret.Write(context.Background(), "design-key", "s3cret"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"API_KEY": "s3cret", "MODE": "plain", "LEGATO_PRIVATE_TOKEN": "<unset>", "HOME": os.Getenv("HOME")} {
		if got := f.text(t, "ex-1", "design__env", `{"text":"`+name+`"}`); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestTestingAServerListsItsToolsOrSaysWhyItCannot(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	ctx := context.Background()
	res, err := f.broker.Test(ctx, "design")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" || len(res.Tools) == 0 || !strings.Contains(res.Stderr, "fake server starting") {
		t.Fatalf("test %+v", res)
	}

	cur, _ := f.conns.Get(ctx, "design")
	env := cur.Env
	env["FAKE_MCP_FAIL"] = "Cannot find module 'zod'"
	_, err = f.conns.Update(ctx, "design", MCPConnectionChange{Env: &env, ExpectedRevision: cur.Revision})
	var pending *MCPApprovalRequired
	errors.As(err, &pending)
	a, _ := f.approvals.Get(ctx, pending.ApprovalID)
	f.allow(t, a)
	res, err = f.broker.Test(ctx, "design")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" || !strings.Contains(res.Stderr, "Cannot find module 'zod'") {
		t.Fatalf("a failing server: %+v", res)
	}

	cur, _ = f.conns.Get(ctx, "design")
	node, path := "node", map[string]string{"PATH": "/nonexistent/bin"}
	_, err = f.conns.Update(ctx, "design", MCPConnectionChange{Command: &node, Env: &path, ExpectedRevision: cur.Revision})
	errors.As(err, &pending)
	a, _ = f.approvals.Get(ctx, pending.ApprovalID)
	f.allow(t, a)
	res, err = f.broker.Test(ctx, "design")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Error, `"node" is not in PATH /nonexistent/bin`) {
		t.Fatalf("a missing node: %+v", res)
	}
	if got, _ := f.conns.Get(ctx, "design"); got.Problem == nil || got.Problem.Code != "command_not_found" {
		t.Fatalf("problem %+v", got.Problem)
	}
}

func TestTurningALocalServerOffStopsItsSessions(t *testing.T) {
	f := newStdioBrokerFixture(t, nil)
	ctx := context.Background()
	pid, _ := strconv.Atoi(f.text(t, "ex-1", "design__pid", `{}`))
	cur, _ := f.conns.Get(ctx, "design")
	off := false
	if _, err := f.conns.Update(ctx, "design", MCPConnectionChange{Enabled: &off, ExpectedRevision: cur.Revision}); err != nil {
		t.Fatal(err)
	}
	f.broker.Sweep(ctx, f.db)
	eventuallyGone(t, pid)
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
