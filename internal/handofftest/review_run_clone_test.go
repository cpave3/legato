package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewClone is the clone a run's review step asked for.
func (r reviewRun) reviewClone(t *testing.T, view *store.WorkflowRunView) service.JobCloneRequest {
	t.Helper()
	for _, req := range r.worker.Repos.Clones() {
		if req.TaskID == view.Run.TaskID {
			return req
		}
	}
	t.Fatalf("the run asked for no clone: %+v", r.worker.Repos.Clones())
	return service.JobCloneRequest{}
}

func (r reviewRun) lastBrief(t *testing.T) string {
	t.Helper()
	spawns := r.worker.Tmux.Spawned()
	if len(spawns) == 0 {
		t.Fatal("no reviewer started")
	}
	return briefOf(t, spawns[len(spawns)-1])
}

func TestARunsReviewerGetsACloneOfItsOwnOnThePullRequestsHeadBranch(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	req := r.reviewClone(t, view)
	if !req.Own {
		t.Fatal("the reviewer may join the run task's clone, an author's")
	}
	if req.FreshBranch != "feature/greeting" || req.Ref != "bbb222" {
		t.Fatalf("clone on %q at %q, want feature/greeting at the head", req.FreshBranch, req.Ref)
	}
	r.tick()
	if _, branches, _ := r.worker.Checkout.Branches(context.Background(), "", ""); len(branches) != 1 || branches[0] != "feature/greeting" {
		t.Fatalf("checkout on %v", branches)
	}
	brief := r.lastBrief(t)
	if !strings.Contains(brief, "branch `feature/greeting`, base `main`") || !strings.Contains(brief, "`bbb222`") {
		t.Fatalf("the brief does not name the head branch and commit:\n%s", brief)
	}
	if !strings.Contains(brief, "Your checkout is on `feature/greeting`") {
		t.Fatalf("the brief does not say where the checkout is:\n%s", brief)
	}
}

func TestAForkedHeadIsReviewedOnALegatoBranchThatTheBriefExplains(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.OpenForkPR(14, "patch-1", "ccc333", "someone/widget")
	view := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "14"})
	if req := r.reviewClone(t, view); req.FreshBranch != "legato-review/pr-14" {
		t.Fatalf("a fork's head cloned on %q", req.FreshBranch)
	}
	r.tick()
	brief := r.lastBrief(t)
	if !strings.Contains(brief, "branch `patch-1` (in someone/widget), base `main`") {
		t.Fatalf("the brief does not name GitHub's head branch:\n%s", brief)
	}
	if !strings.Contains(brief, "Your checkout is on `legato-review/pr-14` at `ccc333`") {
		t.Fatalf("the brief does not name the local branch:\n%s", brief)
	}
}

func TestEveryLayerOfAStackIsCheckedOutOnItsOwnHeadBranch(t *testing.T) {
	r := newStackRun(t)
	view := r.start(t, middleOfStack)
	if req := r.reviewClone(t, view); req.FreshBranch != "stack/two" {
		t.Fatalf("clone on %q", req.FreshBranch)
	}
	r.tick()
	brief := r.lastBrief(t)
	for i, branch := range []string{"stack/one", "stack/two", "stack/three"} {
		if !strings.Contains(brief, fmt.Sprintf("Your checkout is on `%s` at `sha%d`", branch, 21+i)) {
			t.Fatalf("layer %s:\n%s", branch, brief)
		}
	}
	feedback, err := service.ReviewFeedback(context.Background(), r.worker.Store, view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	for i, pr := range feedback.PRs {
		if want := []string{"stack/one", "stack/two", "stack/three"}[i]; pr.Branch != want {
			t.Fatalf("feedback names #%d's branch %q, want GitHub's %q", pr.Number, pr.Branch, want)
		}
	}
}
