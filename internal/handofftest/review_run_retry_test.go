package handofftest

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// latestAttempt is the newest attempt of a run's step.
func latestAttempt(t testing.TB, view *store.WorkflowRunView, step string) store.WorkflowStepAttempt {
	t.Helper()
	var found *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if a := view.Attempts[i]; a.Path == step && (found == nil || a.AttemptNumber > found.AttemptNumber) {
			found = &view.Attempts[i]
		}
	}
	if found == nil {
		t.Fatalf("no attempt of %s", step)
	}
	return *found
}

// retry retries the run's failed review step and opens it, as the holder's
// tick does.
func (r reviewRun) retry(t *testing.T, runID string) *store.WorkflowRunView {
	t.Helper()
	ctx := context.Background()
	if _, err := r.runs.Workflows.Retry(ctx, runID, "review", false); err != nil {
		t.Fatalf("retry: %v", err)
	}
	r.runs.Open(ctx)
	view := r.show(t, runID)
	if view.Run.Status != "running" || view.Steps[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("after retry: run %q, step %q: %s", view.Run.Status, view.Steps[0].Status, view.Run.Error)
	}
	return view
}

// The operator fails a run's review step that waits for its author, then
// retries it: the step resumes the same loop, with pass 1, its finding and
// its item, and the author's push starts pass 2, not pass 1 again.
func TestRetryAfterAFailureResumesTheSameReviewLoop(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	task := view.Run.TaskID
	findings, _ := r.worker.Store.ListHandoffReviewFindings(ctx, task)
	items, _ := r.worker.Store.ListHandoffReviewItems(ctx, task)
	if len(findings) != 1 || len(items) != 1 {
		t.Fatalf("after pass 1: findings %+v, items %+v", findings, items)
	}

	attempt := latestAttempt(t, r.show(t, view.Run.ID), "review")
	if _, err := r.runs.Workflows.Fail(ctx, view.Run.ID, "review", attempt.ID, "the operator failed it"); err != nil {
		t.Fatal(err)
	}
	if got := r.show(t, view.Run.ID); got.Run.Status != "failed" {
		t.Fatalf("run %q after fail", got.Run.Status)
	}
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor || loop.Pass != 1 {
		t.Fatalf("a failed run's loop = %+v; want it kept for Retry", loop)
	}

	view = r.retry(t, view.Run.ID)
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor || loop.Pass != 1 {
		t.Fatalf("the retried loop = %+v; want pass 1 waiting for its author", loop)
	}
	again, _ := r.worker.Store.ListHandoffReviewFindings(ctx, task)
	if len(again) != 1 || again[0].ID != findings[0].ID || again[0].Status != "open" {
		t.Fatalf("findings after retry = %+v", again)
	}
	if again, _ := r.worker.Store.ListHandoffReviewItems(ctx, task); len(again) != 1 || again[0].ID != items[0].ID {
		t.Fatalf("items after retry = %+v", again)
	}
	r.tick()
	if r.reviewer(t, task) || len(r.worker.ReviewGitHub.Posted()) != 1 {
		t.Fatal("the retried loop reviewed again with nothing from its author")
	}

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if loop := r.loopPhase(t, task); !r.reviewer(t, task) || loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("no pass 2 after the push: %+v", loop)
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+task+"/2/12"] != "ccc333" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: findings[0].ID, Status: "resolved", Reason: "guarded"}}})
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "succeeded" || outputs(t, got, "review")["passes"] != float64(2) {
		t.Fatalf("run %q: %s", got.Run.Status, got.Run.Error)
	}
}

// A failed run keeps its loop, and the loop's claim, for Retry; the
// operator who gives up on it cancels it instead: the run is cancelled, the
// holder's next tick closes its loop, and the pull request is free for
// another review (LEG-605 review).
func TestCancellingAFailedReviewRunFreesItsPullRequest(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	attempt := latestAttempt(t, r.show(t, view.Run.ID), "review")
	if _, err := r.runs.Workflows.Fail(ctx, view.Run.ID, "review", attempt.ID, "the operator failed it"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held == nil {
		t.Fatal("the failed run's loop gave up its claim on #12 before Retry or Cancel")
	}

	cancelled, err := r.runs.Workflows.Cancel(ctx, view.Run.ID, "not needed")
	if err != nil {
		t.Fatalf("cancel a failed run: %v", err)
	}
	if cancelled.Run.Status != "cancelled" {
		t.Fatalf("run %q after cancel", cancelled.Run.Status)
	}
	r.tick()
	loop, _ := r.worker.Store.CurrentReviewLoop(ctx, view.Run.TaskID)
	if loop == nil || loop.State != store.ReviewLoopClosedState || loop.Outcome != service.ReviewOutcomeCancelled {
		t.Fatalf("loop = %+v; want it closed cancelled", loop)
	}
	again := r.start(t, pr12)
	if again.Run.Status != "running" || again.Steps[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("another run on #12: %q: %s", again.Run.Status, again.Run.Error)
	}
}

// A step that failed while its pass 2 was in flight resumes on Retry with
// that pass pinned again under its number, 2, following the last posted
// pass: no primary-key error, and no pass 3.
func TestRetryOfAStepThatFailedMidPassPinsThatPassAgain(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	task := view.Run.TaskID
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if loop := r.loopPhase(t, task); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("no pass 2: %+v", loop)
	}

	// A failure that did not go through the step's executor (the run's
	// own service failing it): the loop is left as it was.
	attempt := latestAttempt(t, r.show(t, view.Run.ID), "review")
	if _, err := r.runs.Workflows.FailWaiting(ctx, view.Run.ID, "review", attempt.ID, "lost"); err != nil {
		t.Fatal(err)
	}
	r.retry(t, view.Run.ID)
	if r.reviewer(t, task) {
		t.Fatal("the failed attempt's reviewer still runs after Retry")
	}
	if loop := r.loopPhase(t, task); loop.Pass != 1 || loop.Phase != service.ReviewPhaseReady {
		t.Fatalf("the retried loop = %+v; want pass 2 to start again", loop)
	}
	r.tick()
	loop := r.loopPhase(t, task)
	if loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing || !r.reviewer(t, task) {
		t.Fatalf("after the retry's tick: %+v", loop)
	}
	pass, err := r.worker.Store.GetHandoffReviewPass(ctx, task, 2)
	if err != nil || pass.State != service.ReviewPassReviewing || pass.Attempt != 1 {
		t.Fatalf("pass 2 = %+v, %v; want it pinned again", pass, err)
	}
	if _, err := r.worker.Store.GetHandoffReviewPass(ctx, task, 3); err == nil {
		t.Fatal("a pass 3 was pinned")
	}
}
