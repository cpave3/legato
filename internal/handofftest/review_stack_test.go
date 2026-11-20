package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// stacked makes the handed-off work a stack of five pull requests, #137 to
// #141, built with plain branches: no Staccato graph, HEAD at the top.
func (r reviewed) stacked(t *testing.T) []string {
	t.Helper()
	gh := r.worker.ReviewGitHub
	branches := []string{r.branch}
	for i := 2; i <= 5; i++ {
		branches = append(branches, fmt.Sprintf("%s-part-%d", r.branch, i))
	}
	base := "main"
	for i, branch := range branches {
		gh.OpenStackedPR(137+i, branch, base, fmt.Sprintf("sha%d", 137+i))
		r.worker.Checkout.Commit(branch, fmt.Sprintf("sha%d", 137+i))
		base = branch
	}
	// The clone has the top branch checked out and knows no stack.
	r.worker.Checkout.Set(r.worker.Tmux.Spawned()[0].WorkDir, branches[4], "sha141")
	return branches
}

func TestAStackBuiltWithPlainBranchesIsReviewedWholeInPassOne(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.ClosePR(7) // the harness's own pull request is not this work's
	branches := r.stacked(t)

	r.finish(t)

	if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 1 {
		t.Fatalf("loop %+v", loop)
	}
	prs, err := r.worker.Store.ListHandoffReviewPRs(context.Background(), r.card, 1)
	if err != nil || len(prs) != 5 {
		t.Fatalf("pass 1 reviews %d pull requests, %v", len(prs), err)
	}
	for i, pr := range prs {
		base := "main"
		if i > 0 {
			base = branches[i-1]
		}
		if pr.Number != 137+i || pr.Branch != branches[i] || pr.Base != base {
			t.Fatalf("pull request %d: %+v", i, pr)
		}
	}
	spawned := r.worker.Tmux.Spawned()
	brief := briefOf(t, spawned[len(spawned)-1])
	for i := range prs {
		if !strings.Contains(brief, fmt.Sprintf("### #%d", 137+i)) {
			t.Fatalf("the brief leaves out #%d", 137+i)
		}
	}
	if !strings.Contains(brief, "branch `"+branches[2]+"`, base `"+branches[1]+"`") {
		t.Fatal("the brief does not give a layer its own base")
	}
}

func TestALayerTheCloneLacksIsFetchedForReviewNotAMismatch(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.ClosePR(7)
	gh := r.worker.ReviewGitHub
	// The author has only the bottom branch; someone stacked a layer on
	// it from another clone (the same author App).
	gh.OpenStackedPR(137, r.branch, "main", "sha137")
	gh.OpenStackedPR(138, r.branch+"-top", r.branch, "sha138")
	r.worker.Checkout.Set(r.worker.Tmux.Spawned()[0].WorkDir, r.branch, "sha137")

	r.finish(t)

	if typed := r.worker.typedTo("legato-" + r.card); strings.Contains(typed, "Push or discard") {
		t.Fatalf("a fetched layer was a mismatch: %q", typed)
	}
	prs, _ := r.worker.Store.ListHandoffReviewPRs(context.Background(), r.card, 1)
	if len(prs) != 2 || r.worker.Checkout.Pins()["refs/legato/review/"+r.card+"/1/138"] != "sha138" {
		t.Fatalf("pass 1: %+v, pins %v", prs, r.worker.Checkout.Pins())
	}
	if fetched := r.worker.ReviewGitHub.Fetched(); len(fetched) != 1 || fetched[0] != 138 {
		t.Fatalf("fetched %v", fetched)
	}
}

func TestAStackTheLoopFoundIsWhatTheWorkerReportsAndTheMainTracks(t *testing.T) {
	gh := NewFakeGitHub(t)
	r := newReviewedWith(t, ReviewPassOptions{}, gh)
	r.worker.ReviewGitHub.ClosePR(7)
	branches := r.stacked(t)
	var prs []FakePR
	base := "main"
	for i, b := range branches {
		prs = append(prs, FakePR{Number: 137 + i, Head: b, Base: base})
		base = b
	}
	gh.Set("acme/widget", prs...)
	r.main.StartConnections()

	r.finish(t)

	var want []string
	for i, b := range branches {
		want = append(want, fmt.Sprintf("%s (api) #%d", b, 137+i))
	}
	eventuallyTracked(t, r.main, r.taskID, want)
	if got := r.away(t).Branches; strings.Join(got, ",") != strings.Join(branches, ",") {
		t.Fatalf("the worker reports %v", got)
	}
}
