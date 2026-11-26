package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// A worker runs the built-in review-loop for each make-a-change handoff with
// review passes (spec LEG-559 § Handoffs, D10, Q1; LEG-600): the handoff
// service starts it when it accepts the handoff, with the internal handoff
// binding no file, launcher or API can set.

// handoffLoop is the loop record the card hosts.
func (r reviewed) handoffLoop(t *testing.T) *store.ReviewLoop {
	t.Helper()
	l, err := r.worker.Store.CurrentReviewLoop(context.Background(), r.card)
	if err != nil || l == nil {
		t.Fatalf("the card's loop record = %+v, %v", l, err)
	}
	return l
}

// reviewRun is the review-loop run the handoff's loop belongs to.
func (r reviewed) reviewRun(t *testing.T) *store.WorkflowRunView {
	t.Helper()
	l := r.handoffLoop(t)
	view, err := r.worker.Store.GetWorkflowRun(context.Background(), l.RunID)
	if err != nil {
		t.Fatalf("the review run %q of the card's loop: %v", l.RunID, err)
	}
	return view
}

func TestAcceptingAReviewedHandoffStartsAReviewLoopRunThatCountsCommentsFromOpen(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()

	// Accepting it started the run, before any tick: owned by the run,
	// hosted by the card, bound to the handoff, with no steward.
	l := r.handoffLoop(t)
	h, err := r.worker.Store.OpenHandoffForTask(ctx, r.card)
	if err != nil {
		t.Fatal(err)
	}
	if l.OwnerKind != store.ReviewLoopOwnerRun || l.RunID == "" || l.HostTask != r.card || l.HandoffID != h.HandoffID ||
		l.MainInstanceID != h.MainInstanceID || l.Steward || l.Ready || !strings.Contains(l.HandoffColumns, `"done":"Done"`) {
		t.Fatalf("the card's loop = %+v", l)
	}
	run := r.reviewRun(t)
	if run.Run.WorkflowName != workflowengine.ReviewLoopWorkflowName || run.Run.ParentRunID != "" || run.Run.RelatedTaskID != r.card ||
		(run.Run.Status != "running" && run.Run.Status != "waiting") {
		t.Fatalf("the review run = %+v", run.Run)
	}

	// Comments count from open, before pass 1.
	r.worker.ReviewGitHub.SetComments(7, ownersReview)
	r.tick()
	if got := strings.Join(r.commentIDs(t), ","); got != "c-r-1" {
		t.Fatalf("counted comments %q before pass 1; want the owner's review", got)
	}
	if loop := r.loop(t); loop.Pass != 0 || loop.Phase != service.ReviewPhaseAuthor {
		t.Fatalf("loop %+v; want no pass before the card is in Done", loop)
	}
	if open := stepOf(t, r.reviewRun(t), "open"); open.Status != "succeeded" {
		t.Fatalf("the open step is %s", open.Status)
	}

	// The card in Done starts pass 1.
	r.answer(t, "c-r-1", service.AnswerFixed, "split it out")
	r.finish(t)
	if loop := r.loop(t); loop.Pass != 1 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("loop %+v; want pass 1 reviewing", loop)
	}
}

// Recall cancels the handoff's run: the loop closes, its claims go, the
// reviewer stops, and nothing posts after (AC 40).
func TestRecallCancelsAHandoffsReviewLoopRunAndNothingPostsAfter(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	r.finish(t)
	reviewer := r.reviewer(t)
	runID := r.handoffLoop(t).RunID

	if _, err := r.main.Handoffs.Recall(ctx, r.taskID); err != nil {
		t.Fatal(err)
	}
	r.tick()

	view, err := r.worker.Store.GetWorkflowRun(ctx, runID)
	if err != nil || view.Run.Status != "cancelled" {
		t.Fatalf("the review run %+v, %v; want it cancelled", view.Run, err)
	}
	l, err := r.worker.Store.GetReviewLoop(ctx, r.handoffLoop(t).ID)
	if err != nil || l.State != store.ReviewLoopClosedState {
		t.Fatalf("the loop %+v, %v; want it closed", l, err)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 7); held != nil {
		t.Fatalf("#7 is still held by %s", held.ID)
	}
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, r.card, "reviewer"); err == nil {
		t.Fatal("the reviewer still runs after the Recall")
	}
	// The reviewer's late submission posts nothing.
	_, _ = r.worker.Reviews.Submit(ctx, r.card, reviewer.ExecutionID, service.ReviewSubmission{Summary: "Good."})
	r.tick()
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted %+v after the Recall", posted)
	}
}

// A handoff's run ends at approved and lets go of its pull requests; the
// handoff is done, as before (AC 41).
func TestAHandoffsReviewLoopRunEndsAtApprovedAndReleasesItsClaim(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Solid fix."}); err != nil {
		t.Fatal(err)
	}
	r.tick()

	view := r.reviewRun(t)
	if view.Run.Status != "succeeded" {
		t.Fatalf("the review run is %s (%s); want succeeded", view.Run.Status, view.Run.Error)
	}
	if close := stepOf(t, view, "close"); !strings.Contains(close.OutputsJSON, `"outcome":"approved"`) {
		t.Fatalf("close outputs %s; want approved", close.OutputsJSON)
	}
	l := r.handoffLoop(t)
	if l.State != store.ReviewLoopClosedState || l.Outcome != service.ReviewOutcomeApproved {
		t.Fatalf("the loop %+v; want closed approved", l)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 7); held != nil {
		t.Fatalf("#7 is still held by %s", held.ID)
	}
	if r.column(t) != "Done" || r.handoffState(t) != store.HandoffDone {
		t.Fatalf("column %q, handoff %q", r.column(t), r.handoffState(t))
	}
	// Another tick changes nothing: the closed loop is history.
	r.tick()
	if again := r.handoffLoop(t); again.ID != l.ID || again.State != store.ReviewLoopClosedState {
		t.Fatalf("after another tick the card's loop is %+v", again)
	}
}

// Only the handoff service sets the binding, and never with a steward
// (handoff_input_refused).
func TestAHandoffsReviewLoopRunRefusesASteward(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	ctx := context.Background()
	runs := StartReviewRuns(t, r.worker)
	before, err := runs.Workflows.ListRuns(ctx, store.WorkflowRunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runs.Workflows.StartHandoffReviewLoop(ctx, service.HandoffReviewLoopStart{Repo: "acme/widget", Card: r.card, Steward: true,
		Bind: func(context.Context, string) error { return nil }})
	var refused *service.WorkflowStepError
	if !errors.As(err, &refused) || refused.Code != service.ReviewRefusalHandoffInput {
		t.Fatalf("a handoff run with a steward: %v; want handoff_input_refused", err)
	}
	after, _ := runs.Workflows.ListRuns(ctx, store.WorkflowRunFilter{})
	if len(after) != len(before) {
		t.Fatalf("%d runs after the refusal; want %d", len(after), len(before))
	}
}
