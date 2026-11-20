package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// setAuthor gives the card's author an activity and background jobs, as its
// hooks would.
func (r reviewed) setAuthor(t *testing.T, activity string, jobs store.BackgroundJobs) {
	t.Helper()
	ctx := context.Background()
	session, err := r.worker.Store.GetAgentSessionByTaskID(ctx, r.card)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetAgentHookState(ctx, session.ID, activity, jobs, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAnAuthorWithABackgroundJobIsReviewed(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.setAuthor(t, "background", store.BackgroundJobs{{Kind: "shell", ID: "b1", Label: "tail -f server.log"}})

	r.finish(t)

	if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 1 {
		t.Fatalf("a background job held the review: %+v", loop)
	}
}

func TestAWorkingOrWaitingAuthorHoldsThePassAndTheBlockSaysSo(t *testing.T) {
	for _, c := range []struct{ activity, waitingOn, summary string }{
		{"working", service.ReviewWaitingOnAuthorWorking, "Waiting for the author · working"},
		{"waiting", service.ReviewWaitingOnAuthorWaiting, "Waiting for the author · it waits for an answer"},
	} {
		t.Run(c.activity, func(t *testing.T) {
			r := newReviewed(t, ReviewPassOptions{})
			r.setAuthor(t, c.activity, nil)

			r.finish(t)

			if loop := r.loop(t); loop.Phase != service.ReviewPhaseReady || loop.Pass != 0 {
				t.Fatalf("a pass started: %+v", loop)
			}
			block := r.worker.Reviews.ReviewBlock(context.Background(), r.card)
			if block == nil || block.WaitingOn != c.waitingOn || block.Summary() != c.summary || strings.Contains(block.Summary(), "Reviewing") {
				t.Fatalf("block %+v, summary %q", block, block.Summary())
			}

			r.setAuthor(t, "", nil)
			r.tick()
			if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing {
				t.Fatalf("an idle author is not reviewed: %+v", loop)
			}
			if block := r.worker.Reviews.ReviewBlock(context.Background(), r.card); block.WaitingOn != "" || block.Summary() != "Reviewing · pass 1" {
				t.Fatalf("block %+v, summary %q", block, block.Summary())
			}
		})
	}
}

func TestAReadyLoopWaitingOnGitHubSaysSo(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.FailReads(1000) // GitHub is down

	r.finish(t)

	block := r.worker.Reviews.ReviewBlock(context.Background(), r.card)
	if r.loop(t).Phase != service.ReviewPhaseReady || block.WaitingOn != service.ReviewWaitingOnGitHub || block.Summary() != "Waiting for GitHub" {
		t.Fatalf("phase %q, block %+v, summary %q", r.loop(t).Phase, block, block.Summary())
	}
	r.worker.ReviewGitHub.FailReads(0)
	r.tick()
	if r.loop(t).Phase != service.ReviewPhaseReviewing {
		t.Fatalf("phase %q after GitHub answered", r.loop(t).Phase)
	}
}

func TestTheSummaryOfAReadyLoopIsNotReviewing(t *testing.T) {
	for _, c := range []struct {
		block service.ReviewLoopBlock
		want  string
	}{
		{service.ReviewLoopBlock{Phase: service.ReviewPhaseReviewing, Pass: 1, WaitingOn: service.ReviewWaitingOnAuthorWorking}, "Waiting for the author · working"},
		{service.ReviewLoopBlock{Phase: service.ReviewPhaseReviewing, Pass: 1, WaitingOn: service.ReviewWaitingOnAuthorPush}, "Waiting for the author to push"},
		{service.ReviewLoopBlock{Phase: service.ReviewPhaseReviewing, Pass: 2, WaitingOn: service.ReviewWaitingOnStart}, "Starting the next pass"},
		{service.ReviewLoopBlock{Phase: service.ReviewPhaseWaitingForReviewer, Pass: 1, WaitingOn: service.ReviewWaitingOnReviewerSlot}, "Waiting for a reviewer · pass 2"},
		{service.ReviewLoopBlock{Phase: service.ReviewPhaseReviewing, Pass: 1}, "Reviewing · pass 1"},
	} {
		if got := c.block.Summary(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.block, got, c.want)
		}
	}
}
