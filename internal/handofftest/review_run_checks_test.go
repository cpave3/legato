package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

var failingGo = service.CheckResult{Name: "go", State: service.CheckFailing, Summary: "tests failed"}

func TestAnAgentAuthoredRunGetsAFailingCheckAndIsNotApprovedUntilItIsSettled(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	ctx := context.Background()
	view := r.start(t, agentAuthored)
	task := view.Run.TaskID
	r.tick()

	r.submit(t, task, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()

	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 || posted[0].Event != service.ReviewComment {
		t.Fatalf("posted %+v", posted)
	}
	if run := r.show(t, view.Run.ID); run.Run.Status != "running" {
		t.Fatalf("run %q with a failing check", run.Run.Status)
	}
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("loop %+v", loop)
	}
	feedback, err := service.ReviewFeedback(ctx, r.worker.Store, "board-1")
	if err != nil || len(feedback.Items) != 1 || feedback.Items[0].ID != "k-12-go" {
		t.Fatalf("feedback %+v, %v", feedback, err)
	}
	r.worker.Clock.Advance(3 * time.Minute) // new items wait for a quiet time
	r.tick()
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato review pass] 1 failing check on your pull requests") {
		t.Fatalf("typed to the author: %q", typed)
	}

	// The author answers that the check is flaky: the run ends approved,
	// and the pull request is approved.
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", "k-12-go", service.AnswerReplied, "A flaky test, tracked in #99.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 2 || posted[1].Event != service.ReviewApprove {
		t.Fatalf("posted %+v", posted)
	}
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || outputs(t, view, "review")["outcome"] != "approved" {
		t.Fatalf("run %q: %+v", view.Run.Status, outputs(t, view, "review"))
	}
}

func TestAPersonAuthoredRunGetsNoCheckItems(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	view := r.start(t, pr12)
	r.tick()

	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})

	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted %+v", posted)
	}
	if items, _ := r.worker.Store.ListHandoffReviewItems(context.Background(), view.Run.TaskID); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
	if run := r.show(t, view.Run.ID); run.Run.Status != "succeeded" {
		t.Fatalf("run %q", run.Run.Status)
	}
}
