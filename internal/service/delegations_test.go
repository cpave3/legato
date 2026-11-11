package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type spawnCall struct {
	taskID string
	opts   AgentSpawnOptions
}

// fakeDelegationAgents records what the delegation service asks of agents.
// A spawn records a running session in the store, as the real one does.
type fakeDelegationAgents struct {
	db        *store.Store
	mu        sync.Mutex
	messages  []string // "<task>: <text>"
	spawns    []spawnCall
	spawnErr  error
	sandboxed bool
}

func (f *fakeDelegationAgents) QueueMessage(_ context.Context, taskID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, taskID+": "+text)
	return nil
}

func (f *fakeDelegationAgents) SpawnAgent(ctx context.Context, taskID string, _, _ int, opts ...AgentSpawnOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var opt AgentSpawnOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	f.spawns = append(f.spawns, spawnCall{taskID: taskID, opts: opt})
	if f.spawnErr != nil {
		return f.spawnErr
	}
	return f.db.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, AgentKind: opt.AgentKind, TmuxSession: "legato-" + taskID, Status: "running", Sandboxed: f.sandboxed})
}

func (f *fakeDelegationAgents) GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error) {
	s, err := f.db.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	s.ExecutionID = "ex-" + taskID
	return s, nil
}

func (f *fakeDelegationAgents) GetTaskResumeSession(ctx context.Context, taskID, kind string) (*TaskResumeSession, error) {
	saved, err := f.db.GetTaskNativeSession(ctx, taskID, kind)
	if err != nil || saved == nil {
		return nil, err
	}
	return &TaskResumeSession{ID: saved.ProviderSessionID, CWD: saved.CWD}, nil
}

type fakeWorktrees struct {
	created []string // "<task> <primary> <branch>"
}

func (f *fakeWorktrees) Create(_ context.Context, taskID, primaryDir, branch, baseBranch string) (*store.TaskWorktree, error) {
	f.created = append(f.created, taskID+" "+primaryDir+" "+branch)
	return &store.TaskWorktree{PrimaryDir: primaryDir, Path: primaryDir + "." + branch, Branch: branch, BaseBranch: baseBranch}, nil
}

type delegationFixture struct {
	svc       *DelegationService
	store     *store.Store
	agents    *fakeDelegationAgents
	worktrees *fakeWorktrees
	t         *testing.T
}

func newDelegationFixture(t *testing.T, adjust ...func(*DelegationOptions)) *delegationFixture {
	t.Helper()
	db := newIntegrationStore(t)
	bus := events.New()
	board := NewBoardService(db, bus)
	seedColumns(t, db)
	agents := &fakeDelegationAgents{db: db, sandboxed: true}
	wt := &fakeWorktrees{}
	opts := DelegationOptions{
		Agent:        "claude-code",
		Repositories: []DelegationRepository{{Repo: "acme/widget", PrimaryDir: "/src/widget", BaseBranch: "main"}},
		Worktrees:    wt,
		Bus:          bus,
		Run:          func(f func()) { f() },
	}
	for _, a := range adjust {
		a(&opts)
	}
	svc := NewDelegationService(db, agents, board, opts)
	return &delegationFixture{svc: svc, store: db, agents: agents, worktrees: wt, t: t}
}

func (f *delegationFixture) task(id string, cc *store.CodeContext) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: id, Title: "Task " + id, Status: "Doing", CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z"}); err != nil {
		f.t.Fatal(err)
	}
	if cc != nil {
		if err := f.store.SetCodeContext(ctx, id, cc); err != nil {
			f.t.Fatal(err)
		}
	}
}

func fixRequest(target DelegationTarget) DelegationRequest {
	return DelegationRequest{Integration: "argus", CaseID: "ci:acme/widget#12", Mode: "fix", Target: target,
		Brief: "## Case\nChecks fail.\n```data\nFAIL TestX\n```", IdempotencyKey: "argus-1"}
}

func delegationCode(err error) string {
	var de *DelegationError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}

func TestADelegationToARunningAgentIsATaggedMessage(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("t1", &store.CodeContext{Repo: "acme/widget", Branch: "feat-x", WorkingDir: "/src/widget"})
	if err := f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "t1", AgentKind: "codex", TmuxSession: "legato-t1", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	status, created, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil || !created {
		t.Fatalf("delegate = %+v, %v, %v", status, created, err)
	}
	if !strings.HasPrefix(status.DelegationID, "dl-") || status.Route != "message" || status.TaskID != "t1" {
		t.Fatalf("status = %+v", status)
	}
	if len(f.agents.messages) != 1 {
		t.Fatalf("messages = %v", f.agents.messages)
	}
	msg := f.agents.messages[0]
	if !strings.HasPrefix(msg, "t1: [argus case ci:acme/widget#12 · delegation "+status.DelegationID+" · fix]") ||
		!strings.Contains(msg, fixRequest(DelegationTarget{}).Brief) || !strings.Contains(msg, "reply_case") || !strings.Contains(msg, "not from the operator") {
		t.Fatalf("message = %q", msg)
	}
	got, err := f.svc.Status(context.Background(), "argus", status.DelegationID)
	if err != nil || got.State != "delivered" || got.AgentID != "ex-t1" || got.Enforced || got.DeliveredAt == "" {
		t.Fatalf("status = %+v, %v", got, err)
	}
	if _, err := f.svc.Status(context.Background(), "someone-else", status.DelegationID); delegationCode(err) != "not_found" {
		t.Fatalf("other integration err = %v", err)
	}
}

func TestARepeatedDelegationIsTheSameOneAndAChangedOneConflicts(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	_ = f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "t1", AgentKind: "codex", TmuxSession: "legato-t1", Status: "running"})
	first, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil || created || again.DelegationID != first.DelegationID {
		t.Fatalf("repeat = %+v, %v, %v", again, created, err)
	}
	if len(f.agents.messages) != 1 {
		t.Fatalf("delivered %d times", len(f.agents.messages))
	}
	changed := fixRequest(DelegationTarget{Task: "t1"})
	changed.Brief = "something else"
	if _, _, err := f.svc.Delegate(context.Background(), changed); delegationCode(err) != "idempotency_conflict" {
		t.Fatalf("changed err = %v", err)
	}
}

func TestAStoppedAgentIsResumedWithTheBrief(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	savedNativeConversation(t, f.store, "t1", "claude-code", "/src/widget", "")
	req := fixRequest(DelegationTarget{Task: "t1"})
	req.Mode = "find_out"
	status, _, err := f.svc.Delegate(context.Background(), req)
	if err != nil || status.Route != "resume" {
		t.Fatalf("delegate = %+v, %v", status, err)
	}
	if len(f.agents.spawns) != 1 {
		t.Fatalf("spawns = %+v", f.agents.spawns)
	}
	opt := f.agents.spawns[0].opts
	if opt.SessionPolicy != "resume" || opt.ResumeSessionID != "saved-conversation" || opt.AgentKind != "claude-code" || opt.WorkingDir != "/src/widget" ||
		!opt.ReadOnlyCheckout || !strings.Contains(opt.Prompt, "· find out]") {
		t.Fatalf("spawn = %+v", opt)
	}
	got, _ := f.svc.Status(context.Background(), "argus", status.DelegationID)
	if got.State != "delivered" || !got.Enforced {
		t.Fatalf("status = %+v", got)
	}
}

func TestATaskWithoutAnAgentStartsOneInItsCheckout(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget.feat"})
	status, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil || status.Route != "start" {
		t.Fatalf("delegate = %+v, %v", status, err)
	}
	opt := f.agents.spawns[0].opts
	if opt.AgentKind != "claude-code" || opt.WorkingDir != "/src/widget.feat" || opt.ReadOnlyCheckout || !strings.Contains(opt.Prompt, "[argus case") {
		t.Fatalf("spawn = %+v", opt)
	}

	f.task("t2", nil)
	req := fixRequest(DelegationTarget{Task: "t2"})
	req.IdempotencyKey = "argus-2"
	if _, _, err := f.svc.Delegate(context.Background(), req); delegationCode(err) != "no_checkout" {
		t.Fatalf("no checkout err = %v", err)
	}
}

func TestDelegationsThatCannotBeRoutedAreRefusedAndNotStored(t *testing.T) {
	f := newDelegationFixture(t, func(o *DelegationOptions) { o.Worktrees = nil })
	f.task("swarm", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	_ = f.store.CreateTask(context.Background(), store.Task{ID: "gone", Title: "gone", Status: "Done", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"})
	_ = f.store.ArchiveTask(context.Background(), "gone")
	for _, tc := range []struct {
		name   string
		mode   string
		target DelegationTarget
		code   string
	}{
		{"unknown task", "fix", DelegationTarget{Task: "nope"}, "unknown_task"},
		{"archived task", "fix", DelegationTarget{Task: "gone"}, "unknown_task"},
		{"unknown repo", "fix", DelegationTarget{Repo: "acme/other"}, "unknown_repo"},
		{"fix a PR nobody owns without worktrees", "fix", DelegationTarget{Repo: "acme/widget", PR: 12, Branch: "feat-x"}, "no_checkout"},
		{"no target", "fix", DelegationTarget{}, "bad_request"},
		{"bad mode", "rewrite", DelegationTarget{Task: "nope"}, "bad_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := fixRequest(tc.target)
			req.Mode = tc.mode
			req.IdempotencyKey = "argus-" + tc.name
			if _, _, err := f.svc.Delegate(context.Background(), req); delegationCode(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
	// A stopped swarm worker's conversation is not resumed for an integration.
	swarmWorkerWithSavedConversation(t, f.store, "swarm")
	req := fixRequest(DelegationTarget{Task: "swarm"})
	req.IdempotencyKey = "argus-swarm"
	if _, _, err := f.svc.Delegate(context.Background(), req); delegationCode(err) != "not_resumable" {
		t.Fatalf("swarm err = %v", err)
	}
	if open, _ := f.store.ListDelegationsByState(context.Background(), store.DelegationPending); len(open) != 0 {
		t.Fatalf("stored = %+v", open)
	}
	if len(f.agents.spawns)+len(f.agents.messages) != 0 {
		t.Fatalf("agents were asked: %+v %v", f.agents.spawns, f.agents.messages)
	}
}

func TestAFindOutForAPRNobodyOwnsStartsReadOnlyInTheMainCheckout(t *testing.T) {
	f := newDelegationFixture(t, func(o *DelegationOptions) { o.Worktrees = nil })
	req := fixRequest(DelegationTarget{Repo: "acme/widget", PR: 12, Branch: "feat-x"})
	req.Mode, req.Title = "find_out", "Argus: CI failing on acme/widget#12"
	status, _, err := f.svc.Delegate(context.Background(), req)
	if err != nil || status.Route != "start" {
		t.Fatalf("delegate = %+v, %v", status, err)
	}
	task, err := f.store.GetTask(context.Background(), status.TaskID)
	if err != nil || task.Title != "Argus: CI failing on acme/widget#12" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	opt := f.agents.spawns[0].opts
	if opt.WorkingDir != "/src/widget" || !opt.ReadOnlyCheckout {
		t.Fatalf("spawn = %+v", opt)
	}
}

func TestAFixForAPRNobodyOwnsGetsAWorktreeOnItsBranch(t *testing.T) {
	f := newDelegationFixture(t)
	status, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Repo: "acme/widget", PR: 12, Branch: "feat-x"}))
	if err != nil || status.Route != "start" || status.TaskID == "" {
		t.Fatalf("delegate = %+v, %v", status, err)
	}
	if len(f.worktrees.created) != 1 || f.worktrees.created[0] != status.TaskID+" /src/widget feat-x" {
		t.Fatalf("worktrees = %v", f.worktrees.created)
	}
	if opt := f.agents.spawns[0].opts; opt.WorkingDir != "/src/widget.feat-x" {
		t.Fatalf("spawn = %+v", opt)
	}
}

func TestAPROwnedByATaskGoesToThatTask(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("owner", &store.CodeContext{Repo: "acme/widget", Branch: "feat-x", WorkingDir: "/src/widget.feat-x"})
	_ = f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "owner", AgentKind: "codex", TmuxSession: "legato-owner", Status: "running"})
	status, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Repo: "acme/widget", PR: 12, Branch: "feat-x"}))
	if err != nil || status.TaskID != "owner" || status.Route != "message" {
		t.Fatalf("delegate = %+v, %v", status, err)
	}
}

func TestAFailedSpawnFailsTheDelegation(t *testing.T) {
	f := newDelegationFixture(t)
	f.agents.spawnErr = errors.New("tmux is gone")
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	status, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Status(context.Background(), "argus", status.DelegationID)
	if got.State != "failed" || got.Error == nil || got.Error.Code != "spawn_failed" {
		t.Fatalf("status = %+v", got)
	}
}

func TestAFollowUpMessageReachesTheSameAgentOnceDelivered(t *testing.T) {
	f := newDelegationFixture(t)
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	_ = f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "t1", AgentKind: "codex", TmuxSession: "legato-t1", Status: "running"})
	status, _, _ := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))

	msg, created, err := f.svc.Message(context.Background(), "argus", status.DelegationID, "Use the staging database.", "m-1")
	if err != nil || !created || msg.State != "delivered" || !strings.HasPrefix(msg.MessageID, "dm-") {
		t.Fatalf("message = %+v, %v, %v", msg, created, err)
	}
	last := f.agents.messages[len(f.agents.messages)-1]
	if !strings.HasPrefix(last, "t1: [argus case ci:acme/widget#12 · delegation "+status.DelegationID+" · message]") || !strings.Contains(last, "Use the staging database.") {
		t.Fatalf("message = %q", last)
	}
	again, created, err := f.svc.Message(context.Background(), "argus", status.DelegationID, "Use the staging database.", "m-1")
	if err != nil || created || again.MessageID != msg.MessageID {
		t.Fatalf("repeat = %+v, %v, %v", again, created, err)
	}
	if _, _, err := f.svc.Message(context.Background(), "argus", status.DelegationID, "other text", "m-1"); delegationCode(err) != "idempotency_conflict" {
		t.Fatalf("conflict err = %v", err)
	}
	if _, _, err := f.svc.Message(context.Background(), "argus", "dl-nope", "x", "m-2"); delegationCode(err) != "not_found" {
		t.Fatalf("unknown err = %v", err)
	}
	_ = f.store.SetDelegationFailed(context.Background(), status.DelegationID, "spawn_failed", "x")
	if _, _, err := f.svc.Message(context.Background(), "argus", status.DelegationID, "x", "m-3"); delegationCode(err) != "closed" {
		t.Fatalf("closed err = %v", err)
	}
}

func TestAskModeWaitsForThePersonsDecision(t *testing.T) {
	var approvals *ApprovalService
	f := newDelegationFixture(t, func(o *DelegationOptions) {
		o.Delivery = "ask"
		approvals = NewApprovalService(newIntegrationStore(t), ApprovalServiceOptions{})
		o.Approvals = approvals
	})
	// The approvals live in the delegation store, as in Legato.
	approvals = NewApprovalService(f.store, ApprovalServiceOptions{})
	f.svc.opts.Approvals = approvals
	f.svc.RegisterResolver()
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	_ = f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "t1", AgentKind: "codex", TmuxSession: "legato-t1", Status: "running"})
	ctx := context.Background()

	status, _, err := f.svc.Delegate(ctx, fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil || status.State != "awaiting_approval" || len(f.agents.messages) != 0 {
		t.Fatalf("delegate = %+v, %v, messages %v", status, err, f.agents.messages)
	}
	pending, _ := approvals.Pending(ctx)
	if len(pending) != 1 || pending[0].Source != ApprovalSourceDelegation {
		t.Fatalf("pending = %+v", pending)
	}
	if _, err := approvals.Decide(ctx, pending[0].ID, ApprovalDecision{Decision: ApprovalAllow}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Status(ctx, "argus", status.DelegationID); got.State != "delivered" || len(f.agents.messages) != 1 {
		t.Fatalf("after allow = %+v, messages %v", got, f.agents.messages)
	}

	req := fixRequest(DelegationTarget{Task: "t1"})
	req.IdempotencyKey = "argus-2"
	second, _, _ := f.svc.Delegate(ctx, req)
	pending, _ = approvals.Pending(ctx)
	if _, err := approvals.Decide(ctx, pending[0].ID, ApprovalDecision{Decision: ApprovalDeny}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Status(ctx, "argus", second.DelegationID)
	if got.State != "failed" || got.Error == nil || got.Error.Code != "denied" || len(f.agents.messages) != 1 {
		t.Fatalf("after deny = %+v", got)
	}
}

// swarmWorkerWithSavedConversation leaves a task whose stopped agent was a
// swarm's conductor with a saved conversation. (A worker's conversation is
// never saved.)
func swarmWorkerWithSavedConversation(t *testing.T, s *store.Store, taskID string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, AgentKind: "claude-code", ExecutionID: "ex-swarm", TmuxSession: "w", Status: "running", Role: "conductor"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-swarm", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: "claude-code", AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, taskID, id, "ex-swarm", "claude-code", "saved-conversation", "/src/widget", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentSessionStatus(ctx, taskID, "dead"); err != nil {
		t.Fatal(err)
	}
}

func TestADelegationLeftPendingIsDeliveredLater(t *testing.T) {
	var held []func()
	f := newDelegationFixture(t, func(o *DelegationOptions) { o.Run = func(fn func()) { held = append(held, fn) } })
	f.task("t1", &store.CodeContext{Repo: "acme/widget", WorkingDir: "/src/widget"})
	_ = f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "t1", AgentKind: "codex", TmuxSession: "legato-t1", Status: "running"})
	status, _, err := f.svc.Delegate(context.Background(), fixRequest(DelegationTarget{Task: "t1"}))
	if err != nil {
		t.Fatal(err)
	}
	held = nil // the Legato that accepted it stopped before delivering
	f.svc.ResumeStale(context.Background(), time.Hour)
	if len(held) != 0 {
		t.Fatal("a young delegation was taken over")
	}
	f.svc.ResumeStale(context.Background(), -time.Second)
	for _, fn := range held {
		fn()
	}
	if got, _ := f.svc.Status(context.Background(), "argus", status.DelegationID); got.State != "delivered" {
		t.Fatalf("status = %+v", got)
	}
}
