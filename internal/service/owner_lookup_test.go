package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type ownerFixture struct {
	svc   *OwnerLookupService
	store *store.Store
	t     *testing.T
}

func newOwnerFixture(t *testing.T) *ownerFixture {
	t.Helper()
	agents, s, _ := promptService(t)
	return &ownerFixture{svc: NewOwnerLookupService(s, agents, "https://legato.lan"), store: s, t: t}
}

// task makes a task with code context and, when pr > 0, a linked PR.
func (f *ownerFixture) task(id, updated string, cc store.CodeContext, pr int) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: id, Title: "Task " + id, Status: "Doing", CreatedAt: updated, UpdatedAt: updated}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.SetCodeContext(ctx, id, &cc); err != nil {
		f.t.Fatal(err)
	}
	if pr > 0 {
		raw, _ := store.MarshalPRMeta(&store.PRMeta{Repo: cc.Repo, Branch: cc.Branch, PRNumber: pr, PRURL: "https://github.com/" + cc.Repo + "/pull/12", State: "OPEN"})
		if err := f.store.UpdatePRMeta(ctx, id, raw); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *ownerFixture) running(taskID, kind, activity string) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.store.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, AgentKind: kind, TmuxSession: "legato-" + taskID, Status: "running"}); err != nil {
		f.t.Fatal(err)
	}
	if activity != "" {
		if err := f.store.UpdateAgentActivity(ctx, taskID, activity); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestOwnerLookupFindsATaskByBranchBeforeItsPRIsLinked(t *testing.T) {
	f := newOwnerFixture(t)
	f.task("t1", "2026-11-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "feat-x", WorkingDir: "/src/widget"}, 0)
	f.task("other", "2026-11-01T00:00:00Z", store.CodeContext{Repo: "acme/other", Branch: "feat-x"}, 0)

	matches, err := f.svc.Lookup(context.Background(), OwnerQuery{Repo: "Acme/Widget", PR: 12, Branch: "feat-x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Task.ID != "t1" || matches[0].Match != "branch" {
		t.Fatalf("matches = %+v", matches)
	}
	m := matches[0]
	if m.Task.URL != "https://legato.lan/board?highlight_task=t1" || m.Task.Status != "Doing" || m.Worktree == nil || m.Worktree.Branch != "feat-x" || m.PR != nil {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Agents) != 0 {
		t.Fatalf("agents = %+v, want none", m.Agents)
	}
}

func TestOwnerLookupRanksPRThenSHAThenBranchThenLiveAgents(t *testing.T) {
	f := newOwnerFixture(t)
	f.task("by-branch-old", "2026-10-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "feat-x"}, 0)
	f.task("by-branch-live", "2026-09-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "feat-x"}, 0)
	f.task("by-sha", "2026-08-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "other", HeadSHA: "0123456789abcdef"}, 0)
	f.task("by-pr", "2026-07-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "elsewhere"}, 12)
	f.running("by-branch-live", "claude-code", "waiting")

	matches, err := f.svc.Lookup(context.Background(), OwnerQuery{Repo: "acme/widget", PR: 12, Branch: "feat-x", SHA: "0123456"})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range matches {
		order = append(order, m.Task.ID+":"+m.Match)
	}
	want := []string{"by-pr:pr", "by-sha:sha", "by-branch-live:branch", "by-branch-old:branch"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if pr := matches[0].PR; pr == nil || pr.Number != 12 || pr.State != "OPEN" {
		t.Fatalf("pr = %+v", pr)
	}
	if a := matches[2].Agents; len(a) != 1 || a[0].Kind != "claude-code" || a[0].State != "waiting" {
		t.Fatalf("agents = %+v", a)
	}
}

func TestOwnerLookupReportsAgentStates(t *testing.T) {
	f := newOwnerFixture(t)
	for _, tc := range []struct{ activity, want string }{{"", "idle"}, {"working", "working"}, {"error", "error"}, {"background", "working"}} {
		id := "t-" + tc.want + tc.activity
		f.task(id, "2026-11-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: id}, 0)
		f.running(id, "codex", tc.activity)
		matches, err := f.svc.Lookup(context.Background(), OwnerQuery{Repo: "acme/widget", Branch: id})
		if err != nil || len(matches) != 1 || len(matches[0].Agents) != 1 || matches[0].Agents[0].State != tc.want {
			t.Fatalf("activity %q: %+v, %v", tc.activity, matches, err)
		}
	}
	// An agent that ended with its conversation saved is stopped.
	f.task("t-stopped", "2026-11-01T00:00:00Z", store.CodeContext{Repo: "acme/widget", Branch: "stopped"}, 0)
	savedNativeConversation(t, f.store, "t-stopped", "claude-code", "/src/widget", "")
	matches, err := f.svc.Lookup(context.Background(), OwnerQuery{Repo: "acme/widget", Branch: "stopped"})
	if err != nil || len(matches[0].Agents) != 1 {
		t.Fatalf("stopped: %+v, %v", matches, err)
	}
	if a := matches[0].Agents[0]; a.State != "stopped" || a.Kind != "claude-code" || a.ID != "ex-old" {
		t.Fatalf("stopped agent = %+v", a)
	}
}

func TestOwnerLookupNeedsARepoAndOneMoreThing(t *testing.T) {
	f := newOwnerFixture(t)
	for _, q := range []OwnerQuery{{PR: 1}, {Repo: "acme/widget"}, {Repo: "acme/widget", SHA: "abc"}} {
		if _, err := f.svc.Lookup(context.Background(), q); !errors.Is(err, ErrOwnerQuery) {
			t.Errorf("%+v err = %v", q, err)
		}
	}
	if m, err := f.svc.Lookup(context.Background(), OwnerQuery{Repo: "acme/widget", Branch: "nothing"}); err != nil || len(m) != 0 {
		t.Fatalf("no match = %+v, %v", m, err)
	}
}
