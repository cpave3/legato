package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// A review post GitHub refuses with a 4xx: GitHub saw it and took nothing,
// so the loop is stuck for the operator and the pass ends. An ended pass
// holds no reviewer slot, and Another pass starts the next pass, which
// posts once.

func TestARefusedPostEndsItsPassFreesItsSlotAndAnotherPassStartsTheNext(t *testing.T) {
	ctx := context.Background()
	r := newReviewRun(t, ReviewPassOptions{MaxConcurrent: 1})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.worker.ReviewGitHub.RefusePosts(1)

	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})

	loop := r.loopPhase(t, task)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "GitHub refused the review of #12") {
		t.Fatalf("loop = %+v", loop)
	}
	if pass, _ := r.worker.Store.GetHandoffReviewPass(ctx, task, 1); pass.State == service.ReviewPassSubmitted || pass.State == service.ReviewPassReviewing {
		t.Fatalf("pass 1 still in flight: %+v", pass)
	}
	if n, err := r.worker.Store.ReviewPassesInFlight(ctx); err != nil || n != 0 {
		t.Fatalf("passes in flight = %d, %v", n, err)
	}
	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewStuck {
		t.Fatalf("items = %+v", items)
	}

	// The only reviewer is free: a second loop starts its pass.
	r.worker.ReviewGitHub.OpenPR(13, "feature/other", "ddd444")
	other := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "13"})
	r.tick()
	if phase := r.loopPhase(t, other.Run.TaskID).Phase; phase != service.ReviewPhaseReviewing || !r.reviewer(t, other.Run.TaskID) {
		t.Fatalf("second loop %q, reviewer %v", phase, r.reviewer(t, other.Run.TaskID))
	}
	r.submit(t, other.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})

	// Another pass starts pass 2, which posts once.
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(ctx, service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "another_pass"}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loopPhase(t, task); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing || !r.reviewer(t, task) {
		t.Fatalf("after another pass: loop %+v, reviewer %v", loop, r.reviewer(t, task))
	}
	// Pass 1's finding stands, as it did when its pass stayed submitted:
	// pass 2 rules on it.
	findings, _ := r.worker.Store.ListHandoffReviewFindings(ctx, task)
	if len(findings) != 1 || findings[0].Pass != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Still one problem.",
		Rulings: []service.SubmittedRuling{{Finding: findings[0].ID, Status: "open", Reason: "not fixed"}}})

	var on12 []service.ReviewPost
	for _, post := range r.worker.ReviewGitHub.Posted() {
		if post.Number == 12 {
			on12 = append(on12, post)
		}
	}
	if len(on12) != 1 || !strings.Contains(on12[0].Marker, "/pass-2/pr-12") {
		t.Fatalf("reviews of #12 = %+v", on12)
	}
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor || loop.Pass != 2 {
		t.Fatalf("loop = %+v", loop)
	}
}
