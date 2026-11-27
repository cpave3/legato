package handofftest

import (
	"context"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
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

// A step still waiting to open holds its pull request before its loop
// opens: an open step (review-loop) as much as an inline passes step, and
// either refuses the other.
func TestAStepWaitingToOpenHoldsItsPullRequest(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		definition func() (*workflowengine.Definition, error)
	}{
		{name: "open step", definition: func() (*workflowengine.Definition, error) { return workflowengine.BuiltInReviewLoop(), nil }},
		{name: "inline passes step", definition: oldReviewPassesCopy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			definition, err := tc.definition()
			if err != nil {
				t.Fatal(err)
			}
			// Started without the holder's tick: the step waits to open.
			view, err := r.runs.Workflows.Start(ctx, definition, map[string]string{"repo": "Acme/Widget", "pr_number": "12"})
			if err != nil {
				t.Fatal(err)
			}
			active := r.worker.Reviews.ActiveLoopOn(ctx, "acme/widget", 12)
			if active == nil || active.Name != "run "+view.Run.ID || active.Link != "/workflows/runs/"+view.Run.ID {
				t.Fatalf("active on #12: %+v", active)
			}
			if other := r.worker.Reviews.ActiveLoopOn(ctx, "acme/widget", 13); other != nil {
				t.Fatalf("active on #13: %+v", other)
			}
		})
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
