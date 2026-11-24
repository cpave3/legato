package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var blockingFinding = service.SubmittedFinding{PR: 12, Severity: "blocking", Title: "Greets nobody", Body: "An empty name prints a bare comma.",
	FailureScenario: "greet(\"\") prints \"Hello, \".", Place: &service.ReviewPlace{Path: "greet.go", Line: 4}}

// afterPassOne is a run whose pass 1 posted one blocking finding, waiting for
// the branch's owner.
func afterPassOne(t *testing.T, opts ReviewPassOptions) (reviewRun, *store.WorkflowRunView) {
	t.Helper()
	r := newReviewRun(t, opts)
	view := r.start(t, pr12)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	return r, view
}

func (r reviewRun) reviewer(t *testing.T, taskID string) bool {
	t.Helper()
	_, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), taskID, "reviewer")
	return err == nil
}

func (r reviewRun) loopPhase(t *testing.T, taskID string) store.HandoffReview {
	t.Helper()
	loop, err := r.worker.Store.GetHandoffReview(context.Background(), taskID)
	if err != nil || loop == nil {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
	return *loop
}

func TestAPushThatSettlesStartsTheNextPassOnWhatChanged(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	task := view.Run.TaskID
	findings, _ := r.worker.Store.ListHandoffReviewFindings(context.Background(), task)

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	if r.reviewer(t, task) {
		t.Fatal("a pass started at the push, before the quiet time")
	}
	r.worker.Clock.Advance(90 * time.Second)
	r.worker.ReviewGitHub.Push(12, "ddd444") // a second push restarts the wait
	r.worker.Checkout.Change("ddd444", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(90 * time.Second)
	r.tick()
	if r.reviewer(t, task) {
		t.Fatal("a pass started before the second push settled")
	}
	r.worker.Clock.Advance(time.Minute)
	r.tick()
	if !r.reviewer(t, task) || r.loopPhase(t, task).Pass != 2 {
		t.Fatalf("no pass 2: %+v", r.loopPhase(t, task))
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+task+"/2/12"] != "ddd444" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: findings[0].ID, Status: "resolved", Reason: "guarded"}}})
	if got := r.show(t, view.Run.ID); got.Run.Status != "succeeded" || outputs(t, got, "review")["passes"] != float64(2) {
		t.Fatalf("run %q", got.Run.Status)
	}
	if replies := r.worker.ReviewGitHub.Replies(); len(replies) != 1 || !strings.Contains(replies[0], "Resolved in pass 2") {
		t.Fatalf("replies = %v", replies)
	}
}

func TestAReplyInABlockingFindingsThreadStartsAPassWithNoPush(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	task := view.Run.TaskID
	r.worker.ReviewGitHub.SetAuthor(12, "octocat", "CONTRIBUTOR") // the branch's owner
	findings, _ := r.worker.Store.ListHandoffReviewFindings(context.Background(), task)
	root := findings[0].CommentID
	r.worker.ReviewGitHub.SetComments(12,
		service.PRComment{Kind: "review", ID: 1, Author: "legato-reviewer[bot]", Body: "<!-- legato-review-pass x -->"},
		service.PRComment{Kind: "inline", ID: root, ThreadRoot: root, Author: "legato-reviewer[bot]", Body: "Greets nobody"},
		service.PRComment{Kind: "inline", ID: root + 50, ThreadRoot: root, Author: "octocat", Association: "CONTRIBUTOR", Body: "An empty name never reaches greet: the form requires it."})
	r.tick()
	if !r.reviewer(t, task) || r.loopPhase(t, task).Pass != 2 {
		t.Fatalf("no pass 2 after the reply: %+v", r.loopPhase(t, task))
	}
	item, err := r.worker.Store.GetHandoffReviewItem(context.Background(), task, findings[0].ID)
	if err != nil || item.Answer != service.AnswerReplied || !strings.Contains(item.AnswerNote, "the form requires it") {
		t.Fatalf("item = %+v, %v", item, err)
	}
}

func TestRunAnotherPassStartsAPassOnlyWhileTheLoopWaitsForItsAuthor(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	if err := r.runs.Caller.AnotherPass(ctx, view.Run.ID, "review"); err == nil {
		t.Fatal("another pass while one runs")
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	if err := r.runs.Caller.AnotherPass(ctx, view.Run.ID, "review"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if !r.reviewer(t, task) || r.loopPhase(t, task).Pass != 2 {
		t.Fatalf("no pass 2: %+v", r.loopPhase(t, task))
	}
}

func TestAMergedPullRequestEndsTheRunClosed(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.MergePR(12)
	r.tick()
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "succeeded" || outputs(t, got, "review")["outcome"] != service.ReviewOutcomeClosed {
		t.Fatalf("run %q, outputs %+v", got.Run.Status, outputs(t, got, "review"))
	}
	if len(r.runs.Clones.removed) != 1 {
		t.Fatal("the clone was kept")
	}
}

func TestALoopAtItsMaximumPassesIsStuck(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{MaxPasses: 1})
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	loop := r.loopPhase(t, view.Run.TaskID)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "limit of 1 passes") || r.reviewer(t, view.Run.TaskID) {
		t.Fatalf("loop = %+v", loop)
	}
}
