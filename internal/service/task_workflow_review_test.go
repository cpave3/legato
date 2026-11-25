package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// authorApp is Legato's author App's login, as PR tracking keeps a pull
// request's author.
const authorApp = "legato-author[bot]"

func launcherWithReviewPasses(t *testing.T, prMeta *store.PRMeta) *TaskWorkflowLauncher {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Card", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if prMeta != nil {
		ctx := context.Background()
		if _, err := db.TrackTaskBranches(ctx, "task-1", []store.BranchRef{{Repo: prMeta.Repo, Branch: prMeta.Branch}}, BranchSourceManual); err != nil {
			t.Fatal(err)
		}
		if err := db.SetTaskBranchPR(ctx, "task-1", prMeta.Repo, prMeta.Branch, prMeta); err != nil {
			t.Fatal(err)
		}
	}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewTaskNoteExecutor(db)})
	launcher := NewTaskWorkflowLauncher(db, workflows, func() workflowengine.DiscoveryResult {
		return workflowengine.WithBuiltIns(workflowengine.DiscoveryResult{}, BuiltInWorkflows()...)
	})
	launcher.SetReviewAuthors(NewReviewAuthors(db, []string{"legato-author"}))
	return launcher
}

func TestACardWithAPullRequestOffersAReviewLoopWithItsAuthorModeDerived(t *testing.T) {
	launcher := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: authorApp})
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "review-loop" || items[0].Title != "Review passes · #12 · agent" || items[0].Values["repo"] != "acme/widget" ||
		items[0].Values["pr_number"] != "12" || items[0].Values["agent_author"] != "true" {
		t.Fatalf("items = %#v", items)
	}
}

// A pull request a person opened is a person's.
func TestACardWithAPersonsPullRequestOffersItAsAPersons(t *testing.T) {
	launcher := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: "octocat"})
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil || len(items) != 1 || items[0].Title != "Review passes · #12 · person" || items[0].Values["agent_author"] != "false" {
		t.Fatalf("items = %#v, %v", items, err)
	}
}

func TestACardWithNoPullRequestOffersNoReviewPasses(t *testing.T) {
	launcher := launcherWithReviewPasses(t, nil)
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %#v, %v", items, err)
	}
}

func TestACardWhosePullRequestsAreAStackOffersTheStackFirst(t *testing.T) {
	launcher := launcherWithReviewPasses(t, nil)
	ctx := context.Background()
	db := launcher.store
	var refs []store.BranchRef
	for _, b := range []string{"s1", "s2", "s3"} {
		refs = append(refs, store.BranchRef{Repo: "acme/widget", Branch: b})
	}
	if _, err := db.TrackTaskBranches(ctx, "task-1", refs, BranchSourceStack); err != nil {
		t.Fatal(err)
	}
	for i, b := range []string{"s1", "s2", "s3"} {
		if err := db.SetTaskBranchPR(ctx, "task-1", "acme/widget", b, &store.PRMeta{Repo: "acme/widget", Branch: b, PRNumber: 137 + i, State: "OPEN", Author: authorApp}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := launcher.List(ctx, "task-1")
	if err != nil || len(items) != 4 {
		t.Fatalf("items = %#v, %v", items, err)
	}
	if items[0].Title != "Review passes · stack #137–#139 · agent" || items[0].Values["scope"] != "stack" || items[0].Values["pr_number"] != "137" || items[0].Values["agent_author"] != "true" {
		t.Fatalf("the stack item = %#v", items[0])
	}
	if items[2].Title != "Review passes · #138 · agent" || items[2].Values["scope"] != "pr" {
		t.Fatalf("a layer's item = %#v", items[2])
	}
}

// A stack with a person's pull request in it is a person's.
func TestAStackWithAPersonsPullRequestIsAPersons(t *testing.T) {
	launcher := launcherWithReviewPasses(t, nil)
	ctx := context.Background()
	db := launcher.store
	if _, err := db.TrackTaskBranches(ctx, "task-1", []store.BranchRef{{Repo: "acme/widget", Branch: "s1"}, {Repo: "acme/widget", Branch: "s2"}}, BranchSourceStack); err != nil {
		t.Fatal(err)
	}
	for i, b := range []string{"s1", "s2"} {
		author := []string{authorApp, "octocat"}[i]
		if err := db.SetTaskBranchPR(ctx, "task-1", "acme/widget", b, &store.PRMeta{Repo: "acme/widget", Branch: b, PRNumber: 139 + i, State: "OPEN", Author: author}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := launcher.List(ctx, "task-1")
	if err != nil || len(items) != 3 || items[0].Title != "Review passes · stack #139–#140 · person" || items[0].Values["agent_author"] != "false" ||
		items[1].Title != "Review passes · #139 · agent" || items[2].Title != "Review passes · #140 · person" {
		t.Fatalf("items = %#v, %v", items, err)
	}
}

// With no Legato on the data folder running the review engine, a card's
// menu offers no review and says why (spec LEG-559 D9, AC 5).
func TestACardOffersNoReviewPassesWithoutTheReviewEngineAndSaysWhy(t *testing.T) {
	launcher := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: authorApp})
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		enabled bool
		reason  string
	}{
		{"review passes off", false, "Review passes are off: turn on review_passes.enabled (Settings › Review passes)."},
		{"no process runs the engine", true, "Review passes need `legato serve` running."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher.SetReviewEngine(func() ReviewEngineStatus { return ReviewEngineStatusOf(tc.enabled, false) })
			items, err := launcher.List(ctx, "task-1")
			if err != nil || len(items) != 0 {
				t.Fatalf("items = %#v, %v", items, err)
			}
			if got := launcher.ReviewUnavailable(ctx, "task-1"); got != tc.reason {
				t.Fatalf("reason = %q", got)
			}
		})
	}
}

// The engine in another process on the data folder is enough: the item is
// offered, whatever this process's own setting says.
func TestACardOffersReviewPassesWhenAnotherProcessRunsTheEngine(t *testing.T) {
	launcher := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: authorApp})
	ctx := context.Background()
	launcher.SetReviewEngine(func() ReviewEngineStatus { return ReviewEngineStatusOf(false, true) })
	items, err := launcher.List(ctx, "task-1")
	if err != nil || len(items) != 1 || items[0].Title != "Review passes · #12 · agent" {
		t.Fatalf("items = %#v, %v", items, err)
	}
	if got := launcher.ReviewUnavailable(ctx, "task-1"); got != "" {
		t.Fatalf("reason = %q", got)
	}
}

// A card with nothing to review says nothing of the engine.
func TestACardWithNoPullRequestSaysNothingOfTheReviewEngine(t *testing.T) {
	launcher := launcherWithReviewPasses(t, nil)
	launcher.SetReviewEngine(func() ReviewEngineStatus { return ReviewEngineStatusOf(true, false) })
	if got := launcher.ReviewUnavailable(context.Background(), "task-1"); got != "" {
		t.Fatalf("reason = %q", got)
	}
}

// GET /api/review-loops asks of a pull request alone: the card that tracks
// it says whose it is, and one no card tracks is a person's.
func TestAPullRequestAloneIsAnAgentsOnlyWhenTheCardThatTracksItSaysSo(t *testing.T) {
	launcher := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: authorApp})
	authors := NewReviewAuthors(launcher.store, []string{"legato-author"})
	ctx := context.Background()
	if !authors.AgentAuthor(ctx, "ACME/widget", 12) {
		t.Fatal("the author App's pull request is a person's")
	}
	if authors.AgentAuthor(ctx, "acme/widget", 13) {
		t.Fatal("a pull request no card tracks is an agent's")
	}
	if err := launcher.store.SetTaskBranchPR(ctx, "task-1", "acme/widget", "feature/greeting", &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: "octocat"}); err != nil {
		t.Fatal(err)
	}
	if authors.AgentAuthor(ctx, "acme/widget", 12) {
		t.Fatal("a person's pull request is an agent's")
	}
}
