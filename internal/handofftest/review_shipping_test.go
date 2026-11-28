package handofftest

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// The Shipping page's list (LEG-706): GET /api/shipping lists every review
// loop, whichever caller opened it, with where it stands as the run page
// says it, its layers' CI and mergeability, the steward's last wake, the
// author agent and the reviewer, and the actions its waiting step takes.

// shipping is GET /api/shipping on the worker's web server.
func (r reviewRun) shipping(t *testing.T) service.ShippingView {
	t.Helper()
	resp := r.worker.Do(http.MethodGet, "/api/shipping", r.worker.WebToken(), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/shipping: %d", resp.StatusCode)
	}
	var view service.ShippingView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

// shippingLoop is the one loop the list holds.
func oneShippingLoop(t *testing.T, view service.ShippingView) service.ShippingLoop {
	t.Helper()
	if len(view.Loops) != 1 {
		t.Fatalf("loops %+v", view.Loops)
	}
	return view.Loops[0]
}

func TestShippingListsAStewardedLoopWithItsLayerAuthorReviewerAndActions(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	got := oneShippingLoop(t, r.shipping(t))
	if got.LoopID != r.record(t, view).ID || got.Repo != "acme/widget" || !got.Open {
		t.Fatalf("loop %+v", got)
	}
	if got.Phase != service.ShippingWatching || got.NeedsYou {
		t.Fatalf("phase %q, needs you %v", got.Phase, got.NeedsYou)
	}
	if got.Caller != "review-loop" || got.RunID != view.Run.ID || got.Link != "/workflows/runs/"+view.Run.ID {
		t.Fatalf("caller %q, run %q, link %q", got.Caller, got.RunID, got.Link)
	}
	if got.Task == nil || got.Task.ID != "board-1" || got.Task.Title != "Greeting" {
		t.Fatalf("task %+v", got.Task)
	}
	if got.Author == nil || got.Author.Task != "board-1" || got.Author.State == "" {
		t.Fatalf("author %+v", got.Author)
	}
	if got.Reviewer == "" {
		t.Fatalf("no reviewer: %+v", got)
	}
	if len(got.Layers) != 1 || got.Layers[0].Number != 12 || got.Layers[0].URL != "https://github.com/acme/widget/pull/12" {
		t.Fatalf("layers %+v", got.Layers)
	}
	if got.Steward == nil || got.Steward.Held {
		t.Fatalf("steward %+v", got.Steward)
	}
	if !slices.Equal(got.Actions, []string{"stop_watching", "end"}) || got.Step == "" {
		t.Fatalf("actions %v on step %q", got.Actions, got.Step)
	}

	// The row's step takes its actions: Stop watching holds the steward.
	if _, err := r.runs.Workflows.StepAction(context.Background(), got.RunID, got.Step, "stop_watching", ""); err != nil {
		t.Fatalf("stop watching on %s: %v", got.Step, err)
	}
	held := oneShippingLoop(t, r.shipping(t))
	if held.Phase != service.ShippingHeld || !held.NeedsYou || !slices.Equal(held.Actions, []string{"watch_again", "end"}) {
		t.Fatalf("held: phase %q, needs you %v, actions %v", held.Phase, held.NeedsYou, held.Actions)
	}
}

func TestShippingFollowsALoopFromReviewingToChangesRequestedToStuck(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{MaxPasses: 1})
	withBoardAuthor(t, r)
	view := r.startLoop(t, agentAuthored)
	r.tick()

	reviewing := oneShippingLoop(t, r.shipping(t))
	if reviewing.Phase != service.ShippingReviewing || reviewing.Pass != 1 || reviewing.NeedsYou || len(reviewing.Actions) != 0 {
		t.Fatalf("reviewing: %+v", reviewing)
	}

	result := r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	changes := oneShippingLoop(t, r.shipping(t))
	if changes.Phase != service.ShippingChangesRequested || changes.Findings == nil || changes.Findings.Blocking != 1 {
		t.Fatalf("changes requested: %+v", changes)
	}
	if !slices.Equal(changes.Actions, []string{"another_pass", "message"}) || changes.Step == "" {
		t.Fatalf("changes requested: actions %v on %q", changes.Actions, changes.Step)
	}

	// The author fixes it and goes quiet; one pass is all it had.
	if err := service.AnswerReviewItem(context.Background(), r.worker.Store, "board-1", result.Findings[0], service.AnswerFixed, "", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.authorActivity(t, "idle")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()
	stuck := oneShippingLoop(t, r.shipping(t))
	if stuck.Phase != service.ShippingStuck || !stuck.NeedsYou || !slices.Contains(stuck.Actions, "end") {
		t.Fatalf("stuck: %+v", stuck)
	}
	if stuck.Author == nil || stuck.Author.State != "idle" {
		t.Fatalf("author %+v", stuck.Author)
	}

	// End: the loop is listed as ended, with its outcome and no actions.
	if _, err := r.runs.Workflows.StepAction(context.Background(), stuck.RunID, stuck.Step, "end", ""); err != nil {
		t.Fatal(err)
	}
	r.tick()
	ended := oneShippingLoop(t, r.shipping(t))
	if ended.Open || ended.Phase != service.ShippingEnded || ended.Outcome != service.ReviewOutcomeStuck || len(ended.Actions) != 0 || ended.ClosedAt == "" {
		t.Fatalf("ended: %+v", ended)
	}
}

func TestShippingSaysAFailingCheckAfterApprovalNeedsYou(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.stewarded(t, stewardInputs)
	if got := oneShippingLoop(t, r.shipping(t)); got.Checks != service.ShippingChecksPassing || got.NeedsYou {
		t.Fatalf("before: checks %q, needs you %v", got.Checks, got.NeedsYou)
	}

	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	r.worker.ReviewGitHub.SetMergeable(12, "MERGEABLE", "BEHIND")
	r.watchTick()
	got := oneShippingLoop(t, r.shipping(t))
	if got.Phase != service.ShippingWatching || got.Checks != service.ShippingChecksFailing || !got.NeedsYou {
		t.Fatalf("phase %q, checks %q, needs you %v", got.Phase, got.Checks, got.NeedsYou)
	}
	if layer := got.Layers[0]; !slices.Equal(layer.FailingChecks, []string{"go"}) || layer.Mergeable != "BEHIND" {
		t.Fatalf("layer %+v", layer)
	}
	if got.Steward == nil || got.Steward.Held {
		t.Fatalf("steward %+v", got.Steward)
	}
}

func TestShippingSaysAStewardIsStuckAtItsReentryLimit(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "steward": "true", "max_reentries": "1"})
	r.reenterClean(t, view.Run.TaskID, "re1")
	r.worker.ReviewGitHub.Push(12, "re2")
	r.worker.Checkout.Change("re2", "greet.go", 3, 5)
	r.watchTick()
	r.tick()

	got := oneShippingLoop(t, r.shipping(t))
	if got.Phase != service.ShippingStuck || !got.NeedsYou || got.Steward == nil || got.Steward.Reentries != 1 || got.Steward.MaxReentries != 1 {
		t.Fatalf("stuck steward: %+v, steward %+v", got, got.Steward)
	}
	if got.Steward.LastWake != "pushed" || got.Steward.LastWakeAt == "" {
		t.Fatalf("last wake %+v", got.Steward)
	}
	if !slices.Equal(got.Actions, []string{"watch_again", "message", "end"}) || got.ApprovalStands == "" {
		t.Fatalf("actions %v, approval %q", got.Actions, got.ApprovalStands)
	}
}

func TestShippingListsAStackWithEveryLayerInOrder(t *testing.T) {
	r := newStackRun(t)
	view := r.start(t, middleOfStack)
	r.tick()

	got := oneShippingLoop(t, r.shipping(t))
	var numbers []int
	for _, layer := range got.Layers {
		numbers = append(numbers, layer.Number)
	}
	if !slices.Equal(numbers, []int{21, 22, 23}) || got.Phase != service.ShippingReviewing {
		t.Fatalf("layers %v, phase %q", numbers, got.Phase)
	}
	// The old inline step's run is its own caller.
	if got.Caller != view.Run.WorkflowName || got.Kind != service.ReviewLoopKindRun {
		t.Fatalf("caller %q, kind %q", got.Caller, got.Kind)
	}
}

func TestShippingListsTicketToMergedPRsLoopAsAChildRunOfItsCaller(t *testing.T) {
	s := startShip(t, ReviewPassOptions{})
	s.approved(t)

	got := oneShippingLoop(t, s.shipping(t))
	if got.Kind != service.ReviewLoopKindChildRun || got.Caller != "ticket-to-merged-pr" || got.CallerTitle != s.show(t, s.parent.Run.ID).Run.WorkflowTitle {
		t.Fatalf("kind %q, caller %q (%q)", got.Kind, got.Caller, got.CallerTitle)
	}
	if got.RunID != s.child(t).Run.ID || got.Phase != service.ShippingWatching {
		t.Fatalf("run %q, phase %q", got.RunID, got.Phase)
	}
	if got.Task == nil || got.Task.ID != "LEG-7" || got.Task.Title != "Fix login" {
		t.Fatalf("task %+v", got.Task)
	}
}

// shipWithoutSteward reviews a pull request with review-loop, without a
// steward, then waits at a gate as a caller waits for the merge.
const shipWithoutSteward = `schema_version: 2
name: ship-without-steward
title: Ship without a steward
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
steps:
  - id: review
    title: Review
    type: workflow.run
    with:
      workflow: review-loop
      inputs:
        repo: ${{ workflow.inputs.repo }}
        pr_number: ${{ workflow.inputs.pr_number }}
  - id: merged
    title: Wait for the merge
    type: gate
    with:
      title: Merged?
`

func TestShippingListsAnApprovedLoopWhoseCallerStillRunsAsWaitingForTheMerge(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	parent := r.startFile(t, shipWithoutSteward, map[string]string{"repo": "acme/widget", "pr_number": "12"})
	r.runs.Open(context.Background())
	r.tick()
	child := r.childOf(t, r.show(t, parent.Run.ID), "review")
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	r.worker.Settle()

	got := oneShippingLoop(t, r.shipping(t))
	if got.Open || got.Phase != service.ShippingWaitingForMerge || got.Outcome != service.ReviewOutcomeApproved || got.Caller != "ship-without-steward" {
		t.Fatalf("loop %+v (parent: %s)", got, stepStates(r.show(t, parent.Run.ID)))
	}
}

func TestShippingListsAHandoffsLoopOnItsCard(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	resp := r.worker.Do(http.MethodGet, "/api/shipping", r.worker.WebToken(), "")
	var view service.ShippingView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	got := oneShippingLoop(t, view)
	if got.Kind != service.ReviewLoopKindHandoff || got.Caller != "handoff" || got.Link != "/board?highlight_task="+r.card {
		t.Fatalf("kind %q, caller %q, link %q", got.Kind, got.Caller, got.Link)
	}
	if got.Task == nil || got.Task.ID != r.card || !got.Open {
		t.Fatalf("task %+v, open %v", got.Task, got.Open)
	}
}
