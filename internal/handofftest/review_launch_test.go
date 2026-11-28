package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// The card menu's and the TUI's review (spec LEG-559 stories 1 to 8, AC 1,
// 2 and 6): the launcher looks the pull request's loop up, opens one that
// holds it whatever started it, and otherwise starts the built-in
// review-loop with agent_author derived from who opened the pull request.

// cardWithPR12 is a board card that tracks #12 of acme/widget, which author
// opened on GitHub, as PR tracking keeps it.
func cardWithPR12(t *testing.T, r reviewRun, author string) {
	t.Helper()
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "card-1", Title: "Greeting", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.worker.Store.TrackTaskBranches(ctx, "card-1", []store.BranchRef{{Repo: "acme/widget", Branch: "feature/greeting"}}, service.BranchSourceManual); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetTaskBranchPR(ctx, "card-1", "acme/widget", "feature/greeting",
		&store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN", Author: author}); err != nil {
		t.Fatal(err)
	}
}

// authorApp is the login of Legato's author App, which the launcher knows
// from github.app_logins.
const authorApp = "legato-author[bot]"

// launcherOn is the card menu's launcher on workflows, which find the
// built-ins as Legato's discovery does, deriving the author from what PR
// tracking keeps.
func launcherOn(r reviewRun, workflows *service.WorkflowService) *service.TaskWorkflowLauncher {
	launcher := service.NewTaskWorkflowLauncher(r.worker.Store, workflows, func() workflowengine.DiscoveryResult {
		return workflowengine.WithBuiltIns(workflowengine.DiscoveryResult{}, service.BuiltInWorkflows()...)
	})
	launcher.SetReviewAuthors(service.NewReviewAuthors(r.worker.Store, []string{"legato-author"}))
	return launcher
}

// reviewItems are the card's review items.
func reviewItems(t *testing.T, launcher *service.TaskWorkflowLauncher) []service.TaskWorkflowOption {
	t.Helper()
	all, err := launcher.List(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	var items []service.TaskWorkflowOption
	for _, item := range all {
		if item.Name == workflowengine.ReviewLoopWorkflowName {
			items = append(items, item)
		}
	}
	return items
}

func TestTheCardMenuStartsReviewLoopAsOneLoopTheRunShows(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	cardWithPR12(t, r, authorApp)
	ctx := context.Background()
	launcher := launcherOn(r, r.runs.Workflows)

	items := reviewItems(t, launcher)
	if len(items) != 1 || items[0].Title != "Review passes · #12 · agent" || items[0].Loop != nil ||
		items[0].Values["repo"] != "acme/widget" || items[0].Values["pr_number"] != "12" || items[0].Values["agent_author"] != "true" {
		t.Fatalf("items = %#v", items)
	}
	// An agent's loop marks ready once approved, keeping its pull
	// requests drafts until then (LEG-712); it sets no steward.
	if items[0].Values["ready"] != "true" {
		t.Fatalf("the item does not set ready: %v", items[0].Values)
	}
	if _, ok := items[0].Values["steward"]; ok {
		t.Fatalf("the item sets steward: %v", items[0].Values)
	}
	run, err := launcher.Start(ctx, items[0].Name, items[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	r.runs.Open(ctx)

	loop, err := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12)
	if err != nil || loop == nil || !strings.HasPrefix(loop.ID, "rl-") || loop.RunID != run.ID || !loop.AgentAuthor || !loop.Ready {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
	view := r.show(t, run.ID)
	if view.Run.WorkflowName != workflowengine.ReviewLoopWorkflowName || outputs(t, view, "open")["loop_id"] != loop.ID {
		t.Fatalf("the run shows %s, open = %v", view.Run.WorkflowName, outputs(t, view, "open"))
	}
}

// A start from a process without the engine (the TUI beside serve) writes
// the run, and the holder's tick opens its loop.
func TestATUIStartWithoutTheEngineOpensItsLoopAtTheHoldersTick(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	cardWithPR12(t, r, authorApp)
	ctx := context.Background()
	tui := service.NewWorkflowService(r.worker.Store, service.NewReviewLoopSteps().Executors())
	launcher := launcherOn(r, tui)

	items := reviewItems(t, launcher)
	run, err := launcher.Start(ctx, items[0].Name, items[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := tui.Show(ctx, run.ID); view.Run.Status != "running" || stepOf(t, view, "open").Status != service.WorkflowStepWaiting {
		t.Fatalf("run %q, open %+v", view.Run.Status, stepOf(t, view, "open"))
	}
	r.tick()
	loop, err := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12)
	if err != nil || loop == nil || loop.RunID != run.ID {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
}

// A pull request any loop holds (here a child run's, which a workflow.run
// step started) offers that loop, not a start; a start on it anyway is
// refused naming the loop and linking to it, and starts no run.
func TestAPullRequestAnyLoopHoldsOpensThatLoopInsteadOfStartingAnother(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	cardWithPR12(t, r, authorApp)
	ctx := context.Background()
	parent := r.startFile(t, callerFile, pr12)
	child := r.childOf(t, parent, "review")
	r.tick()
	launcher := launcherOn(r, r.runs.Workflows)

	items := reviewItems(t, launcher)
	if len(items) != 1 || items[0].Title != "Open review loop · #12" || items[0].Loop == nil ||
		items[0].Loop.Kind != service.ReviewLoopKindChildRun || items[0].Loop.Link != "/workflows/runs/"+child.Run.ID {
		t.Fatalf("items = %#v", items)
	}
	runs, _ := r.worker.Store.ListWorkflowRuns(ctx, store.WorkflowRunFilter{})
	_, err := launcher.Start(ctx, workflowengine.ReviewLoopWorkflowName, map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "false"})
	var refusal *service.WorkflowStepError
	if !errors.As(err, &refusal) || refusal.Code != service.ReviewRefusalLoopActive || refusal.Link != "/workflows/runs/"+child.Run.ID ||
		!strings.Contains(err.Error(), items[0].Loop.LoopID) {
		t.Fatalf("start = %v", err)
	}
	if after, _ := r.worker.Store.ListWorkflowRuns(ctx, store.WorkflowRunFilter{}); len(after) != len(runs) {
		t.Fatalf("a refused start made a run: %d runs, then %d", len(runs), len(after))
	}
}

// The item says "agent" only for a pull request Legato's author App opened;
// a coworker's is the person's, and its loop is not agent-authored.
func TestTheMenuSaysAgentOnlyForAPullRequestTheAuthorAppOpened(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	cardWithPR12(t, r, "octocat")
	launcher := launcherOn(r, r.runs.Workflows)

	items := reviewItems(t, launcher)
	if len(items) != 1 || items[0].Title != "Review passes · #12 · person" || items[0].Values["agent_author"] != "false" {
		t.Fatalf("items = %#v", items)
	}
	run, err := launcher.Start(context.Background(), items[0].Name, items[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	r.runs.Open(context.Background())
	if loop, _ := r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 12); loop == nil || loop.RunID != run.ID || loop.AgentAuthor {
		t.Fatalf("loop = %+v", loop)
	}
}
