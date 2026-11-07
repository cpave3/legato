package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func (r reviewed) away(t *testing.T) service.AwayTask { return mainAway(t, r.main, r.taskID) }

func TestTheMainSeesEachPhaseOfTheReview(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.main.StartConnections()

	r.finish(t)
	eventually(t, "in review", func() bool {
		a := r.away(t)
		return a.View == service.AwayViewInReview && a.Review != nil && a.Review.Phase == service.ReviewPhaseReviewing
	})
	if got := r.away(t).Review.Summary(); got != "Reviewing · pass 1" {
		t.Fatalf("summary %q", got)
	}

	if _, err := r.submit(t, service.ReviewSubmission{Summary: "One bug.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "No bound", Body: "x", Place: &service.ReviewPlace{Path: "login.go", Line: 3}}}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "author fixing", func() bool { return r.away(t).Review.Phase == service.ReviewPhaseWaitingForAuthor })
	a := r.away(t)
	if a.View != service.AwayViewInReview || a.Review.Summary() != "Author fixing · 1 blocking" || len(a.Review.Links) != 1 {
		t.Fatalf("away %+v, review %+v", a, a.Review)
	}

	// Done, fixed and pushed; then the second reviewer runs out of time.
	view, _ := service.ReviewFeedback(context.Background(), r.worker.Store, r.card)
	r.answer(t, view.Items[0].ID, service.AnswerFixed, "")
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.ReviewGitHub.OpenPR(7, r.branch, "bbb222")
	r.finish(t)
	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()
	eventually(t, "needs you", func() bool { return r.away(t).View == service.AwayViewNeedsYou })
	if got := r.away(t).Review; !strings.Contains(got.Reason, "ran past") {
		t.Fatalf("stuck review %+v", got)
	}
}

func TestReturnIsRefusedMidReviewAndAllowedWhenStuck(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.main.StartConnections()
	ctx := context.Background()
	r.finish(t)
	eventually(t, "in review", func() bool { return r.away(t).View == service.AwayViewInReview })

	_, err := r.main.Handoffs.Return(ctx, r.taskID)
	if !errors.Is(err, service.ErrReturnRefused) || !strings.Contains(err.Error(), "review pass 1 is running") {
		t.Fatalf("return mid-review: %v", err)
	}

	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()
	eventually(t, "stuck", func() bool { return r.away(t).Review.Stuck() })
	back, err := r.main.Handoffs.Return(ctx, r.taskID)
	if err != nil || back.Recalled {
		t.Fatalf("return when stuck: %+v, %v", back, err)
	}
	notes, _ := r.main.Store.ListTaskNotes(ctx, r.taskID)
	found := false
	for _, n := range notes {
		found = found || strings.Contains(n.Body, "Review passes: 1 pass(es)")
	}
	if !found {
		t.Fatalf("notes %+v; want the review summed up", notes)
	}
}

func TestRecallMidPassStopsEverySlotAndPostsNothingMore(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	r.finish(t)
	reviewer := r.reviewer(t)

	if _, err := r.main.Handoffs.Recall(ctx, r.taskID); err != nil {
		t.Fatal(err)
	}

	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, r.card, "reviewer"); err == nil {
		t.Fatal("the reviewer still runs after a recall")
	}
	if _, err := r.worker.Store.GetAgentSessionByTaskID(ctx, r.card); err == nil {
		t.Fatal("the author still runs after a recall")
	}
	// A late submission is not posted.
	_, _ = r.worker.Reviews.Submit(ctx, r.card, reviewer.ExecutionID, service.ReviewSubmission{Summary: "late"})
	r.tick()
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted after a recall: %+v", posted)
	}
}

func TestTakeBackNotesTheLastReviewHeard(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.main.StartConnections()
	ctx := context.Background()
	r.finish(t)
	eventually(t, "in review", func() bool { return r.away(t).View == service.AwayViewInReview })
	r.worker.Stop()
	r.main.Clock.Advance(time.Hour)
	eventually(t, "unreachable", func() bool { return r.away(t).View == service.AwayViewUnreachable })

	if _, err := r.main.Handoffs.TakeBack(ctx, r.taskID); err != nil {
		t.Fatal(err)
	}
	notes, _ := r.main.Store.ListTaskNotes(ctx, r.taskID)
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Body, "Last heard of its review: Review passes: 1 pass(es); Reviewing · pass 1") {
		t.Fatalf("notes %+v", notes)
	}
}

func TestDropStopsTheLoopAndUnpairLeavesTheAuthorRunning(t *testing.T) {
	ctx := context.Background()
	t.Run("drop", func(t *testing.T) {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		h, _ := r.worker.Store.OpenHandoffForTask(ctx, r.card)
		if _, err := r.worker.Handoffs.Drop(ctx, h.MainInstanceID, h.HandoffID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, r.card, "reviewer"); err == nil {
			t.Fatal("the reviewer still runs after a drop")
		}
		r.tick()
		if len(r.worker.ReviewGitHub.Posted()) != 0 {
			t.Fatal("posted after a drop")
		}
	})
	t.Run("unpair", func(t *testing.T) {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		h, _ := r.worker.Store.OpenHandoffForTask(ctx, r.card)
		if _, err := r.worker.Handoffs.UnpairMain(ctx, h.MainInstanceID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, r.card, "reviewer"); err == nil {
			t.Fatal("the reviewer still runs after unpairing")
		}
		if _, err := r.worker.Store.GetAgentSessionByTaskID(ctx, r.card); err != nil {
			t.Fatalf("the author stopped on unpairing: %v", err)
		}
	})
}
