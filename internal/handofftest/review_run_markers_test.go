package handofftest

import (
	"context"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

const twoReviews = `schema_version: 1
name: review-twice
title: Review twice
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
steps:
  - id: first
    type: github.pr.review_passes
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
      scope: pr
  - id: second
    type: github.pr.review_passes
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
      scope: pr
`

// Two review steps of one run are two loops on the run's task, each with
// its own pass 1: each posts its own review, so the second is not taken
// for the first's when the pull request moved on between them (LEG-605
// review).
func TestTwoLoopsOnOneTaskPostTheirOwnReviews(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	definition, err := workflowengine.Parse("review-twice.workflow.yaml", []byte(twoReviews))
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.runs.Workflows.Start(ctx, definition, pr12)
	if err != nil {
		t.Fatal(err)
	}
	r.runs.Open(ctx)
	task := view.Run.TaskID
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "Looks right."}) // approves bbb222; the first step succeeds

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.runs.Open(ctx) // the second step's loop
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "Still right."})

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 2 || posted[0].CommitID != "bbb222" || posted[1].CommitID != "ccc333" || posted[0].Marker == posted[1].Marker {
		t.Fatalf("posted = %+v", posted)
	}
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
}
