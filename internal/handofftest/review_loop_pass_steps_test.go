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

// passStepsFile reviews in a repeat loop of pass, confirm and wait steps,
// then closes the loop: the shape of the built-in review-loop.
const passStepsFile = `schema_version: 2
name: review-by-steps
title: Review by steps
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
  max_passes: {type: integer, default: 0}
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
      max_passes: ${{ workflow.inputs.max_passes }}
  - id: review
    title: Review
    loop: {repeat: true, max: 20, until: {field: loop.last.state, in: [approved, closed, stuck]}}
    outputs:
      state: {type: string, value: "${{ steps.confirm.outputs.state ?? steps.author.outputs.state ?? steps.pass.outputs.state }}"}
    steps:
      - id: pass
        title: Pass
        type: github.pr.review.pass
        with: {loop: "${{ steps.open.outputs.loop_id }}"}
      - id: confirm
        title: Confirm
        if: {field: steps.pass.outputs.verdict, equals: clean}
        type: github.pr.review.confirm
        with: {loop: "${{ steps.open.outputs.loop_id }}"}
      - id: author
        title: Author
        if: {field: steps.pass.outputs.verdict, equals: changes}
        type: github.pr.review.wait
        with: {loop: "${{ steps.open.outputs.loop_id }}", mode: author}
  - id: close
    title: Close
    type: github.pr.review.close
    with: {loop: "${{ steps.open.outputs.loop_id }}"}
`

// stepsOf are the run's steps of an ID that ran (not skipped), in the
// order they ran.
func stepsOf(view *store.WorkflowRunView, id string) []store.WorkflowStep {
	var out []store.WorkflowStep
	for _, s := range view.Steps {
		if s.StepID == id && s.Status != "skipped" {
			out = append(out, s)
		}
	}
	return out
}

func stepOutputs(t *testing.T, s store.WorkflowStep) map[string]any {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal([]byte(s.OutputsJSON), &values); err != nil {
		t.Fatalf("outputs of %s (%s): %v", s.Path, s.Status, err)
	}
	return values
}

func (r reviewRun) loopRecord(t *testing.T, view *store.WorkflowRunView) *store.ReviewLoop {
	t.Helper()
	id, _ := outputs(t, view, "open")["loop_id"].(string)
	rec, err := r.worker.Store.GetReviewLoop(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// passOneChanges starts the steps and posts pass 1 with one blocking
// finding.
func passOneChanges(t *testing.T, opts ReviewPassOptions, inputs map[string]string) (reviewRun, *store.WorkflowRunView) {
	t.Helper()
	r := newReviewRun(t, opts)
	view := r.startFile(t, passStepsFile, inputs)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	return r, r.show(t, view.Run.ID)
}

func TestThePassWaitAndConfirmStepsRunAReviewToApproval(t *testing.T) {
	r, view := passOneChanges(t, ReviewPassOptions{}, pr12)
	passes := stepsOf(view, "pass")
	if len(passes) != 1 || passes[0].Status != "succeeded" {
		t.Fatalf("pass steps = %+v", passes)
	}
	if out := stepOutputs(t, passes[0]); out["verdict"] != "changes" || out["state"] != "posted" || out["pass"] != float64(1) {
		t.Fatalf("pass 1 outputs = %v", out)
	}
	if author := stepsOf(view, "author"); len(author) != 1 || author[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("author steps = %+v", author)
	}

	// The author pushes; after the quiet time pass 2 runs and is clean.
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	view = r.show(t, view.Run.ID)
	if author := stepsOf(view, "author"); stepOutputs(t, author[0])["state"] != "ready" {
		t.Fatalf("author outputs = %v", stepOutputs(t, author[0]))
	}
	finding := r.findings(t, view.Run.TaskID)[0].ID
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "guarded"}}})
	r.tick()
	r.tick()
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	passes = stepsOf(view, "pass")
	if len(passes) != 2 || stepOutputs(t, passes[1])["verdict"] != "clean" || stepOutputs(t, passes[1])["pass"] != float64(2) {
		t.Fatalf("pass steps = %+v", passes)
	}
	confirm := stepsOf(view, "confirm")
	if len(confirm) != 1 || stepOutputs(t, confirm[0])["state"] != "approved" || stepOutputs(t, confirm[0])["pass"] != float64(2) {
		t.Fatalf("confirm steps = %+v", confirm)
	}
	closed := outputs(t, view, "close")
	if closed["outcome"] != "approved" || closed["passes"] != float64(2) || closed["clean_pass"] != float64(2) || closed["confirmed_at"] == "" {
		t.Fatalf("close outputs = %v", closed)
	}
	layer := closed["prs"].([]any)[0].(map[string]any)
	if layer["approved"] != true || layer["head_sha"] != "ccc333" {
		t.Fatalf("layer = %v", layer)
	}
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 2 || posted[1].Event != service.ReviewApprove || posted[1].CommitID != "ccc333" {
		t.Fatalf("posted = %+v", posted)
	}
}

func TestAReviewerKilledOnceRerunsThePassAtTheSamePinsAndASecondKillIsStuck(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.startFile(t, passStepsFile, pr12)
	task := view.Run.TaskID
	r.tick()
	first, err := r.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	// GitHub's head moves meanwhile: the re-run keeps the pass's pin.
	_ = r.worker.Tmux.Kill("legato-" + task + "_reviewer")
	r.tick()
	second, err := r.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer")
	if err != nil || second.ExecutionID == first.ExecutionID {
		t.Fatalf("no new reviewer: %+v, %v", second, err)
	}
	loop := r.loopPhase(t, task)
	if loop.Pass != 1 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("after one death: pass %d, phase %q", loop.Pass, loop.Phase)
	}
	if rec := r.loopRecord(t, r.show(t, view.Run.ID)); rec.PassDeaths != 1 {
		t.Fatalf("deaths = %d", rec.PassDeaths)
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+task+"/1/12"] != "bbb222" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}

	_ = r.worker.Tmux.Kill("legato-" + task + "_reviewer")
	r.tick()
	loop = r.loopPhase(t, task)
	if loop.Pass != 1 || loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "exited twice") {
		t.Fatalf("after two deaths: pass %d, phase %q (%s)", loop.Pass, loop.Phase, loop.Reason)
	}
	view = r.show(t, view.Run.ID)
	if pass := stepsOf(view, "pass"); len(pass) != 1 || pass[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("the pass step %+v; want it waiting on the stuck loop", pass)
	}
	items := r.attention(t)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(ctx, service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "end", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	view = r.show(t, view.Run.ID)
	if pass := stepsOf(view, "pass"); stepOutputs(t, pass[0])["state"] != "stuck" || view.Run.Status != "succeeded" {
		t.Fatalf("run %q, pass %+v", view.Run.Status, pass)
	}
}

// The limit is reached as the author's round ends, so the step that waits
// on the stuck loop is the wait; End ends it, and the loop, as stuck.
func TestAStuckLoopWaitsWithItsThreeActionsAndEndGivesStuck(t *testing.T) {
	r, view := passOneChanges(t, ReviewPassOptions{}, map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "1"})
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()
	view = r.show(t, view.Run.ID)
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseStuck {
		t.Fatalf("phase %q", loop.Phase)
	}
	items := r.attention(t)
	if len(items) != 1 || !strings.Contains(items[0].Reason, "limit of 1 passes") {
		t.Fatalf("items = %+v", items)
	}
	var ids []string
	for _, action := range items[0].Actions {
		ids = append(ids, action.ID)
	}
	if strings.Join(ids, ",") != "open,another_pass,end" {
		t.Fatalf("actions = %v", ids)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run %q while stuck", view.Run.Status)
	}

	adapter := service.NewWorkflowAttentionAdapter(r.runs.Workflows)
	if err := adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "end", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	author := stepsOf(view, "author")
	if len(author) != 1 || stepOutputs(t, author[0])["state"] != "stuck" {
		t.Fatalf("author steps = %+v", author)
	}
	if closed := outputs(t, view, "close"); closed["outcome"] != "stuck" {
		t.Fatalf("close outputs = %v", closed)
	}
}

func TestAnotherPassRaisesOnlyTheCurrentWindowByOne(t *testing.T) {
	r, view := passOneChanges(t, ReviewPassOptions{}, map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "1"})
	task := view.Run.TaskID
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseStuck {
		t.Fatalf("phase %q", loop.Phase)
	}
	items := r.attention(t)
	adapter := service.NewWorkflowAttentionAdapter(r.runs.Workflows)
	if err := adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "another_pass"}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if !r.reviewer(t, task) {
		t.Fatal("Another pass started no pass")
	}
	finding := r.findings(t, task)[0].ID
	r.submit(t, task, service.ReviewSubmission{Summary: "Still wrong.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "open", Reason: "still prints a bare comma"}}})
	r.tick()
	// The setting is as it was; one more pass was all the window got.
	if s := r.settings(t, task); s.MaxPasses != 1 {
		t.Fatalf("max passes = %d", s.MaxPasses)
	}
	r.worker.ReviewGitHub.Push(12, "ddd444")
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseStuck || loop.Pass != 2 {
		t.Fatalf("after pass 2: pass %d, phase %q", loop.Pass, loop.Phase)
	}
	if rec := r.loopRecord(t, r.show(t, view.Run.ID)); rec.WindowStart != 1 {
		t.Fatalf("window start = %d", rec.WindowStart)
	}
}

func (r reviewRun) findings(t *testing.T, taskID string) []store.HandoffReviewFinding {
	t.Helper()
	findings, err := r.worker.Store.ListHandoffReviewFindings(context.Background(), taskID)
	if err != nil || len(findings) == 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	return findings
}
