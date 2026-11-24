package handofftest

import (
	"context"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// A push while the reviewer worked: GitHub's head is no longer the one the
// pass pinned, so nothing is posted at the old head and the pass is pinned
// again at the new one. The discarded pass is not counted: a loop allowed
// one pass still reviews the new head, and the step says one pass.
func TestAPassWhoseHeadMovedOnGitHubPostsNothingAndIsPinnedAgain(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{MaxPasses: 1})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.worker.ReviewGitHub.Push(12, "ccc333")

	r.submit(t, task, service.ReviewSubmission{Summary: "Clean."})

	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted at the old head: %+v", posted)
	}
	r.tick()
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 1 ||
		r.worker.Checkout.Pins()["refs/legato/review/"+task+"/1/12"] != "ccc333" {
		t.Fatalf("loop = %+v, pins %v", loop, r.worker.Checkout.Pins())
	}

	r.submit(t, task, service.ReviewSubmission{Summary: "Clean."})

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].CommitID != "ccc333" || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted = %+v", posted)
	}
	view = r.show(t, view.Run.ID)
	if out := outputs(t, view, "review"); view.Run.Status != "succeeded" || out["passes"] != float64(1) {
		t.Fatalf("run %q: %+v", view.Run.Status, out)
	}
}

// A layer's head moves after the pass was submitted, before it is posted:
// every layer's head is read again before posting, so no layer of the pass
// is posted, the bottom one whose head stood included.
func TestAStackPassIsNotPostedWhenAnyLayersHeadMovesAfterSubmit(t *testing.T) {
	r := newStackRun(t)
	view := r.start(t, middleOfStack)
	task := view.Run.TaskID
	r.tick()
	r.worker.HoldStarts(true) // the post waits for the next tick
	result := r.submit(t, task, service.ReviewSubmission{Summary: "All good."})
	r.worker.HoldStarts(false)
	if result.Verdict != service.VerdictClean {
		t.Fatalf("submit = %+v", result)
	}

	r.worker.ReviewGitHub.Push(22, "sha22b")
	r.tick()

	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted %s", fmt.Sprint(postedEvents(posted)))
	}
	r.tick()
	prs, _ := r.worker.Store.ListHandoffReviewPRs(context.Background(), task, 1)
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 1 || len(prs) != 3 || prs[1].HeadSHA != "sha22b" {
		t.Fatalf("loop = %+v, pass 1 reviews %+v", loop, prs)
	}
}

// A handoff's author pushes while its pass runs: the main sees the pass
// pinned again as pass 1, and the clean pass at the new head approves it.
func TestTheMainSeesADiscardedPassReviewedAgainAsTheSamePass(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.main.StartConnections()
	r.finish(t)
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.ReviewGitHub.Push(7, "bbb222")

	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Fine."}); err != nil {
		t.Fatal(err)
	}
	r.tick()

	eventually(t, "reviewing at the new head", func() bool {
		a := r.away(t)
		return a.Review != nil && a.Review.Phase == service.ReviewPhaseReviewing && a.Review.WaitingOn == ""
	})
	if got := r.away(t).Review; got.Pass != 1 || got.Summary() != "Reviewing · pass 1" {
		t.Fatalf("the main sees %+v (%q)", got, got.Summary())
	}
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Fine."}); err != nil {
		t.Fatal(err)
	}
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].CommitID != "bbb222" || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted = %+v", posted)
	}
	eventually(t, "approved", func() bool { return r.away(t).Review.Phase == service.ReviewPhaseClean })
	if got := r.away(t).Review.Pass; got != 1 {
		t.Fatalf("the main sees pass %d", got)
	}
}
