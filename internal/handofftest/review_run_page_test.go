package handofftest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The run page's review loop (spec LEG-559 stories 11–13, 20, 23; AC 20,
// 34, 36, 39; LEG-594): the details of a review-loop run's steps say the
// phase, passes, findings, each layer's state and the reviewer; while
// stewarding, each layer's mergeability, the last wake, re-entries, the
// idle deadline and held; the actions the waiting step takes; that
// legato-reviews' approval still stands; and that the author agent was left
// running.

// pageDetails is the loop as the run page shows it: the details of the
// step that waits, else of any review step.
func (r reviewRun) pageDetails(t *testing.T, runID string) service.ReviewRunDetails {
	t.Helper()
	view := r.show(t, runID)
	for i := len(view.Attempts) - 1; i >= 0; i-- {
		if view.Attempts[i].Status != service.WorkflowStepWaiting {
			continue
		}
		if got, ok := view.Details[view.Attempts[i].Path].(service.ReviewRunDetails); ok {
			return got
		}
	}
	for _, d := range view.Details {
		if got, ok := d.(service.ReviewRunDetails); ok {
			return got
		}
	}
	t.Fatalf("no review details: %+v (%s)", view.Details, stepStates(view))
	return service.ReviewRunDetails{}
}

func TestTheRunPageShowsTheStewardsLayersMergeabilityWakeBudgetDeadlineAndHeld(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetMergeable(12, "CONFLICTING", "DIRTY")
	r.watchTick()

	d := r.pageDetails(t, view.Run.ID)
	if d.LoopID == "" || d.Pass != 1 || d.Findings == nil || d.Findings.Total != 0 {
		t.Fatalf("details %+v", d)
	}
	if len(d.Layers) != 1 || d.Layers[0].Number != 12 || d.Layers[0].State != "open" || d.Layers[0].Mergeable != "CONFLICTING" || d.Layers[0].URL != "https://github.com/acme/widget/pull/12" {
		t.Fatalf("layers %+v", d.Layers)
	}
	s := d.Steward
	if s == nil || s.Held || s.Reentries != 0 || s.MaxReentries != 5 || s.IdleDeadline == "" {
		t.Fatalf("steward %+v", s)
	}
	if !slices.Equal(d.Actions, []string{"stop_watching", "end"}) {
		t.Fatalf("actions %v", d.Actions)
	}
	if d.ApprovalStands != "" || d.Summary != "Stewarding" {
		t.Fatalf("watching says %q, approval %q", d.Summary, d.ApprovalStands)
	}

	// GitHub's BEHIND, from the merge state, is the layer's mergeability.
	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "BEHIND")
	r.watchTick()
	if d := r.pageDetails(t, view.Run.ID); d.Layers[0].Mergeable != "BEHIND" {
		t.Fatalf("layers %+v", d.Layers)
	}

	// Held: Watch again and End, and the approval still stands.
	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}
	d = r.pageDetails(t, view.Run.ID)
	if d.Steward == nil || !d.Steward.Held || d.Summary != "Held" {
		t.Fatalf("held: %q, steward %+v", d.Summary, d.Steward)
	}
	if !slices.Equal(d.Actions, []string{"watch_again", "end"}) {
		t.Fatalf("held actions %v", d.Actions)
	}
	if d.ApprovalStands != "legato-reviews' approval at bbb222 still stands" {
		t.Fatalf("approval %q", d.ApprovalStands)
	}

	// Watch again, then a push: the wake and one re-entry of five.
	if err := r.stewardAct(t, view, "watch_again"); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	d = r.pageDetails(t, view.Run.ID)
	if d.Steward == nil || d.Steward.LastWake != "pushed" || d.Steward.LastWakeAt == "" || d.Steward.Reentries != 1 || d.Steward.MaxReentries != 5 {
		t.Fatalf("after a push: steward %+v", d.Steward)
	}
	if !slices.Equal(d.Actions, []string{"end"}) || d.ApprovalStands != "" {
		t.Fatalf("in a re-entry's review: actions %v, approval %q", d.Actions, d.ApprovalStands)
	}
}

// The watch keeps the record's layers as GitHub last reported them, so a
// process without the engine shows them too.
func TestTheWatchWritesEachLayersStateAndMergeabilityToTheRecord(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetMergeable(12, "CONFLICTING", "DIRTY")
	r.watchTick()
	layers, err := r.worker.Store.ListReviewLoopPRs(context.Background(), r.record(t, view).ID)
	if err != nil || len(layers) != 1 || layers[0].State != "open" || layers[0].Mergeable != "CONFLICTING" {
		t.Fatalf("layers %+v, %v", layers, err)
	}
}

func TestAStuckWaitOffersAnotherPassMessageTheAuthorAndEnd(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{MaxPasses: 1})
	withBoardAuthor(t, r)
	view, finding := agentPassOne(t, r)
	if err := service.AnswerReviewItem(context.Background(), r.worker.Store, "board-1", finding, service.AnswerFixed, "", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.authorActivity(t, "idle")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()

	d := r.pageDetails(t, view.Run.ID)
	if d.Phase != service.ReviewPhaseStuck || !slices.Equal(d.Actions, []string{"another_pass", "message", "end"}) {
		t.Fatalf("stuck: phase %q, actions %v", d.Phase, d.Actions)
	}
	if d.Findings == nil || d.Findings.Total != 1 || d.Findings.Blocking != 1 {
		t.Fatalf("findings %+v", d.Findings)
	}
	if d.Reviewer == "" {
		t.Fatalf("no reviewer: %+v", d)
	}
	if d.ApprovalStands != "" {
		t.Fatalf("never approved, yet %q", d.ApprovalStands)
	}
	if d.AuthorLeftRunning {
		t.Fatal("left running while the loop waits")
	}

	// End: the loop ends stuck, and its author agent was left running.
	if _, err := r.runs.Workflows.StepAction(context.Background(), view.Run.ID, waitingPath(t, r.show(t, view.Run.ID)), "end", ""); err != nil {
		t.Fatal(err)
	}
	r.tick()
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", got.Run.Status, stepStates(got))
	}
	d = r.pageDetails(t, view.Run.ID)
	if !d.AuthorLeftRunning || d.AuthorTask != "board-1" || d.Outcome != service.ReviewOutcomeStuck || len(d.Actions) != 0 {
		t.Fatalf("after End: %+v", d)
	}
}

func TestAStewardStuckAtItsBudgetSaysTheApprovalStandsAndOffersWatchAgain(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	inputs := map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "steward": "true", "max_reentries": "1"}
	view := r.stewarded(t, inputs)
	r.reenterClean(t, view.Run.TaskID, "re1")
	r.worker.ReviewGitHub.Push(12, "re2")
	r.worker.Checkout.Change("re2", "greet.go", 3, 5)
	r.watchTick()
	r.tick()

	d := r.pageDetails(t, view.Run.ID)
	if d.Phase != service.ReviewPhaseStuck || d.Steward == nil || d.Steward.Reentries != 1 || d.Steward.MaxReentries != 1 {
		t.Fatalf("stuck steward: phase %q, steward %+v", d.Phase, d.Steward)
	}
	if !slices.Equal(d.Actions, []string{"watch_again", "message", "end"}) {
		t.Fatalf("actions %v", d.Actions)
	}
	if d.ApprovalStands != "legato-reviews' approval at re1 still stands" {
		t.Fatalf("approval %q", d.ApprovalStands)
	}
}

func TestAnEndedStewardSaysItsAuthorWasLeftRunningAndAMergedOneDoesNot(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	if err := r.stewardAct(t, view, "end"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if d := r.pageDetails(t, view.Run.ID); !d.AuthorLeftRunning || d.Outcome != service.ReviewOutcomeEnded {
		t.Fatalf("ended: %+v", d)
	}

	// Stopped: no longer left running.
	if err := r.worker.Agents.KillAgent(context.Background(), "board-1"); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if d := r.pageDetails(t, view.Run.ID); d.AuthorLeftRunning {
		t.Fatalf("stopped, yet %+v", d)
	}

	merged := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, merged)
	mv := merged.stewarded(t, stewardInputs)
	merged.worker.ReviewGitHub.MergePR(12)
	merged.watchTick()
	merged.tick()
	if d := merged.pageDetails(t, mv.Run.ID); d.AuthorLeftRunning || d.Outcome != service.ReviewOutcomeMerged {
		t.Fatalf("merged: %+v", d)
	}
}

// The board's PR chip: stewarding, or held, from the claim.
func TestTheLookupSaysAPullRequestIsStewardedOrHeld(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	lookup := service.NewReviewLoopLookup(r.worker.Store)
	ctx := context.Background()
	if got := lookup.StewardState(ctx, "acme/widget", 12); got != "" {
		t.Fatalf("before any loop: %q", got)
	}
	view := r.stewarded(t, stewardInputs)
	if got := lookup.StewardState(ctx, "acme/widget", 12); got != service.StewardStateStewarding {
		t.Fatalf("watching: %q", got)
	}
	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}
	if got := lookup.StewardState(ctx, "Acme/Widget", 12); got != service.StewardStateHeld {
		t.Fatalf("held: %q", got)
	}
	if got := lookup.StewardState(ctx, "acme/widget", 13); got != "" {
		t.Fatalf("another pull request: %q", got)
	}
}

// waitingPath is the path of the step that waits.
func waitingPath(t *testing.T, view *store.WorkflowRunView) string {
	t.Helper()
	for i := len(view.Attempts) - 1; i >= 0; i-- {
		if view.Attempts[i].Status == service.WorkflowStepWaiting {
			return view.Attempts[i].Path
		}
	}
	t.Fatalf("nothing waits: %s", stepStates(view))
	return ""
}

