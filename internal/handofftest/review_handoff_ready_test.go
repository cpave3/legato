package handofftest

import (
	"fmt"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A worker's draft rule with ready_after_review sets its handoffs'
// review-loop runs' ready input (LEG-456, spec LEG-559 story 92, AC 27):
// once review passes approve the stack, github.pr.ready marks every draft
// layer ready, bottom up, as the author App. Legato never merges.

// readyHandoff is a reviewed handoff on a worker with these rules, its work
// a stack of two draft pull requests, #31 on main and #32 on #31.
func readyHandoff(t *testing.T, rules ...config.PullRequestRule) reviewed {
	t.Helper()
	r := newReviewedOn(t, WorkerOptions{ReviewPasses: &ReviewPassOptions{}, PullRequests: rules}, nil)
	gh := r.worker.ReviewGitHub
	gh.ClosePR(7) // the harness's own pull request is not this work's
	branches := []string{r.branch, r.branch + "-part-2"}
	base := "main"
	for i, branch := range branches {
		gh.OpenStackedPR(31+i, branch, base, fmt.Sprintf("sha%d", 31+i))
		gh.Draft(31+i, true)
		r.worker.Checkout.Commit(branch, fmt.Sprintf("sha%d", 31+i))
		base = branch
	}
	r.worker.Checkout.Set(r.worker.Tmux.Spawned()[0].WorkDir, branches[1], "sha32")
	return r
}

// approve runs pass 1 clean on the handoff's stack and lets the run go on.
func (r reviewed) approve(t *testing.T) {
	t.Helper()
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Solid fix."}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
}

func TestADraftRuleWithReadyAfterReviewMarksAnApprovedHandoffStackReady(t *testing.T) {
	r := readyHandoff(t, config.PullRequestRule{Match: "acme/*", Mode: config.PullRequestsDraft, ReadyAfterReview: true})
	if l := r.handoffLoop(t); !l.Ready {
		t.Fatalf("the handoff's loop %+v; want ready", l)
	}
	r.approve(t)

	view := r.reviewRun(t)
	if view.Run.Status != "succeeded" {
		t.Fatalf("the review run is %s (%s); want succeeded", view.Run.Status, view.Run.Error)
	}
	if got := r.worker.ReviewGitHub.Readied(); len(got) != 2 || got[0] != 31 || got[1] != 32 {
		t.Fatalf("readied %v; want #31 then #32", got)
	}
	if out := runOutputs(t, view); out["outcome"] != "approved" || out["ready"] != true {
		t.Fatalf("outputs = %v", out)
	}
	if r.column(t) != "Done" || r.handoffState(t) != store.HandoffDone {
		t.Fatalf("column %q, handoff %q", r.column(t), r.handoffState(t))
	}
}

func TestADraftRuleWithoutReadyAfterReviewLeavesTheHandoffStackADraft(t *testing.T) {
	r := readyHandoff(t, config.PullRequestRule{Match: "acme/*", Mode: config.PullRequestsDraft})
	r.approve(t)

	view := r.reviewRun(t)
	if view.Run.Status != "succeeded" || len(r.worker.ReviewGitHub.Readied()) != 0 || runOutputs(t, view)["ready"] != false {
		t.Fatalf("run %q (%s), readied %v, outputs %v", view.Run.Status, view.Run.Error, r.worker.ReviewGitHub.Readied(), runOutputs(t, view))
	}
	if s := stepOf(t, view, "ready"); s.Status != "skipped" {
		t.Fatalf("ready step %q", s.Status)
	}
}

// A layer that will not flip fails the step, and its on_failure pauses the
// handoff's run.
func TestALayerOfAHandoffStackThatWillNotFlipPausesTheRun(t *testing.T) {
	r := readyHandoff(t, config.PullRequestRule{Match: "acme/widget", Mode: config.PullRequestsDraft, ReadyAfterReview: true})
	r.worker.ReviewGitHub.RefuseReady(32)
	r.approve(t)

	ready := stepOf(t, r.reviewRun(t), "ready")
	if ready.Status != service.WorkflowStepPaused || ready.ErrorCode != service.ReviewRefusalReadyFailed {
		t.Fatalf("ready %q (%s): %s", ready.Status, ready.ErrorCode, ready.Error)
	}
	if got := r.worker.ReviewGitHub.Readied(); len(got) != 1 || got[0] != 31 {
		t.Fatalf("readied %v; want #31, below the refusal", got)
	}
	// The review approved: the card is done, whatever the pause.
	if r.column(t) != "Done" || r.handoffState(t) != store.HandoffDone {
		t.Fatalf("column %q, handoff %q", r.column(t), r.handoffState(t))
	}
}
