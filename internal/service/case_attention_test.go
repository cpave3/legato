package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type caseAttentionFixture struct {
	t         *testing.T
	db        *store.Store
	attention *AttentionService
	svc       *CaseAttentionService
}

func newCaseAttentionFixture(t *testing.T) *caseAttentionFixture {
	t.Helper()
	db := newIntegrationStore(t)
	attention := NewAttentionService(db, nil)
	svc := NewCaseAttentionService(db, CaseAttentionOptions{Origin: "https://argus.lan", Attention: attention, Owners: NewOwnerLookupService(db, nil, "")})
	if err := attention.RegisterProducer(svc.Producer()); err != nil {
		t.Fatal(err)
	}
	return &caseAttentionFixture{t: t, db: db, attention: attention, svc: svc}
}

// task makes a task working on a repository's branch, with a PR when pr > 0.
func (f *caseAttentionFixture) task(id, repo, branch string, pr int) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.db.CreateTask(ctx, store.Task{ID: id, Title: "Task " + id, Status: "Doing"}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.db.SetCodeContext(ctx, id, &store.CodeContext{Repo: repo, Branch: branch}); err != nil {
		f.t.Fatal(err)
	}
	if pr > 0 {
		raw, _ := store.MarshalPRMeta(&store.PRMeta{Repo: repo, Branch: branch, PRNumber: pr, State: "OPEN", CheckStatus: "fail"})
		if err := f.db.UpdatePRMeta(ctx, id, raw); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *caseAttentionFixture) items() map[string]AttentionItem {
	f.t.Helper()
	items, err := f.attention.List(context.Background(), AttentionFilter{Producer: "argus"})
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]AttentionItem{}
	for _, item := range items {
		out[item.SourceKey] = item
	}
	return out
}

func casePush(revision string) CasePush {
	return CasePush{
		Revision: revision, Signal: "argus_case", Title: "CI failing: acme/widget#12", Reason: "flaky (0.8): rerun proposed",
		Detail: "Triage says flaky.", Urgency: "normal", Target: CaseTarget{Repo: "acme/widget", PR: 12}, Link: "https://argus.lan/cases/1",
	}
}

func TestACaseBecomesAnAttentionItemOnTheTaskThatOwnsItsPR(t *testing.T) {
	f := newCaseAttentionFixture(t)
	f.task("t1", "acme/widget", "feat-x", 12)
	res, err := f.svc.Upsert(context.Background(), "argus", "case-1", casePush("r1"))
	if err != nil {
		t.Fatal(err)
	}
	item, ok := f.items()["case-1"]
	if !ok {
		t.Fatalf("no item: %+v", f.items())
	}
	if res.ItemID != item.ID || res.TaskID != "t1" || res.Status != "open" {
		t.Fatalf("result = %+v, item %+v", res, item)
	}
	if item.TaskID != "t1" || item.Category != "argus" || item.SignalKind != "argus_case" || item.Urgency != "normal" ||
		item.Navigation != (AttentionNavigation{Kind: "external", Target: "https://argus.lan/cases/1"}) || item.Detail != "Triage says flaky." {
		t.Fatalf("item = %+v", item)
	}
}

func TestACaseNoTaskOwnsIsUnattachedUntilATaskTakesItsPR(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	res, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r1"))
	if err != nil || res.TaskID != "" || res.Status != "open" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if item := f.items()["case-1"]; item.TaskID != "" || item.WorkspaceID != nil {
		t.Fatalf("item = %+v", item)
	}
	f.task("t1", "acme/widget", "feat-x", 12)
	if err := f.attention.Reconcile(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if item := f.items()["case-1"]; item.TaskID != "t1" {
		t.Fatalf("after the PR was linked: %+v", item)
	}
}

func TestAnUpsertIsRefusedWhenItCannotBeShown(t *testing.T) {
	f := newCaseAttentionFixture(t)
	for _, tc := range []struct {
		name, key string
		edit      func(*CasePush)
		code      string
	}{
		{"bad key", "issue-1", func(*CasePush) {}, "bad_request"},
		{"no revision", "case-1", func(p *CasePush) { p.Revision = " " }, "bad_request"},
		{"no title", "case-1", func(p *CasePush) { p.Title = "" }, "bad_request"},
		{"no reason", "case-1", func(p *CasePush) { p.Reason = "" }, "bad_request"},
		{"unknown signal", "case-1", func(p *CasePush) { p.Signal = "argus_panic" }, "bad_request"},
		{"unknown urgency", "case-1", func(p *CasePush) { p.Urgency = "urgent" }, "bad_request"},
		{"target without a repository", "case-1", func(p *CasePush) { p.Target = CaseTarget{PR: 12} }, "bad_request"},
		{"link elsewhere", "case-1", func(p *CasePush) { p.Link = "https://evil.example/cases/1" }, "invalid_link"},
		{"link over plain http", "case-1", func(p *CasePush) { p.Link = "http://argus.lan/cases/1" }, "invalid_link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			push := casePush("r1")
			tc.edit(&push)
			_, err := f.svc.Upsert(context.Background(), "argus", tc.key, push)
			var de *DelegationError
			if !errors.As(err, &de) || de.Code != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
	if len(f.items()) != 0 {
		t.Fatalf("a refused upsert was stored: %+v", f.items())
	}
	if _, err := f.svc.Upsert(context.Background(), "jenkins", "case-1", casePush("r1")); err == nil {
		t.Fatal("an integration other than Argus pushed an item")
	}
}

func TestLongCaseTextIsCutRatherThanRefused(t *testing.T) {
	f := newCaseAttentionFixture(t)
	push := casePush("r1")
	push.Title = strings.Repeat("é", 300)
	push.Reason = "first line\nsecond line"
	push.Detail = strings.Repeat("x", 10<<10)
	if _, err := f.svc.Upsert(context.Background(), "argus", "case-1", push); err != nil {
		t.Fatal(err)
	}
	item := f.items()["case-1"]
	if n := len([]rune(item.Title)); n != 200 || !strings.HasSuffix(item.Title, "…") {
		t.Fatalf("title has %d characters: %q", n, item.Title)
	}
	if item.Reason != "first line" {
		t.Fatalf("reason = %q", item.Reason)
	}
	if len(item.Detail) > 4096 || !strings.HasSuffix(item.Detail, "…") {
		t.Fatalf("detail is %d bytes", len(item.Detail))
	}
}

func TestAResolvedCaseStaysClosedWhenALateRetryArrives(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	if err := f.svc.Resolve(ctx, "argus", "case-1", "r1", ""); !isCode(err, "not_found") {
		t.Fatalf("resolve unknown = %v", err)
	}
	if _, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r7")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Resolve(ctx, "argus", "case-1", "r7", "fixed"); err != nil {
		t.Fatal(err)
	}
	if _, open := f.items()["case-1"]; open {
		t.Fatal("still open after the resolve")
	}
	res, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r7"))
	if err != nil || res.Status != "resolved" {
		t.Fatalf("late retry = %+v, %v", res, err)
	}
	if _, open := f.items()["case-1"]; open {
		t.Fatal("the late retry reopened the case")
	}
	if res, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r8")); err != nil || res.Status != "open" {
		t.Fatalf("reopened = %+v, %v", res, err)
	}
}

func TestADismissedCaseStaysDismissedUntilItChanges(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	res, _ := f.svc.Upsert(ctx, "argus", "case-1", casePush("r1"))
	if err := f.attention.Dismiss(ctx, res.ItemID, "r1"); err != nil {
		t.Fatal(err)
	}
	f.task("t1", "acme/widget", "feat-x", 12)
	res, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r1"))
	if err != nil || res.Status != "dismissed" || res.TaskID != "t1" {
		t.Fatalf("same revision = %+v, %v", res, err)
	}
	if _, shown := f.items()["case-1"]; shown {
		t.Fatal("a dismissed case came back at the same revision")
	}
	if res, _ := f.svc.Upsert(ctx, "argus", "case-1", casePush("r2")); res.Status != "open" {
		t.Fatalf("new revision = %+v", res)
	}
}

func TestSyncClosesTheCasesArgusNoLongerHasOpen(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	for _, key := range []string{"case-a", "case-b"} {
		if _, err := f.svc.Upsert(ctx, "argus", key, casePush("r1")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.Sync(ctx, "argus", []string{"case-a", "not a key"}); !isCode(err, "bad_request") {
		t.Fatalf("invalid key = %v", err)
	}
	if len(f.items()) != 2 {
		t.Fatal("a refused sync resolved something")
	}
	n, err := f.svc.Sync(ctx, "argus", []string{"case-a", "case-unknown"})
	if err != nil || n != 1 {
		t.Fatalf("sync = %d, %v", n, err)
	}
	items := f.items()
	if _, ok := items["case-a"]; !ok || len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
}

func TestRemovingArgusFromTheConfigurationClosesItsItems(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Upsert(ctx, "argus", "case-1", casePush("r1")); err != nil {
		t.Fatal(err)
	}
	attention := NewAttentionService(f.db, nil)
	unconfigured := NewCaseAttentionService(f.db, CaseAttentionOptions{Attention: attention})
	if err := attention.RegisterProducer(unconfigured.Producer()); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if items, _ := attention.List(ctx, AttentionFilter{Producer: "argus"}); len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
	if _, err := unconfigured.Upsert(ctx, "argus", "case-2", casePush("r1")); !isCode(err, "unavailable") {
		t.Fatalf("upsert without Argus = %v", err)
	}
}

func isCode(err error, code string) bool {
	var de *DelegationError
	return errors.As(err, &de) && de.Code == code
}

func TestAFailingPRWithAnArgusCaseIsReportedOnce(t *testing.T) {
	f := newCaseAttentionFixture(t)
	ctx := context.Background()
	if err := f.attention.RegisterProducer(NewPRAttentionAdapter(f.db)); err != nil {
		t.Fatal(err)
	}
	f.task("ci-only", "acme/widget", "feat-x", 12)
	f.task("reviewed", "acme/widget", "feat-y", 13)
	raw, _ := store.MarshalPRMeta(&store.PRMeta{Repo: "acme/widget", Branch: "feat-y", PRNumber: 13, State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", CheckStatus: "fail"})
	if err := f.db.UpdatePRMeta(ctx, "reviewed", raw); err != nil {
		t.Fatal(err)
	}
	prItems := func() map[string]AttentionItem {
		items, err := f.attention.List(ctx, AttentionFilter{Producer: "pull_requests"})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]AttentionItem{}
		for _, item := range items {
			out[item.TaskID] = item
		}
		return out
	}
	if err := f.attention.Reconcile(ctx, "pull_requests"); err != nil {
		t.Fatal(err)
	}
	if len(prItems()) != 2 {
		t.Fatalf("before Argus: %+v", prItems())
	}

	if _, err := f.svc.Upsert(ctx, "argus", "case-12", casePush("r1")); err != nil {
		t.Fatal(err)
	}
	other := casePush("r1")
	other.Target = CaseTarget{Repo: "Acme/Widget", PR: 13}
	if _, err := f.svc.Upsert(ctx, "argus", "case-13", other); err != nil {
		t.Fatal(err)
	}
	items := prItems()
	if _, ok := items["ci-only"]; ok {
		t.Fatalf("the CI-only PR is reported twice: %+v", items["ci-only"])
	}
	if items["reviewed"].Reason != "Changes requested" {
		t.Fatalf("reviewed PR = %+v", items["reviewed"])
	}

	if err := f.svc.Resolve(ctx, "argus", "case-12", "r1", "fixed"); err != nil {
		t.Fatal(err)
	}
	if item, ok := prItems()["ci-only"]; !ok || item.Reason != "CI failing" {
		t.Fatalf("after Argus closed its case: %+v", prItems())
	}
}
