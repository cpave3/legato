package handofftest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// newStackRun is a worker whose acme/widget has a stack of three pull
// requests, #21 on main, #22 on #21, #23 on #22.
func newStackRun(t *testing.T) reviewRun {
	t.Helper()
	r := newReviewRun(t, ReviewPassOptions{})
	base := "main"
	for i, branch := range []string{"stack/one", "stack/two", "stack/three"} {
		r.worker.ReviewGitHub.OpenStackedPR(21+i, branch, base, fmt.Sprintf("sha%d", 21+i))
		base = branch
	}
	return r
}

var middleOfStack = map[string]string{"repo": "acme/widget", "pr_number": "22"}

func TestAStackRunReviewsEveryLayerAgainstItsOwnBaseInOnePass(t *testing.T) {
	r := newStackRun(t)
	ctx := context.Background()

	view := r.start(t, middleOfStack)
	if view.Run.Status != "running" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Steps[0].Error)
	}
	task := view.Run.TaskID
	if shown := r.show(t, view.Run.ID); shown.Run.WorkflowTitle != "Review loop on acme/widget#21–#23" {
		t.Fatalf("title %q", shown.Run.WorkflowTitle)
	}
	r.tick()

	prs, err := r.worker.Store.ListHandoffReviewPRs(ctx, task, 1)
	if err != nil || len(prs) != 3 {
		t.Fatalf("pass 1 reviews %+v, %v", prs, err)
	}
	for i, base := range []string{"main", "stack/one", "stack/two"} {
		if prs[i].Number != 21+i || prs[i].Base != base || prs[i].HeadSHA != fmt.Sprintf("sha%d", 21+i) {
			t.Fatalf("layer %d: %+v", i, prs[i])
		}
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "All good."})

	if got := fmt.Sprint(postedEvents(r.worker.ReviewGitHub.Posted())); got != "[#21 APPROVE #22 APPROVE #23 APPROVE]" {
		t.Fatalf("posted %s", got)
	}
	view = r.show(t, view.Run.ID)
	out := outputs(t, view, "review")
	if view.Run.Status != "succeeded" || out["outcome"] != "approved" {
		t.Fatalf("run %q: %+v", view.Run.Status, out)
	}
	if reviews, _ := out["reviews"].([]any); len(reviews) != 3 {
		t.Fatalf("reviews %+v", out["reviews"])
	}
}

func TestScopePRReviewsOnlyTheNamedPullRequest(t *testing.T) {
	r := newStackRun(t)
	view := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "22", "scope": "pr"})
	r.tick()

	prs, _ := r.worker.Store.ListHandoffReviewPRs(context.Background(), view.Run.TaskID, 1)
	if len(prs) != 1 || prs[0].Number != 22 {
		t.Fatalf("pass 1 reviews %+v", prs)
	}
	if shown := r.show(t, view.Run.ID); shown.Run.WorkflowTitle != "Review loop on acme/widget#22" {
		t.Fatalf("title %q", shown.Run.WorkflowTitle)
	}
}

func TestAPushToTheBottomWithARestackStartsAPassOverWhatChanged(t *testing.T) {
	r := newStackRun(t)
	ctx := context.Background()
	view := r.start(t, middleOfStack)
	task := view.Run.TaskID
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "A bug at the bottom.", Findings: []service.SubmittedFinding{
		{PR: 21, Severity: "blocking", Title: "Greets nobody", Body: "An empty name prints a bare comma.", FailureScenario: "greet(\"\") prints \"Hello, \"."},
	}})
	if got := fmt.Sprint(postedEvents(r.worker.ReviewGitHub.Posted())); got != "[#21 COMMENT #22 APPROVE #23 APPROVE]" {
		t.Fatalf("posted %s", got)
	}

	// The owner fixes the bottom and restacks: every head moves.
	for i, branch := range []string{"stack/one", "stack/two", "stack/three"} {
		base := "main"
		if i > 0 {
			base = []string{"stack/one", "stack/two"}[i-1]
		}
		r.worker.ReviewGitHub.OpenStackedPR(21+i, branch, base, fmt.Sprintf("sha%d-b", 21+i))
	}
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()

	if loop := r.loopPhase(t, task); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("loop %+v", loop)
	}
	prs, _ := r.worker.Store.ListHandoffReviewPRs(ctx, task, 2)
	if len(prs) != 3 || prs[0].HeadSHA != "sha21-b" || prs[2].HeadSHA != "sha23-b" {
		t.Fatalf("pass 2 reviews %+v", prs)
	}
}

func TestASecondRunOnAnyLayerIsRefusedNamingTheFirst(t *testing.T) {
	r := newStackRun(t)
	first := r.start(t, middleOfStack)

	for _, number := range []string{"21", "23"} {
		second := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": number, "scope": "pr"})
		refused(t, second, "already has an active review loop: run "+first.Run.ID)
	}
}
