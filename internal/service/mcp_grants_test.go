package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type grantsFixture struct {
	db        *store.Store
	approvals *ApprovalService
	broker    *CredentialBroker
	conns     *MCPConnectionService
	grants    *MCPGrants
}

// newGrantsFixture has two running agents: ex-1 on task-1 in acme/app and
// ex-2 on task-2 in acme/other, and an anonymous connection "notion".
func newGrantsFixture(t *testing.T, conn MCPConnectionInput) grantsFixture {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	approvals.opts.PollInterval = 5 * time.Millisecond
	ctx := context.Background()
	for i, repo := range []string{"acme/app", "acme/other"} {
		task := []string{"task-1", "task-2"}[i]
		createTask(t, db, task)
		if err := db.SetCodeContext(ctx, task, &store.CodeContext{Repo: repo}); err != nil {
			t.Fatal(err)
		}
		if err := db.CreateExecution(ctx, store.Execution{ID: []string{"ex-1", "ex-2"}[i], Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: task, ExecutionTaskID: task, AgentKind: "claude-code", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	broker.RegisterResolver()
	conns := NewMCPConnectionService(db, MCPConnectionOptions{})
	grants := NewMCPGrants(broker, conns, db)
	conns.SetGrants(grants)
	if conn.Name == "" {
		conn.Name = "notion"
	}
	conn.URL, conn.Auth = "https://mcp.example.test/mcp", "none"
	if _, err := conns.Add(ctx, conn); err != nil {
		t.Fatal(err)
	}
	return grantsFixture{db: db, approvals: approvals, broker: broker, conns: conns, grants: grants}
}

func (f grantsFixture) call(t *testing.T, execution, tool string) UseDecision {
	t.Helper()
	d, err := f.grants.Authorize(context.Background(), "notion", MCPCaller{ExecutionID: execution}, tool)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f grantsFixture) audit(t *testing.T) []store.CredentialAuditEntry {
	t.Helper()
	entries, err := f.db.ListCredentialAudit(context.Background(), store.CredentialAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestAnMCPConnectionGrantsNothingByDefault(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{})
	if d := f.call(t, "ex-1", "search"); d.Allowed {
		t.Fatal("a tool was allowed with no rule")
	}
	if ok, _ := f.grants.Visible(context.Background(), "notion", MCPCaller{ExecutionID: "ex-1"}, "search"); ok {
		t.Fatal("a tool was listed with no rule")
	}
	entries := f.audit(t)
	if len(entries) == 0 || entries[0].Event != "refuse" || entries[0].Credential != "mcp:notion" || entries[0].Resource != "acme/app/search" || entries[0].ExecutionID != "ex-1" || entries[0].TaskID != "task-1" {
		t.Fatalf("audit %+v", entries)
	}
}

func TestRulesGrantByToolAndRepository(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "search", Repository: "acme/app", Approve: "auto"}}})
	if d := f.call(t, "ex-1", "search"); !d.Allowed {
		t.Fatalf("granted tool refused: %+v", d)
	}
	if d := f.call(t, "ex-2", "search"); d.Allowed {
		t.Fatal("an agent in another repository was allowed")
	}
	if d := f.call(t, "ex-1", "delete_page"); d.Allowed {
		t.Fatal("an ungranted tool was allowed")
	}
	visible, _ := f.grants.Visible(context.Background(), "notion", MCPCaller{ExecutionID: "ex-1"}, "search")
	hidden, _ := f.grants.Visible(context.Background(), "notion", MCPCaller{ExecutionID: "ex-1"}, "delete_page")
	if !visible || hidden {
		t.Fatalf("discovery: search %v, delete_page %v", visible, hidden)
	}
	if use, _ := f.conns.Usable(context.Background(), "notion", MCPCaller{ExecutionID: "ex-1"}); !use.Usable {
		t.Fatalf("usable for ex-1: %+v", use)
	}
	if use, _ := f.conns.Usable(context.Background(), "notion", MCPCaller{ExecutionID: "ex-2"}); use.Usable || use.Code != "not_granted" {
		t.Fatalf("usable for ex-2: %+v", use)
	}
}

// Legato attributes the caller from its execution: a task or repository the
// caller claims is ignored, and an ended execution is refused.
func TestTheCallerIsWhoLegatoSaysItIs(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "acme/app", Approve: "auto"}}})
	ctx := context.Background()
	d, err := f.grants.Authorize(ctx, "notion", MCPCaller{ExecutionID: "ex-2", TaskID: "task-1", Repository: "acme/app"}, "search")
	if err != nil || d.Allowed {
		t.Fatalf("forged task and repository: %+v %v", d, err)
	}
	if d, _ := f.grants.Authorize(ctx, "notion", MCPCaller{ExecutionID: "ex-unknown"}, "search"); d.Allowed {
		t.Fatal("an unknown execution was allowed")
	}
	if err := f.db.TerminalizeExecution(ctx, "ex-1", store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	if d := f.call(t, "ex-1", "search"); d.Allowed {
		t.Fatal("an ended execution was allowed")
	}
}

func TestARequestRuleAsksTheOperatorAndTheGrantLasts(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "request"}}})
	ctx := context.Background()
	// Nobody answers: the call waits, then reports the pending approval.
	pending := f.call(t, "ex-1", "search")
	if pending.Allowed || pending.ApprovalID == "" {
		t.Fatalf("unanswered: %+v", pending)
	}
	approval, err := f.approvals.Get(ctx, pending.ApprovalID)
	if err != nil || approval.Title != "Use mcp:notion" || !strings.Contains(approval.Summary, "acme/app/search") {
		t.Fatalf("approval %+v %v", approval, err)
	}
	if _, err := f.approvals.Decide(ctx, pending.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", By: "web"}); err != nil {
		t.Fatal(err)
	}
	if d := f.call(t, "ex-1", "search"); !d.Allowed {
		t.Fatalf("after approval: %+v", d)
	}
	if d := f.call(t, "ex-1", "search"); d.Allowed {
		t.Fatal("a once grant was used twice")
	}
	if d := f.call(t, "ex-2", "search"); d.Allowed {
		t.Fatal("one agent's grant served another")
	}
}

func TestALookOnlyAgentCallsOnlyToolsTheOperatorCalledReadOnly(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{ReadOnlyTools: []string{"search", "get_*"}, Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "auto"}}})
	ctx := context.Background()
	if err := f.db.SetExecutionReadOnly(ctx, "ex-1"); err != nil {
		t.Fatal(err)
	}
	if d := f.call(t, "ex-1", "get_page"); !d.Allowed {
		t.Fatalf("read tool refused: %+v", d)
	}
	if d := f.call(t, "ex-1", "update_page"); d.Allowed {
		t.Fatal("a look-only agent called a write tool")
	}
	if d := f.call(t, "ex-2", "update_page"); !d.Allowed {
		t.Fatal("an ordinary agent was refused a granted write tool")
	}
}

func TestRuleAccessSeparatesReadAndWriteTools(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{ReadOnlyTools: []string{"search"}, Rules: []MCPRule{{Tools: "*", Repository: "*/*", Access: "read", Approve: "auto"}}})
	if d := f.call(t, "ex-1", "search"); !d.Allowed {
		t.Fatal("read tool refused")
	}
	if d := f.call(t, "ex-1", "update_page"); d.Allowed {
		t.Fatal("a read-only rule allowed a write tool")
	}
}

// Granted access ends when the connection's resource changes, when the
// operator revokes the grant, or when the connection is turned off.
func TestGrantsEndWithTheResourceTheyWereFor(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "session"}}})
	ctx := context.Background()
	approve := func() {
		d := f.call(t, "ex-1", "search")
		if d.ApprovalID == "" {
			t.Fatalf("no approval asked: %+v", d)
		}
		if _, err := f.approvals.Decide(ctx, d.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"}); err != nil {
			t.Fatal(err)
		}
		if d := f.call(t, "ex-1", "search"); !d.Allowed {
			t.Fatalf("after approval: %+v", d)
		}
	}
	approve()
	got, _ := f.conns.Get(ctx, "notion")
	moved := "https://other.example.test/mcp"
	if _, err := f.conns.Update(ctx, "notion", MCPConnectionChange{URL: &moved, ExpectedRevision: got.Revision}); err != nil {
		t.Fatal(err)
	}
	if d := f.call(t, "ex-1", "search"); d.Allowed {
		t.Fatal("a grant for the old resource served the new one")
	}
	approve()
	grants, _ := f.broker.Grants(ctx)
	for _, g := range grants {
		if g.Credential == "mcp:notion" {
			if err := f.broker.EndGrant(ctx, g.ID, "web"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if d := f.call(t, "ex-1", "search"); d.Allowed {
		t.Fatal("a revoked grant still served")
	}
	approve()
	got, _ = f.conns.Get(ctx, "notion")
	off := false
	if _, err := f.conns.Update(ctx, "notion", MCPConnectionChange{Enabled: &off, ExpectedRevision: got.Revision}); err != nil {
		t.Fatal(err)
	}
	if d := f.call(t, "ex-1", "search"); d.Allowed || !strings.Contains(d.Reason, "turned off") {
		t.Fatalf("disabled connection: %+v", d)
	}
}

// The broker uses an MCP connection for the agent; no agent can read or
// exec its token, whatever grant it holds.
func TestAnMCPConnectionIsNeverAValue(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "auto"}}})
	_, _, err := f.broker.Value(context.Background(), CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "mcp:notion", Operation: "mcp.write", Resource: "acme/app/search"})
	if err == nil || !strings.Contains(err.Error(), "broker") {
		t.Fatalf("value of an MCP connection: %v", err)
	}
}

func TestToolNamesAndRulesAreChecked(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "auto"}}})
	for _, bad := range []string{"", "../x", "a/b", "a b", strings.Repeat("x", 129), "*"} {
		if _, err := f.grants.Authorize(context.Background(), "notion", MCPCaller{ExecutionID: "ex-1"}, bad); !errors.Is(err, ErrMCPToolName) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	db := newIntegrationStore(t)
	conns := NewMCPConnectionService(db, MCPConnectionOptions{})
	for _, rule := range []MCPRule{
		{Tools: "*", Repository: "acme", Approve: "auto"},
		{Tools: "a/b", Repository: "*/*", Approve: "auto"},
		{Tools: "*", Repository: "*/*", Approve: "sometimes"},
		{Tools: "*", Repository: "*/*", Access: "admin", Approve: "auto"},
		{Tools: "[", Repository: "*/*", Approve: "auto"},
		{Tools: "*", Repository: "*/*", Approve: "window"},
	} {
		if _, err := conns.Add(context.Background(), MCPConnectionInput{Name: "x", URL: "https://mcp.example.test/mcp", Rules: []MCPRule{rule}}); !errors.Is(err, ErrMCPConnectionInvalid) {
			t.Errorf("%+v: %v", rule, err)
		}
	}
}

// What an agent passes to a tool never reaches the audit log: Legato
// records which tool, not its arguments.
func TestTheAuditRecordsTheToolNotItsArguments(t *testing.T) {
	f := newGrantsFixture(t, MCPConnectionInput{Rules: []MCPRule{{Tools: "*", Repository: "*/*", Approve: "auto"}}})
	f.call(t, "ex-1", "search")
	entries := f.audit(t)
	if len(entries) == 0 || entries[0].Event != "use" || !strings.Contains(entries[0].Detail, "tools/call search") {
		t.Fatalf("audit %+v", entries)
	}
	if n, err := f.broker.VerifyAudit(context.Background()); err != nil || n != 0 {
		t.Fatalf("audit chain: %d %v", n, err)
	}
}
