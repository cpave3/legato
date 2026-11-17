package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
	"github.com/cpave3/legato/internal/engine/store"
)

type stdioFixture struct {
	db        *store.Store
	approvals *ApprovalService
	conns     *MCPConnectionService
}

func newStdioFixture(t *testing.T) stdioFixture {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Secrets: flowMemorySecrets{&memorySecrets{values: map[string]string{}}}})
	conns.SetApprovals(approvals)
	return stdioFixture{db: db, approvals: approvals, conns: conns}
}

// fakeStdioInput is the fake server as an operator would add it.
func fakeStdioInput(t *testing.T, name string) MCPConnectionInput {
	t.Helper()
	path, marker := mcpstdiotest.Command()
	k, v, _ := strings.Cut(marker, "=")
	return MCPConnectionInput{Name: name, Kind: "stdio", Command: path, Args: []string{"--serve"}, Dir: t.TempDir(), Env: map[string]string{k: v}}
}

// propose adds a stdio connection and returns the approval it waits for.
func (f stdioFixture) propose(t *testing.T, in MCPConnectionInput) *Approval {
	t.Helper()
	_, err := f.conns.Add(context.Background(), in)
	var pending *MCPApprovalRequired
	if !errors.As(err, &pending) {
		t.Fatalf("Add = %v, want an approval to wait for", err)
	}
	a, err := f.approvals.Get(context.Background(), pending.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f stdioFixture) allow(t *testing.T, a *Approval) {
	t.Helper()
	if _, err := f.approvals.Decide(context.Background(), a.ID, ApprovalDecision{Decision: ApprovalAllow, By: "operator"}); err != nil {
		t.Fatal(err)
	}
}

func TestALocalServerIsAddedOnlyOnceTheOperatorApprovesIt(t *testing.T) {
	f := newStdioFixture(t)
	ctx := context.Background()
	in := fakeStdioInput(t, "design")
	a := f.propose(t, in)
	if _, err := f.conns.Get(ctx, "design"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("saved before approval: %v", err)
	}
	if a.Source != ApprovalSourceMCPStdio || a.Signed {
		t.Fatalf("approval %+v", a.Approval)
	}
	var details struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(a.Details, &details); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(details.Command, in.Command+" --serve") || details.Cwd != in.Dir {
		t.Fatalf("the operator is not shown what will run: %+v", details)
	}
	if !strings.Contains(a.Summary, "outside") {
		t.Fatalf("the summary does not say it runs outside the sandbox: %q", a.Summary)
	}
	f.allow(t, a)
	got, err := f.conns.Get(ctx, "design")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "stdio" || got.Command != in.Command || got.Dir != in.Dir || len(got.Args) != 1 || got.Authorization != "not_required" || got.Problem != nil {
		t.Fatalf("saved %+v", got)
	}
}

func TestADeniedLocalServerIsNeverSaved(t *testing.T) {
	f := newStdioFixture(t)
	a := f.propose(t, fakeStdioInput(t, "design"))
	if _, err := f.approvals.Decide(context.Background(), a.ID, ApprovalDecision{Decision: ApprovalDeny, By: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conns.Get(context.Background(), "design"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a denied server was saved: %v", err)
	}
}

func TestALocalServerNeedsASignatureOnceADeviceIsEnrolled(t *testing.T) {
	f := newStdioFixture(t)
	enrollTestDevice(t, f.db)
	a := f.propose(t, fakeStdioInput(t, "design"))
	if !a.Signed {
		t.Fatal("the approval is not signed with a device enrolled")
	}
	if _, err := f.approvals.Decide(context.Background(), a.ID, ApprovalDecision{Decision: ApprovalAllow, By: "web"}); !errors.Is(err, ErrApprovalNeedsSignature) {
		t.Fatalf("an unsigned allow = %v", err)
	}
}

func TestWithoutApprovalsALocalServerCannotBeAdded(t *testing.T) {
	approvalsLess := NewMCPConnectionService(newIntegrationStore(t), MCPConnectionOptions{})
	_, err := approvalsLess.Add(context.Background(), fakeStdioInput(t, "design"))
	if err == nil || errors.As(err, new(*MCPApprovalRequired)) {
		t.Fatalf("Add = %v", err)
	}
}

func TestChangingWhatALocalServerRunsWaitsForApprovalAndItsGrantsDoNot(t *testing.T) {
	f := newStdioFixture(t)
	ctx := context.Background()
	f.allow(t, f.propose(t, fakeStdioInput(t, "design")))
	cur, _ := f.conns.Get(ctx, "design")

	// Grants and the switch apply at once.
	rules := []MCPRule{{Tools: "*", Repository: "*/*", Approve: "auto"}}
	off := false
	cur, err := f.conns.Update(ctx, "design", MCPConnectionChange{Rules: &rules, Enabled: &off, ExpectedRevision: cur.Revision})
	if err != nil || cur.Enabled || len(cur.Rules) != 1 {
		t.Fatalf("Update = %+v, %v", cur, err)
	}

	for name, ch := range map[string]MCPConnectionChange{
		"command": {Command: ptr("/bin/sh")},
		"args":    {Args: &[]string{"-c", "true"}},
		"dir":     {Dir: ptr(t.TempDir())},
		"env":     {Env: &map[string]string{"TOKEN": "x"}},
	} {
		ch.ExpectedRevision = cur.Revision
		_, err := f.conns.Update(ctx, "design", ch)
		var pending *MCPApprovalRequired
		if !errors.As(err, &pending) {
			t.Fatalf("%s: Update = %v, want an approval", name, err)
		}
		if got, _ := f.conns.Get(ctx, "design"); got.Revision != cur.Revision {
			t.Fatalf("%s: applied before approval", name)
		}
	}

	ch := MCPConnectionChange{Args: &[]string{"--other"}, ExpectedRevision: cur.Revision}
	_, err = f.conns.Update(ctx, "design", ch)
	var pending *MCPApprovalRequired
	errors.As(err, &pending)
	a, _ := f.approvals.Get(ctx, pending.ApprovalID)
	f.allow(t, a)
	got, _ := f.conns.Get(ctx, "design")
	if len(got.Args) != 1 || got.Args[0] != "--other" || len(got.Rules) != 1 {
		t.Fatalf("after approval %+v", got)
	}
}

func TestAnApprovedChangeToAServerChangedSinceFails(t *testing.T) {
	f := newStdioFixture(t)
	ctx := context.Background()
	f.allow(t, f.propose(t, fakeStdioInput(t, "design")))
	cur, _ := f.conns.Get(ctx, "design")
	_, err := f.conns.Update(ctx, "design", MCPConnectionChange{Args: &[]string{"--a"}, ExpectedRevision: cur.Revision})
	var pending *MCPApprovalRequired
	errors.As(err, &pending)
	off := false
	if _, err := f.conns.Update(ctx, "design", MCPConnectionChange{Enabled: &off, ExpectedRevision: cur.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.approvals.Decide(ctx, pending.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, By: "operator"}); !errors.Is(err, ErrMCPConnectionStale) {
		t.Fatalf("Decide = %v, want stale", err)
	}
}

func TestALocalServerIsCheckedBeforeTheOperatorIsAsked(t *testing.T) {
	f := newStdioFixture(t)
	for name, change := range map[string]func(*MCPConnectionInput){
		"relative dir":   func(in *MCPConnectionInput) { in.Dir = "repo" },
		"missing dir":    func(in *MCPConnectionInput) { in.Dir = "/nonexistent/repo" },
		"no command":     func(in *MCPConnectionInput) { in.Command = "" },
		"relative path":  func(in *MCPConnectionInput) { in.Command = "bin/node" },
		"bad env name":   func(in *MCPConnectionInput) { in.Env = map[string]string{"A-B": "x"} },
		"a URL":          func(in *MCPConnectionInput) { in.URL = "https://mcp.example.test/mcp" },
		"authorization":  func(in *MCPConnectionInput) { in.Auth = "oauth" },
		"nul in an arg":  func(in *MCPConnectionInput) { in.Args = []string{"a\x00b"} },
		"unknown kind":   func(in *MCPConnectionInput) { in.Kind = "ssh" },
		"http with args": func(in *MCPConnectionInput) { in.Kind, in.URL = "http", "https://mcp.example.test/mcp" },
	} {
		in := fakeStdioInput(t, "design")
		change(&in)
		if _, err := f.conns.Add(context.Background(), in); !errors.Is(err, ErrMCPConnectionInvalid) {
			t.Errorf("%s: Add = %v, want invalid", name, err)
		}
	}
	pending, _ := f.approvals.Pending(context.Background())
	if len(pending) != 0 {
		t.Fatalf("the operator was asked about invalid servers: %d", len(pending))
	}
}

func TestATildeDirIsTheHomeFolder(t *testing.T) {
	f := newStdioFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	in := fakeStdioInput(t, "design")
	in.Dir = "~"
	f.allow(t, f.propose(t, in))
	got, _ := f.conns.Get(context.Background(), "design")
	if got.Dir != home {
		t.Fatalf("dir %q, want %q", got.Dir, home)
	}
}

func ptr[T any](v T) *T { return &v }

// enrollTestDevice enrolls a device that can sign decisions.
func enrollTestDevice(t *testing.T, db *store.Store) {
	t.Helper()
	if err := db.InsertApprovalDevice(context.Background(), store.ApprovalDevice{ID: "dev-1", Kind: "android", Name: "phone", PublicKey: []byte{1}, Status: store.DeviceActive, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
}
