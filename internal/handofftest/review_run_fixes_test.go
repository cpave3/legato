package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A push while the reviewer worked: a clean pass of the old head does not
// end the loop as approved; the next pass reviews the new head.
func TestACleanPassOfAHeadThatMovedReviewsTheNewHead(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	r.tick()
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Clean."})
	if got := r.show(t, view.Run.ID); got.Run.Status != "running" {
		t.Fatalf("run %q after a clean pass of an old head", got.Run.Status)
	}
	r.tick()
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Pass != 2 || r.worker.Checkout.Pins()["refs/legato/review/"+view.Run.TaskID+"/2/12"] != "ccc333" {
		t.Fatalf("loop = %+v, pins %v", loop, r.worker.Checkout.Pins())
	}
}

// A run started on a handoff's pull request before the handoff's first pass:
// when the handoff's author finishes, its pass refuses to start on it.
func TestAHandoffPassDoesNotStartOnAPullRequestARunReviews(t *testing.T) {
	h := newReviewed(t, ReviewPassOptions{})
	runs := StartReviewRuns(t, h.worker)
	view, err := runs.Start(context.Background(), map[string]string{"repo": "acme/widget", "pr_number": "7"})
	if err != nil || view.Run.Status != "running" {
		t.Fatalf("run = %+v, %v", view, err)
	}
	h.finish(t)
	loop := h.loop(t)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "run "+view.Run.ID) {
		t.Fatalf("handoff loop = %+v", loop)
	}
	if _, err := h.worker.Store.GetAgentSessionInSlot(context.Background(), h.card, "reviewer"); err == nil {
		t.Fatal("a second reviewer started on the pull request")
	}
}

// A board task's tracked pull request, neither its summary PR nor its
// checked-out branch, links the task.
func TestATrackedPullRequestLinksItsBoardTask(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "board-2", Title: "Stack", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetCodeContext(ctx, "board-2", &store.CodeContext{Repo: "acme/widget", Branch: "stack/top", RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.worker.Store.TrackTaskBranches(ctx, "board-2", []store.BranchRef{{Repo: "acme/widget", Branch: "feature/greeting"}}, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetTaskBranchPR(ctx, "board-2", "acme/widget", "feature/greeting", &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	view := r.start(t, pr12)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Fine."})
	if got := outputs(t, r.show(t, view.Run.ID), "review")["linked_task"]; got != "board-2" {
		t.Fatalf("linked_task = %v", got)
	}
}

// A findings-only run reads no checks, at pass start either.
func TestAFindingsOnlyRunRaisesNoCheckItems(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetChecks("bbb222", service.CheckResult{Name: "build", State: service.CheckFailing, Conclusion: "failure"})
	view := r.start(t, pr12)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	items, _ := r.worker.Store.ListHandoffReviewItems(context.Background(), view.Run.TaskID)
	for _, item := range items {
		if item.Kind == service.ItemCheck {
			t.Fatalf("a check item: %+v", item)
		}
	}
}

// A pull request closed while its reviewer works ends the loop closed, and
// the reviewer it stopped is not taken for a failed pass.
func TestClosingAPullRequestDuringAPassEndsTheLoopClosed(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	r.tick()
	r.worker.ReviewGitHub.ClosePR(12)
	r.tick()
	r.worker.Clock.Advance(time.Second)
	r.tick()
	loop := r.loopPhase(t, view.Run.TaskID)
	if loop.Phase != service.ReviewPhaseEnded || loop.Reason != service.ReviewOutcomeClosed {
		t.Fatalf("loop = %+v", loop)
	}
	pass, _ := r.worker.Store.GetHandoffReviewPass(context.Background(), view.Run.TaskID, 1)
	if pass != nil && pass.State == service.ReviewPassUnable {
		t.Fatalf("pass = %+v", pass)
	}
}

// A board task's agent authors one loop at a time: a second is refused,
// naming the first.
func TestATasksAgentAuthorsOneLoopAtATime(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	if _, err := r.worker.Store.TrackTaskBranches(ctx, "board-1", []store.BranchRef{{Repo: "acme/widget", Branch: "feature/more"}}, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetTaskBranchPR(ctx, "board-1", "acme/widget", "feature/more", &store.PRMeta{Repo: "acme/widget", Branch: "feature/more", PRNumber: 13, State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.OpenPR(13, "feature/more", "eee555")
	first := r.start(t, agentAuthored)
	second := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "13", "agent_author": "true"})
	refused(t, second, "board-1's agent already authors the review loop of run "+first.Run.ID)
}
