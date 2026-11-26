package handofftest

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// Adoption at upgrade (spec LEG-559 § Handoffs, M4; LEG-601): a handoff's
// loop that its handoff owned, as every handoff loop opened before
// review-loop ran handoffs (LEG-600) did, is adopted by a review-loop run
// when the engine holder starts. In one transaction the owner changes from
// the handoff to the run; the claim, the passes, the rulings and the items
// stay, and a pass in flight is the run's pass step's.

// asHandoffOwned makes the card's loop one its handoff owns, as a worker
// before LEG-600 kept it: no review-loop run, the owner the handoff, no
// binding columns. The worker is restarted on it, as an upgrade does.
func (r reviewed) asHandoffOwned(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	l := r.handoffLoop(t)
	db := r.worker.Store.DB()
	if _, err := db.ExecContext(ctx, `DELETE FROM workflow_runs WHERE id = ?`, l.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE review_loops SET owner_kind = 'handoff', run_id = '', step_path = '', attempt_id = '',
		handoff_columns = '' WHERE id = ?`, l.ID); err != nil {
		t.Fatal(err)
	}
	r.worker.Restart()
}

// AC 43: an upgrade adopts a handoff loop mid-pass. The pass in flight is
// posted once, by the run that adopted it, and pass 1's ruling and the
// author's answer are kept.
func TestAnUpgradeAdoptsAHandoffLoopMidPassAndPostsThePassOnce(t *testing.T) {
	ctx := context.Background()
	h := newReviewed(t, ReviewPassOptions{})
	h.finish(t)
	result, err := h.submit(t, service.ReviewSubmission{Summary: "One bug.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "Retries never stop", Body: "The loop has no bound.", FailureScenario: "A login while the server is down never returns.", Place: &service.ReviewPlace{Path: "login.go", Line: 20}},
	}})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	h.tick() // feedback goes to the author
	finding := result.Findings[0]
	h.answer(t, finding, service.AnswerFixed, "bounded the retries")
	h.worker.Checkout.Commit(h.branch, "bbb222")
	h.worker.ReviewGitHub.OpenPR(7, h.branch, "bbb222")
	h.tick()
	h.finish(t)
	if loop := h.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("the loop before the upgrade = %+v; want pass 2 reviewing", loop)
	}
	before := h.handoffLoop(t)
	execution := h.reviewer(t).ExecutionID

	h.asHandoffOwned(t)
	if l := h.handoffLoop(t); l.OwnerKind != store.ReviewLoopOwnerHandoff || l.RunID != "" {
		t.Fatalf("the loop as an older worker kept it = %+v", l)
	}

	// The holder's first tick adopts it: the same loop, now a review-loop
	// run's, still bound to the handoff and holding #7.
	h.tick()
	adopted := h.handoffLoop(t)
	if adopted.ID != before.ID || adopted.OwnerKind != store.ReviewLoopOwnerRun || adopted.RunID == "" || adopted.RunID == before.RunID ||
		adopted.HandoffID != before.HandoffID || adopted.MainInstanceID != before.MainInstanceID || adopted.Pass != 2 ||
		adopted.MarkerKey != before.MarkerKey || adopted.HandoffColumns == "" {
		t.Fatalf("the adopted loop = %+v; was %+v", adopted, before)
	}
	run := h.reviewRun(t)
	if run.Run.WorkflowName != workflowengine.ReviewLoopWorkflowName || run.Run.RelatedTaskID != h.card || run.Run.ParentRunID != "" {
		t.Fatalf("the adopting run = %+v", run.Run)
	}
	if held, _ := h.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 7); held == nil || held.ID != before.ID {
		t.Fatalf("#7 is held by %+v; want the adopted loop", held)
	}
	if loop := h.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("the adopted loop's pass = %+v; want pass 2 still reviewing", loop)
	}
	if got := h.reviewer(t).ExecutionID; got != execution {
		t.Fatalf("the reviewer is %s; want the pass in flight's, %s", got, execution)
	}
	// The next tick's open step takes the loop as its own, and adopts
	// nothing more: one run.
	h.tick()
	if again := h.handoffLoop(t); again.RunID != adopted.RunID || again.AttemptID == "" {
		t.Fatalf("after a second tick the loop is %+v; want run %s's open step's", again, adopted.RunID)
	}
	if open := stepOf(t, h.reviewRun(t), "open"); open.Status != "succeeded" {
		t.Fatalf("the open step is %s", open.Status)
	}
	if runs, err := h.worker.Store.ListWorkflowRunsForTask(ctx, h.card); err != nil || len(runs) != 1 {
		t.Fatalf("the card's runs = %+v, %v; want the adopting one alone", runs, err)
	}

	// The reviewer of the pass in flight submits: it posts once, and the
	// run reads it, confirms and ends approved.
	if _, err := h.submit(t, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the loop is bounded"}}}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	h.tick()
	posted := h.worker.ReviewGitHub.Posted()
	want := []string{before.MarkerKey + "/pass-1/pr-7", before.MarkerKey + "/pass-2/pr-7"}
	if len(posted) != len(want) {
		t.Fatalf("posted %+v; want %v", posted, want)
	}
	for i, p := range posted {
		if p.Marker != want[i] {
			t.Fatalf("post %d is %s; want %v", i, p.Marker, want)
		}
	}
	if posted[1].Event != service.ReviewApprove {
		t.Fatalf("pass 2 posted %s; want an approval", posted[1].Event)
	}
	if view := h.reviewRun(t); view.Run.Status != "succeeded" {
		t.Fatalf("the adopting run is %s (%s); want succeeded", view.Run.Status, view.Run.Error)
	}
	if l := h.handoffLoop(t); l.State != store.ReviewLoopClosedState || l.Outcome != service.ReviewOutcomeApproved {
		t.Fatalf("the loop = %+v; want it closed approved", l)
	}
	if h.handoffState(t) != store.HandoffDone {
		t.Fatalf("handoff %s; want done after the clean pass", h.handoffState(t))
	}

	// The ruling of pass 1's finding and the author's answer are kept.
	findings, err := h.worker.Store.ListHandoffReviewFindings(ctx, h.card)
	if err != nil || len(findings) != 1 || findings[0].ID != finding || findings[0].Status != "resolved" || findings[0].RuledPass != 2 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	items, err := h.worker.Store.ListHandoffReviewItems(ctx, h.card)
	if err != nil {
		t.Fatal(err)
	}
	answered := false
	for _, item := range items {
		answered = answered || (item.ID == finding && item.Answer == service.AnswerFixed && item.AnswerNote == "bounded the retries")
	}
	if !answered {
		t.Fatalf("items = %+v; want the author's answer to %s kept", items, finding)
	}
}

// A handoff's loop opened before LEG-600 whose handoff has closed by the
// upgrade is not adopted: it closes and lets go of its pull requests.
func TestAHandoffOwnedLoopOfAClosedHandoffIsClosedNotAdopted(t *testing.T) {
	ctx := context.Background()
	h := newReviewed(t, ReviewPassOptions{})
	h.finish(t)
	before := h.handoffLoop(t)
	h.asHandoffOwned(t)
	if err := h.worker.Store.CloseHandoffAndArchive(ctx, before.MainInstanceID, before.HandoffID, h.worker.Clock.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	h.tick()
	l, err := h.worker.Store.GetReviewLoop(ctx, before.ID)
	if err != nil || l.State != store.ReviewLoopClosedState || l.OwnerKind != store.ReviewLoopOwnerHandoff {
		t.Fatalf("the loop of the closed handoff = %+v, %v; want it closed, never adopted", l, err)
	}
	if held, _ := h.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 7); held != nil {
		t.Fatalf("#7 is still held by %s", held.ID)
	}
	if runs, err := h.worker.Store.ListWorkflowRunsForTask(ctx, h.card); err != nil || len(runs) != 0 {
		t.Fatalf("runs %+v, %v were started for the closed handoff's loop", runs, err)
	}
}
