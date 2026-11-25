package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The engine's operations (spec LEG-559 § Modules, LEG-582): Confirm before
// any caller reports approved (invariant 9), the operator actions by loop
// ID, and ActiveLoop.

func (r reviewed) loopID(t *testing.T) string {
	t.Helper()
	l, err := r.worker.Store.CurrentReviewLoop(context.Background(), r.card)
	if err != nil || l == nil {
		t.Fatalf("the card's loop = %+v, %v", l, err)
	}
	return l.ID
}

func (r reviewRun) loopID(t *testing.T, taskID string) string {
	t.Helper()
	l, err := r.worker.Store.CurrentReviewLoop(context.Background(), taskID)
	if err != nil || l == nil {
		t.Fatalf("the run's loop = %+v, %v", l, err)
	}
	return l.ID
}

func refusalCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := service.ReviewRefusalCode(err); got != code {
		t.Fatalf("err = %v (code %q), want code %q", err, got, code)
	}
}

// A handoff's clean pass whose heads GitHub cannot confirm right after it
// posts is not approved: the card stays out of Done, the handoff is not
// done, and the main is not told it approved. Once GitHub answers, it is.
func TestAHandoffIsNotApprovedUntilGitHubConfirmsTheCleanPassHeads(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	// The reads at submit, before posting, and before posting the layer
	// answer; the confirm's read after the post does not.
	r.worker.ReviewGitHub.FailReadsAfter(3, 1)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted %+v", posted)
	}
	if r.column(t) == "Done" || r.handoffState(t) == store.HandoffDone {
		t.Fatalf("approved before GitHub confirmed the heads: column %q, handoff %s", r.column(t), r.handoffState(t))
	}
	if block := r.worker.Reviews.ReviewBlock(context.Background(), r.card); block == nil || block.Phase == service.ReviewPhaseClean || strings.Contains(block.Summary(), "approved") {
		t.Fatalf("the main is told %+v (%q)", block, block.Summary())
	}

	r.tick()
	if r.column(t) != "Done" || r.handoffState(t) != store.HandoffDone {
		t.Fatalf("after GitHub answered: column %q, handoff %s", r.column(t), r.handoffState(t))
	}
	l, err := r.worker.Store.GetReviewLoop(context.Background(), r.loopID(t))
	if err != nil || l.ConfirmedAt == "" || l.CleanPass != 1 {
		t.Fatalf("the loop record %+v, %v; want it confirmed at pass 1", l, err)
	}
	if prs, _ := r.worker.Store.ListReviewLoopPRs(context.Background(), l.ID); len(prs) != 1 || prs[0].CleanHead != "aaa111" {
		t.Fatalf("layers %+v; want #7 confirmed clean at aaa111", prs)
	}
}

// A push that lands between a handoff's clean post and its confirm sends
// the loop to another pass, and the handoff is never done at the old head.
func TestAPushBeforeTheConfirmSendsAHandoffToAnotherPass(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	r.worker.ReviewGitHub.FailReadsAfter(3, 1)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	r.worker.Checkout.Commit(r.branch, "bbb222")
	r.worker.ReviewGitHub.Push(7, "bbb222")
	r.tick() // the confirm finds the push
	if state := r.handoffState(t); state == store.HandoffDone || r.column(t) == "Done" {
		t.Fatalf("done at a head GitHub no longer has: handoff %s, column %q", state, r.column(t))
	}
	r.tick()
	if loop := r.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("loop %+v; want pass 2 on the pushed head", loop)
	}
}

// An agent-authored run whose clean pass left a pull request at a COMMENT
// for a failing check is not approved while GitHub refuses the APPROVE that
// settles it: the run waits, and ends approved only once the APPROVE is
// posted.
func TestARunIsNotApprovedWhileItsSettledChecksApproveIsNotPosted(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	ctx := context.Background()
	view := r.start(t, agentAuthored)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", "k-12-go", service.AnswerReplied, "A flaky test.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}

	r.worker.ReviewGitHub.FailPosts(1000, false)
	for range 3 {
		r.tick()
	}
	if got := r.show(t, view.Run.ID); got.Run.Status != "running" {
		t.Fatalf("run %q with no APPROVE on GitHub: %+v", got.Run.Status, outputs(t, got, "review"))
	}
	for _, p := range r.worker.ReviewGitHub.Posted() {
		if p.Event == service.ReviewApprove {
			t.Fatalf("an APPROVE was posted: %+v", p)
		}
	}

	r.worker.ReviewGitHub.FailPosts(0, false)
	r.tick()
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 2 || posted[1].Event != service.ReviewApprove {
		t.Fatalf("posted %+v", posted)
	}
	if got := r.show(t, view.Run.ID); got.Run.Status != "succeeded" || outputs(t, got, "review")["outcome"] != "approved" {
		t.Fatalf("run %q: %+v", got.Run.Status, outputs(t, got, "review"))
	}
}

// Confirm, called on a loop, records it approved only at GitHub's heads, and
// says moved when a head is not the clean pass's pin.
func TestConfirmRecordsApprovedOnlyAtTheCleanPassPins(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	r.worker.ReviewGitHub.FailReadsAfter(3, 1)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := r.loopID(t)
	if got, err := r.worker.Reviews.Confirm(ctx, id); err != nil || got.State != service.ReviewConfirmApproved || got.Pass != 1 {
		t.Fatalf("confirm = %+v, %v", got, err)
	}
	if _, err := r.worker.Reviews.Confirm(ctx, "rl-000000000000"); service.ReviewRefusalCode(err) != service.ReviewRefusalLoopNotFound {
		t.Fatalf("an unknown loop: %v", err)
	}

	other := newReviewed(t, ReviewPassOptions{})
	other.finish(t)
	other.worker.ReviewGitHub.FailReadsAfter(3, 1)
	if _, err := other.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	other.worker.ReviewGitHub.Push(7, "bbb222")
	if got, err := other.worker.Reviews.Confirm(ctx, other.loopID(t)); err != nil || got.State != service.ReviewConfirmMoved {
		t.Fatalf("confirm after a push = %+v, %v", got, err)
	}
	if l, _ := other.worker.Store.GetReviewLoop(ctx, other.loopID(t)); l.ConfirmedAt != "" || l.Phase == service.ReviewPhaseClean {
		t.Fatalf("recorded approved at a moved head: %+v", l)
	}
}

// Another pass, Message the author and End act on a run's loop by its ID.
func TestTheOperatorActsOnARunsLoopByItsID(t *testing.T) {
	ctx := context.Background()
	r, view := stuckRun(t)
	id := r.loopID(t, view.Run.TaskID)
	if err := r.worker.Reviews.AnotherPassOn(ctx, id); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if !r.reviewer(t, view.Run.TaskID) {
		t.Fatal("another pass by loop ID started no reviewer")
	}

	r, view = stuckRun(t)
	id = r.loopID(t, view.Run.TaskID)
	if err := r.worker.Reviews.EndOn(ctx, id); err != nil {
		t.Fatal(err)
	}
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "succeeded" || outputs(t, got, "review")["outcome"] != service.ReviewOutcomeStuck {
		t.Fatalf("run %q, outputs %+v", got.Run.Status, outputs(t, got, "review"))
	}
	refusalCode(t, r.worker.Reviews.AnotherPassOn(ctx, id), service.ReviewRefusalLoopClosed)
	refusalCode(t, r.worker.Reviews.EndOn(ctx, id), service.ReviewRefusalLoopClosed)
	refusalCode(t, r.worker.Reviews.MessageAuthorOn(ctx, "rl-000000000000", "hello"), service.ReviewRefusalLoopNotFound)

	agent := newReviewRun(t, ReviewPassOptions{MaxPasses: 1})
	withBoardAuthor(t, agent)
	view, finding := agentPassOne(t, agent)
	if err := service.AnswerReviewItem(ctx, agent.worker.Store, "board-1", finding, service.AnswerReplied, "Intended.", agent.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	agent.tick()
	agent.tick()
	if err := agent.worker.Reviews.MessageAuthorOn(ctx, agent.loopID(t, view.Run.TaskID), "Guard the empty name."); err != nil {
		t.Fatal(err)
	}
	agent.worker.Settle()
	if typed := agent.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato review pass] From the operator: Guard the empty name.") {
		t.Fatalf("typed = %q", typed)
	}
}

// Another pass and Message the author act on a handoff's loop by its ID; End
// is refused, since a handoff's loop ends with its handoff.
func TestTheOperatorActsOnAHandoffsLoopByItsID(t *testing.T) {
	ctx := context.Background()
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseStuck {
		t.Fatalf("loop %+v", loop)
	}
	id := r.loopID(t)
	if err := r.worker.Reviews.MessageAuthorOn(ctx, id, "rebase on main"); err != nil {
		t.Fatal(err)
	}
	if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, "[legato main] rebase on main") {
		t.Fatalf("typed to the author: %q", typed)
	}
	if err := r.worker.Reviews.EndOn(ctx, id); err == nil {
		t.Fatal("End ended a handoff's loop")
	}
	if err := r.worker.Reviews.AnotherPassOn(ctx, id); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 {
		t.Fatalf("after another pass by loop ID: %+v", loop)
	}
	if err := r.worker.Reviews.AnotherPassOn(ctx, id); !errors.Is(err, service.ErrReviewNotStuck) {
		t.Fatalf("another pass on a loop that is not stuck: %v", err)
	}
}

// ActiveLoop is the loop that holds a pull request, nil once it closed.
func TestActiveLoopIsTheLoopHoldingAPullRequest(t *testing.T) {
	ctx := context.Background()
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	id := r.loopID(t, view.Run.TaskID)
	if l, err := r.worker.Reviews.ActiveLoop(ctx, "ACME/widget", 12); err != nil || l == nil || l.ID != id {
		t.Fatalf("active loop = %+v, %v; want %s", l, err, id)
	}
	if _, err := r.worker.Reviews.Close(ctx, id, service.ReviewOutcomeCancelled); err != nil {
		t.Fatal(err)
	}
	if l, err := r.worker.Reviews.ActiveLoop(ctx, "acme/widget", 12); err != nil || l != nil {
		t.Fatalf("active loop after close = %+v, %v", l, err)
	}
}

// Pass and Wait say where a loop stands, by its ID, and change nothing.
func TestPassAndWaitSayWhereALoopStands(t *testing.T) {
	ctx := context.Background()
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	id := r.loopID(t, view.Run.TaskID)
	if got, err := r.worker.Reviews.Pass(ctx, id); err != nil || got.State != service.ReviewPassPending || got.Pass != 1 {
		t.Fatalf("before pass 1: %+v, %v", got, err)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	if got, err := r.worker.Reviews.Pass(ctx, id); err != nil || got.State != service.ReviewPassStatePosted || got.Verdict != service.VerdictChanges || got.Pass != 1 {
		t.Fatalf("after pass 1: %+v, %v", got, err)
	}
	if got, err := r.worker.Reviews.Wait(ctx, id); err != nil || got.State != service.ReviewWaitPending {
		t.Fatalf("waiting for the author: %+v, %v", got, err)
	}
	r.worker.ReviewGitHub.MergePR(12)
	r.tick()
	if got, err := r.worker.Reviews.Wait(ctx, id); err != nil || got.State != service.ReviewWaitClosed {
		t.Fatalf("after the merge: %+v, %v", got, err)
	}
	refusalCode(t, func() error { _, err := r.worker.Reviews.Pass(ctx, id); return err }(), service.ReviewRefusalLoopClosed)
}
