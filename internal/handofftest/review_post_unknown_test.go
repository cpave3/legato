package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// LEG-561: a post whose outcome is unknown (GitHub answered with a 5xx, or
// not at all) leaves the pass submitted. The next tick looks for its marker
// first and records what GitHub took, threads included, or posts it again
// with the same marker. After ten tries or an hour, the loop needs the
// operator, and no second pass starts.

func TestAReviewGitHubTookBeforeA5xxIsRecordedWithItsThreadOnTheNextTry(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.worker.ReviewGitHub.FailPosts(1, true)

	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	if pass, _ := r.worker.Store.GetHandoffReviewPass(context.Background(), task, 1); pass.State != service.ReviewPassSubmitted {
		t.Fatalf("pass after the 502 = %+v", pass)
	}

	r.tick()

	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 {
		t.Fatalf("GitHub has %d reviews", len(posted))
	}
	findings, _ := r.worker.Store.ListHandoffReviewFindings(context.Background(), task)
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor || len(findings) != 1 || findings[0].CommentID == 0 {
		t.Fatalf("loop %+v, findings %+v", loop, findings)
	}

	// The thread is the one pass 2's ruling replies in.
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if r.loopPhase(t, task).Pass != 2 {
		t.Fatalf("no pass 2: %+v", r.loopPhase(t, task))
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: findings[0].ID, Status: "resolved", Reason: "guarded"}}})
	replies := r.worker.ReviewGitHub.Replies()
	if len(replies) != 1 || !strings.HasPrefix(replies[0], fmt.Sprintf("reviewer %d: Resolved in pass 2", findings[0].CommentID)) {
		t.Fatalf("replies = %v", replies)
	}
}

func TestAReviewGitHubDidNotTakeIsPostedOnceWithTheSameMarker(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.worker.ReviewGitHub.FailPosts(1, false)

	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted = %+v", posted)
	}
	r.tick()
	r.tick()

	posted, tried := r.worker.ReviewGitHub.Posted(), r.worker.ReviewGitHub.Tried()
	if len(posted) != 1 || len(tried) != 2 || tried[0].Marker != tried[1].Marker || posted[0].Marker != tried[0].Marker {
		t.Fatalf("posted %+v, tried %+v", posted, tried)
	}
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseWaitingForAuthor || loop.Pass != 1 {
		t.Fatalf("loop = %+v", loop)
	}
}

// unknownTries submits pass 1 while every post fails with a 502, so its
// outcome is never known: the submit's try, then one per tick.
func unknownTries(t *testing.T) (reviewRun, string) {
	t.Helper()
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	r.tick()
	r.worker.ReviewGitHub.FailPosts(1000, false)
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	return r, view.Run.TaskID
}

func TestAfterTenUnknownTriesTheLoopNeedsYouOnceAndNoSecondPassRuns(t *testing.T) {
	r, task := unknownTries(t)
	for i := 0; i < 8; i++ {
		r.tick()
	}
	if items := r.attention(t); len(items) != 0 || len(r.worker.ReviewGitHub.Tried()) != 9 {
		t.Fatalf("after 9 tries: items %+v, tried %d", items, len(r.worker.ReviewGitHub.Tried()))
	}

	r.tick() // the tenth
	for i := 0; i < 5; i++ {
		r.tick()
	}

	items := r.attention(t)
	if len(items) != 1 || items[0].SignalKind != service.AttentionSignalWorkflowReviewStuck || !strings.Contains(items[0].Reason, "acme/widget#12") {
		t.Fatalf("items = %+v", items)
	}
	loop := r.loopPhase(t, task)
	if loop.Phase != service.ReviewPhaseStuck || loop.Pass != 1 || len(r.worker.ReviewGitHub.Tried()) != 10 || r.reviewer(t, task) {
		t.Fatalf("loop %+v, tried %d, reviewer %v", loop, len(r.worker.ReviewGitHub.Tried()), r.reviewer(t, task))
	}

	// Another pass posts the same pass again, with its marker: never a
	// second pass.
	r.worker.ReviewGitHub.FailPosts(0, false)
	adapter := service.NewWorkflowAttentionAdapter(r.runs.Workflows)
	if err := adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "another_pass"}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	posted, tried := r.worker.ReviewGitHub.Posted(), r.worker.ReviewGitHub.Tried()
	if len(posted) != 1 || posted[0].Marker != tried[0].Marker || r.loopPhase(t, task).Pass != 1 || r.reviewer(t, task) {
		t.Fatalf("posted %+v, loop %+v", posted, r.loopPhase(t, task))
	}
	// The loop goes on: it waits for the branch's owner, a person.
	if phase := r.loopPhase(t, task).Phase; phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("phase %q", phase)
	}
	for _, item := range r.attention(t) {
		if item.SignalKind == service.AttentionSignalWorkflowReviewStuck {
			t.Fatalf("still stuck: %+v", item)
		}
	}
}

func TestAnHourOfUnknownTriesNeedsYou(t *testing.T) {
	r, task := unknownTries(t)
	r.tick()
	r.worker.Clock.Advance(59 * time.Minute)
	r.tick()
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("before the hour: %+v", items)
	}
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()

	if items := r.attention(t); len(items) != 1 || r.loopPhase(t, task).Phase != service.ReviewPhaseStuck || len(r.worker.ReviewGitHub.Tried()) != 4 {
		t.Fatalf("items %+v, loop %+v, tried %d", items, r.loopPhase(t, task), len(r.worker.ReviewGitHub.Tried()))
	}
}
