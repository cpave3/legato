package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var (
	buildFails = service.CheckResult{Name: "build", State: service.CheckFailing, Conclusion: "failure", Summary: "2 tests failed",
		URL: "https://github.com/acme/widget/runs/77", Excerpt: "login_test.go:40: expected 3, got 4"}
	buildPasses = service.CheckResult{Name: "build", State: service.CheckPassing, Conclusion: "success"}
	buildRuns   = service.CheckResult{Name: "build", State: service.CheckPending}
)

func (r reviewed) checkItems(t *testing.T) []store.HandoffReviewItem {
	t.Helper()
	view, err := service.ReviewFeedback(context.Background(), r.worker.Store, r.card)
	if err != nil {
		t.Fatal(err)
	}
	var checks []store.HandoffReviewItem
	for _, item := range view.Items {
		if item.Kind == service.ItemCheck {
			checks = append(checks, item)
		}
	}
	return checks
}

func TestAFailingRequiredCheckIsFeedbackUntilItPassesOnANewHead(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("aaa111", buildFails)

	// The pass runs without waiting for CI, and approves.
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	if r.handoffState(t) == "done" {
		t.Fatal("done with the required check failing")
	}
	r.tick()
	checks := r.checkItems(t)
	if len(checks) != 1 {
		t.Fatalf("check items %+v", checks)
	}
	item := checks[0]
	if item.ID != "k-7-build" || item.URL != buildFails.URL || !strings.Contains(item.Body, "2 tests failed") || !strings.Contains(item.Body, "login_test.go:40") {
		t.Fatalf("check item %+v", item)
	}
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseWaitingForAuthor || r.column(t) != "Doing" {
		t.Fatalf("after the failure: phase %q, column %q", loop.Phase, r.column(t))
	}
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, "1 failing check") || strings.Contains(typed, "login_test") {
		t.Fatalf("typed to the author: %q", typed)
	}
	if block := r.worker.Reviews.ReviewBlock(context.Background(), r.card); block.ChecksOpen != 1 {
		t.Fatalf("block %+v", block)
	}

	// Fixed on a new head, where the check passes: pass 2 is clean and
	// the handoff done.
	r.answer(t, "k-7-build", service.AnswerFixed, "the count was off by one")
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.worker.ReviewGitHub.SetChecks("bbb222", buildPasses)
	r.worker.Clock.Advance(2 * time.Minute)
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Still good."}); err != nil {
		t.Fatal(err)
	}
	if state := r.handoffState(t); state != "done" {
		t.Fatalf("handoff %s after the check passed on bbb222", state)
	}
}

func TestFixedAloneDoesNotHandleACheckThatStillFails(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("aaa111", buildFails)
	r.finish(t)
	_, _ = r.submit(t, service.ReviewSubmission{Summary: "Good."})
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()

	r.answer(t, "k-7-build", service.AnswerFixed, "")
	r.finish(t) // nothing pushed: the check still fails on aaa111
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if r.handoffState(t) == "done" {
		t.Fatal("done on --fixed with the check failing")
	}
	if checks := r.checkItems(t); len(checks) != 1 || checks[0].Answer != "" {
		t.Fatalf("the check item %+v; want it open again", checks)
	}
}

func TestAReplyHandlesAFlakyCheck(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("aaa111", buildFails)
	r.finish(t)
	_, _ = r.submit(t, service.ReviewSubmission{Summary: "Good."})
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()

	r.answer(t, "k-7-build", service.AnswerReplied, "Flaky: the runner ran out of disk.")
	r.finish(t)
	if state := r.handoffState(t); state != "done" {
		t.Fatalf("handoff %s after a reply", state)
	}
	if block := r.worker.Reviews.ReviewBlock(context.Background(), r.card); block.ChecksReplied != 1 || !strings.Contains(block.Summary(), "1 check answered by reply") {
		t.Fatalf("the main is not shown the reply: %+v %q", block, block.Summary())
	}
	if comments := r.worker.ReviewGitHub.Comments(); len(comments) != 1 || !strings.Contains(comments[0], "runner ran out of disk") {
		t.Fatalf("the reply on GitHub: %v", comments)
	}
}

func TestPendingChecksHoldDone(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("aaa111", buildRuns)
	r.finish(t)
	_, _ = r.submit(t, service.ReviewSubmission{Summary: "Good."})
	if r.handoffState(t) == "done" {
		t.Fatal("done with a required check still running")
	}
	if block := r.worker.Reviews.ReviewBlock(context.Background(), r.card); block.ChecksPending != 1 || !block.InReview() || block.Summary() != "Approved · waiting for 1 check" {
		t.Fatalf("block %+v", block)
	}

	r.worker.ReviewGitHub.SetChecks("aaa111", buildPasses)
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()
	if state := r.handoffState(t); state != "done" {
		t.Fatalf("handoff %s once the check passed", state)
	}
}
