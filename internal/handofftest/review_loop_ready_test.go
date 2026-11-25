package handofftest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// readyStack is a worker whose acme/widget has a stack of two draft pull
// requests, #21 on main and #22 on #21.
func readyStack(t *testing.T, opts ReviewPassOptions) reviewRun {
	t.Helper()
	r := newReviewRun(t, opts)
	r.worker.ReviewGitHub.OpenStackedPR(21, "stack/one", "main", "sha21")
	r.worker.ReviewGitHub.OpenStackedPR(22, "stack/two", "stack/one", "sha22")
	r.worker.ReviewGitHub.Draft(21, true)
	r.worker.ReviewGitHub.Draft(22, true)
	return r
}

var readyInputs = map[string]string{"repo": "acme/widget", "pr_number": "22", "ready": "true"}

func runOutputs(t *testing.T, view *store.WorkflowRunView) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(view.Run.OutputsJSON), &out); err != nil {
		t.Fatalf("run outputs %q: %v", view.Run.OutputsJSON, err)
	}
	return out
}

// approveAll runs review-loop's pass 1 clean, confirmed.
func (r reviewRun) approveAll(t *testing.T, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.StartReviewLoop(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	return r.show(t, view.Run.ID)
}

func TestReadyMarksAnApprovedStacksDraftsReadyBottomUp(t *testing.T) {
	r := readyStack(t, ReviewPassOptions{})
	view := r.approveAll(t, readyInputs)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	if got := r.worker.ReviewGitHub.Readied(); len(got) != 2 || got[0] != 21 || got[1] != 22 {
		t.Fatalf("readied %v; want #21 then #22", got)
	}
	out := runOutputs(t, view)
	if out["outcome"] != "approved" || out["ready"] != true {
		t.Fatalf("outputs = %v", out)
	}
	ready := outputs(t, view, "ready")
	if flipped, _ := ready["flipped"].([]any); len(flipped) != 2 || flipped[0] != float64(21) || flipped[1] != float64(22) {
		t.Fatalf("ready outputs = %v", ready)
	}
}

func TestReadyLeavesAReadyLayerAloneAndNothingGoesBackToDraft(t *testing.T) {
	r := readyStack(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.Draft(21, false)
	view := r.approveAll(t, readyInputs)
	if got := r.worker.ReviewGitHub.Readied(); len(got) != 1 || got[0] != 22 {
		t.Fatalf("readied %v; want #22 alone", got)
	}
	if flipped, _ := outputs(t, view, "ready")["flipped"].([]any); len(flipped) != 1 || flipped[0] != float64(22) {
		t.Fatalf("ready outputs = %v", outputs(t, view, "ready"))
	}
}

func TestWithoutReadyTheStackIsLeftADraft(t *testing.T) {
	r := readyStack(t, ReviewPassOptions{})
	view := r.approveAll(t, map[string]string{"repo": "acme/widget", "pr_number": "22"})
	if view.Run.Status != "succeeded" || len(r.worker.ReviewGitHub.Readied()) != 0 || runOutputs(t, view)["ready"] != false {
		t.Fatalf("run %q, readied %v, outputs %v", view.Run.Status, r.worker.ReviewGitHub.Readied(), runOutputs(t, view))
	}
	if s := stepOf(t, view, "ready"); s.Status != "skipped" {
		t.Fatalf("ready step %q", s.Status)
	}
}

func TestALayerThatWillNotBeMarkedReadyFailsTheStepIntoItsOnFailure(t *testing.T) {
	r := readyStack(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.RefuseReady(22)
	view := r.approveAll(t, readyInputs)
	ready := stepOf(t, view, "ready")
	if ready.Status != service.WorkflowStepPaused || ready.ErrorCode != service.ReviewRefusalReadyFailed || !strings.Contains(ready.Error, "#22") {
		t.Fatalf("ready %q (%s): %s", ready.Status, ready.ErrorCode, ready.Error)
	}
	if got := r.worker.ReviewGitHub.Readied(); len(got) != 1 || got[0] != 21 {
		t.Fatalf("readied %v; want #21, below the refusal", got)
	}
}

// readyEarlyFile marks the loop ready before any review.
const readyEarlyFile = `schema_version: 2
name: ready-early
title: Ready early
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with: {repo: acme/widget, pr_number: 12}
  - id: ready
    title: Ready
    type: github.pr.ready
    with: {loop: "${{ steps.open.outputs.loop_id }}"}
`

func TestReadyBeforeApprovalIsRefused(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.Draft(12, true)
	view := r.startFile(t, readyEarlyFile, nil)
	r.tick()
	view = r.show(t, view.Run.ID)
	if ready := stepOf(t, view, "ready"); view.Run.Status != "failed" || ready.ErrorCode != service.ReviewRefusalNotApprovedAtHeads {
		t.Fatalf("run %q, ready %q (%s): %s", view.Run.Status, ready.Status, ready.ErrorCode, ready.Error)
	}
	if len(r.worker.ReviewGitHub.Readied()) != 0 {
		t.Fatal("a pull request was marked ready")
	}
}

// An outsider's pull request is clean but gets a COMMENT, not an APPROVE:
// it is never marked ready on the withheld approval.
func TestReadyRefusesAnApprovalWithheldFromAnOutsider(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.Draft(12, true)
	r.worker.ReviewGitHub.SetAuthor(12, "stranger", "CONTRIBUTOR")
	view := r.approveAll(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "ready": "true"})
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 || posted[0].Event != service.ReviewComment {
		t.Fatalf("posted %+v", posted)
	}
	if ready := stepOf(t, view, "ready"); ready.ErrorCode != service.ReviewRefusalNotApprovedAtHeads || len(r.worker.ReviewGitHub.Readied()) != 0 {
		t.Fatalf("ready %q (%s): %s; readied %v", ready.Status, ready.ErrorCode, ready.Error, r.worker.ReviewGitHub.Readied())
	}
}

// The sign-off seam: a hold keeps every later approval off GitHub, the one
// posted once checks settle and the ready step's.
func TestAHoldOnThePublishSeamSuppressesEveryLaterApproval(t *testing.T) {
	held := func(context.Context, string, int) bool { return true }
	r := newReviewRun(t, ReviewPassOptions{HoldApproval: held})
	withBoardAuthor(t, r)
	r.worker.ReviewGitHub.Draft(12, true)
	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	ctx := context.Background()
	view, err := r.runs.StartReviewLoop(ctx, map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "ready": "true"})
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", "k-12-go", service.AnswerReplied, "A flaky test.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
	for _, post := range r.worker.ReviewGitHub.Posted() {
		if post.Event == service.ReviewApprove {
			t.Fatalf("an approval was posted while held: %+v", post)
		}
	}
	view = r.show(t, view.Run.ID)
	if ready := stepOf(t, view, "ready"); ready.ErrorCode != service.ReviewRefusalNotApprovedAtHeads || len(r.worker.ReviewGitHub.Readied()) != 0 {
		var states []string
		for _, s := range view.Steps {
			states = append(states, s.Path+"="+s.Status)
		}
		t.Fatalf("ready %q (%s): %s; steps %v; loop %+v", ready.Status, ready.ErrorCode, ready.Error, states, r.loopPhase(t, view.Run.TaskID))
	}
}

func TestTheAuthorOfARunThatMarksReadyIsToldNotToMarkItItself(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, err := r.runs.StartReviewLoop(context.Background(), map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "ready": "true"})
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "Do not mark your pull requests ready") {
		t.Fatalf("typed to the author: %q", typed)
	}
}
