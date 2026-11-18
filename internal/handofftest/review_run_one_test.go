package handofftest

import (
	"context"
	"testing"
)

func TestASecondReviewLoopOnAPullRequestIsRefusedWithALinkToTheFirst(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	first := r.start(t, pr12)
	second := r.start(t, pr12)
	refused(t, second, "already has an active review loop: run "+first.Run.ID)
	refused(t, second, "/workflows/runs/"+first.Run.ID)

	if _, err := r.runs.Workflows.Cancel(context.Background(), first.Run.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if third := r.start(t, pr12); third.Run.Status != "running" {
		t.Fatalf("after the first ended: %q %s", third.Run.Status, third.Steps[0].Error)
	}
}

func TestAReviewLoopOnAPullRequestAHandoffReviewsIsRefused(t *testing.T) {
	h := newReviewed(t, ReviewPassOptions{})
	h.finish(t) // pass 1 of the handoff's loop on #7
	runs := StartReviewRuns(t, h.worker)
	view, err := runs.Start(context.Background(), map[string]string{"repo": "acme/widget", "pr_number": "7"})
	if err != nil {
		t.Fatal(err)
	}
	refused(t, view, "already has an active review loop: handed-off task "+h.card)
	refused(t, view, "/board?highlight_task="+h.card)
}
