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

// callerFile calls review-loop with workflow.run and branches on its
// outcome, as ticket-to-merged-pr does (ticket_to_merged_pr_test.go).
const callerFile = `schema_version: 2
name: ship-it
title: Ship it
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
  max_passes: {type: integer, default: 0}
steps:
  - id: review
    title: Review
    type: workflow.run
    with:
      workflow: review-loop
      inputs: {repo: "${{ workflow.inputs.repo }}", pr_number: "${{ workflow.inputs.pr_number }}", max_passes: "${{ workflow.inputs.max_passes }}"}
  - id: approved
    title: Approved
    if: {field: steps.review.outputs.outcome, equals: approved}
    type: task.note
    with: {task: "${{ workflow.task }}", body: "approved"}
  - id: closed
    title: Closed
    if: {field: steps.review.outputs.outcome, equals: closed}
    type: task.note
    with: {task: "${{ workflow.task }}", body: "closed"}
  - id: stuck
    title: Stuck
    if: {field: steps.review.outputs.outcome, equals: stuck}
    type: task.note
    with: {task: "${{ workflow.task }}", body: "stuck"}
`

// childOf is the child run a parent's workflow.run step started.
func (r reviewRun) childOf(t *testing.T, parent *store.WorkflowRunView, step string) *store.WorkflowRunView {
	t.Helper()
	id := stepOf(t, parent, step).ChildRunID
	if id == "" {
		t.Fatalf("step %s started no child: %+v", step, stepOf(t, parent, step))
	}
	return r.show(t, id)
}

// branch says which of the parent's outcome steps ran.
func branch(t *testing.T, view *store.WorkflowRunView) string {
	t.Helper()
	ran := ""
	for _, id := range []string{"approved", "closed", "stuck"} {
		if s := stepOf(t, view, id); s.Status == "succeeded" {
			if ran != "" {
				t.Fatalf("both %s and %s ran", ran, id)
			}
			ran = id
		}
	}
	return ran
}

func TestReviewLoopsOutcomeApprovedReachesTheParentsIfWithTheContractsOutputs(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	parent := r.startFile(t, callerFile, pr12)
	child := r.childOf(t, parent, "review")
	if child.Run.WorkflowName != "review-loop" || child.Run.Status != "running" {
		t.Fatalf("child %s %q: %s", child.Run.WorkflowName, child.Run.Status, child.Run.Error)
	}
	r.tick()
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()

	parent = r.show(t, parent.Run.ID)
	if parent.Run.Status != "succeeded" || branch(t, parent) != "approved" {
		t.Fatalf("parent %q (%s), branch %q", parent.Run.Status, parent.Run.Error, branch(t, parent))
	}
	out := outputs(t, parent, "review")
	findings, ok := out["findings"].(map[string]any)
	if out["outcome"] != "approved" || out["passes"] != float64(1) || out["clean_pass"] != float64(1) || out["confirmed_at"] == "" || !ok || findings["total"] != float64(0) {
		t.Fatalf("review outputs = %v", out)
	}
	if id, _ := out["loop_id"].(string); id == "" || out["linked_task"] != "" || out["summary_path"] == "" {
		t.Fatalf("review outputs = %v", out)
	}
	prs, _ := out["prs"].([]any)
	if len(prs) != 1 || prs[0].(map[string]any)["approved"] != true || prs[0].(map[string]any)["head_sha"] != "bbb222" {
		t.Fatalf("prs = %v", out["prs"])
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 12); held != nil {
		t.Fatalf("the approved loop still holds #12: %s", held.ID)
	}
}

func TestReviewLoopsOutcomeClosedReachesTheParentsIf(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	parent := r.startFile(t, callerFile, pr12)
	child := r.childOf(t, parent, "review")
	r.tick()
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	r.worker.ReviewGitHub.ClosePR(12)
	r.tick()
	r.tick()

	parent = r.show(t, parent.Run.ID)
	if parent.Run.Status != "succeeded" || branch(t, parent) != "closed" || outputs(t, parent, "review")["outcome"] != "closed" {
		t.Fatalf("parent %q (%s), branch %q, outputs %v", parent.Run.Status, parent.Run.Error, branch(t, parent), outputs(t, parent, "review"))
	}
}

func TestReviewLoopsOutcomeStuckReachesTheParentsIfOnlyOnceTheOperatorEndsIt(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	parent := r.startFile(t, callerFile, map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "1"})
	child := r.childOf(t, parent, "review")
	r.tick()
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()
	if got := r.show(t, parent.Run.ID); got.Run.Status != "running" {
		t.Fatalf("the parent %q while the loop is stuck", got.Run.Status)
	}
	items := r.attention(t)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "end", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	parent = r.show(t, parent.Run.ID)
	if parent.Run.Status != "succeeded" || branch(t, parent) != "stuck" || outputs(t, parent, "review")["outcome"] != "stuck" {
		t.Fatalf("parent %q (%s), branch %q", parent.Run.Status, parent.Run.Error, branch(t, parent))
	}
}

func TestARefusedReviewLoopFailsTheParentsStepWithItsCode(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	holder, err := r.runs.StartReviewLoop(context.Background(), pr12)
	if err != nil {
		t.Fatal(err)
	}
	loopID, _ := outputs(t, holder, "open")["loop_id"].(string)

	parent := r.startFile(t, callerFile, pr12)
	review := stepOf(t, parent, "review")
	if parent.Run.Status != "failed" || review.ErrorCode != service.ReviewRefusalLoopActive {
		t.Fatalf("parent %q, review %q (%s): %s", parent.Run.Status, review.Status, review.ErrorCode, review.Error)
	}
	child := r.childOf(t, parent, "review")
	if open := stepOf(t, child, "open"); open.ErrorCode != service.ReviewRefusalLoopActive || !strings.Contains(open.Error, loopID) {
		t.Fatalf("the child's open = %+v", open)
	}
}

func TestRetryOfAFailedReviewLoopChildResumesTheSameChildAndLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	parent := r.startFile(t, callerFile, pr12)
	child := r.childOf(t, parent, "review")
	r.tick()
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	child = r.show(t, child.Run.ID)
	loopID, _ := outputs(t, child, "open")["loop_id"].(string)

	// The operator fails the child's wait for the author.
	author := stepsOf(child, "author")
	if len(author) != 1 || author[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("author = %+v", author)
	}
	attempt := latestAttempt(t, child, author[0].Path)
	if _, err := r.runs.Workflows.Fail(ctx, child.Run.ID, author[0].Path, attempt.ID, "the operator failed it"); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if got := r.show(t, parent.Run.ID); got.Run.Status != "failed" {
		t.Fatalf("parent %q after the child failed", got.Run.Status)
	}
	if rec, _ := r.worker.Store.GetReviewLoop(ctx, loopID); rec.State != store.ReviewLoopOpenState {
		t.Fatal("the failed child's loop closed")
	}

	if _, err := r.runs.Workflows.Retry(ctx, parent.Run.ID, "review", false); err != nil {
		t.Fatalf("retry: %v", err)
	}
	r.worker.Settle()
	parent = r.show(t, parent.Run.ID)
	if got := r.childOf(t, parent, "review"); got.Run.ID != child.Run.ID {
		t.Fatalf("Retry started child %s; want %s again", got.Run.ID, child.Run.ID)
	}
	finding := r.findings(t, child.Run.TaskID)[0].ID
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if loop := r.loopPhase(t, child.Run.TaskID); loop.LoopID != loopID || loop.Pass != 2 {
		t.Fatalf("after Retry: loop %s pass %d; want %s pass 2", loop.LoopID, loop.Pass, loopID)
	}
	r.submit(t, child.Run.TaskID, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "guarded"}}})
	r.tick()
	r.tick()
	parent = r.show(t, parent.Run.ID)
	if parent.Run.Status != "succeeded" || branch(t, parent) != "approved" || outputs(t, parent, "review")["loop_id"] != loopID {
		t.Fatalf("parent %q (%s), outputs %v", parent.Run.Status, parent.Run.Error, outputs(t, parent, "review"))
	}
}

// Both run in one Legato, from the same engine. One after the other: the
// fake checkout has one HEAD, which two reviewers at once would share.
func TestAnOldReviewPRPassesCopyStillRunsBesideReviewLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	r.worker.ReviewGitHub.OpenPR(13, "feature/farewell", "fff666")
	loop, err := r.runs.StartReviewLoop(ctx, pr12)
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.submit(t, loop.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	got := r.show(t, loop.Run.ID)
	var runOutputs map[string]any
	_ = json.Unmarshal([]byte(got.Run.OutputsJSON), &runOutputs)
	if got.Run.Status != "succeeded" || runOutputs["outcome"] != "approved" {
		t.Fatalf("review-loop %q (%s), outputs %v", got.Run.Status, got.Run.Error, runOutputs)
	}

	old := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "13", "scope": "pr"})
	r.tick()
	r.submit(t, old.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	if got := r.show(t, old.Run.ID); got.Run.Status != "succeeded" || outputs(t, got, "review")["outcome"] != "approved" {
		t.Fatalf("review-pr-passes %q (%s)", got.Run.Status, got.Run.Error)
	}
}
