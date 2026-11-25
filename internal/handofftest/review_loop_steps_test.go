package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// openCloseFile opens a review loop on an input's pull request (or list of
// them) and closes it at once.
const openCloseFile = `schema_version: 2
name: review-steps
title: Review steps
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, default: 0}
  prs: {type: json}
  agent_author: {type: boolean, default: false}
  steward: {type: boolean, default: false}
  max_reentries: {type: integer, default: 0}
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
      prs: ${{ workflow.inputs.prs }}
      agent_author: ${{ workflow.inputs.agent_author }}
      steward: ${{ workflow.inputs.steward }}
      max_reentries: ${{ workflow.inputs.max_reentries }}
  - id: close
    title: Close
    type: github.pr.review.close
    with:
      loop: ${{ steps.open.outputs.loop_id }}
`

// closeOnlyFile closes the loop it is given.
const closeOnlyFile = `schema_version: 2
name: close-another
title: Close another
inputs:
  loop: {type: string, required: true}
steps:
  - id: close
    title: Close
    type: github.pr.review.close
    with:
      loop: ${{ workflow.inputs.loop }}
`

// openOnlyFile opens a loop and stops at a gate, holding it.
const openOnlyFile = `schema_version: 2
name: review-hold
title: Review and hold
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
  - id: hold
    title: Hold
    type: gate
    with:
      title: Hold the loop
`

func (r reviewRun) startFile(t *testing.T, yaml string, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.StartFile(context.Background(), yaml, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func stepOf(t *testing.T, view *store.WorkflowRunView, id string) store.WorkflowStep {
	t.Helper()
	for _, s := range view.Steps {
		if s.StepID == id {
			return s
		}
	}
	t.Fatalf("no step %s", id)
	return store.WorkflowStep{}
}

func TestTheOpenStepOpensALoopAndTheCloseStepClosesItWithTheContractsOutputs(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.startFile(t, openCloseFile, pr12)
	open := outputs(t, view, "open")
	loopID, _ := open["loop_id"].(string)
	if !strings.HasPrefix(loopID, "rl-") || open["linked_task"] != "" {
		t.Fatalf("open outputs = %v", open)
	}
	prs, _ := open["prs"].([]any)
	if len(prs) != 1 || prs[0].(map[string]any)["number"] != float64(12) {
		t.Fatalf("open prs = %v", open["prs"])
	}
	held, err := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12)
	if err != nil || held == nil || held.ID != loopID {
		t.Fatalf("claim = %+v, %v", held, err)
	}
	if close := stepOf(t, view, "close"); close.Status != service.WorkflowStepWaiting {
		t.Fatalf("close step %q before the holder's tick", close.Status)
	}

	r.tick()
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	closed := outputs(t, view, "close")
	if closed["outcome"] != "ended" || closed["passes"] != float64(0) || closed["clean_pass"] != float64(0) || closed["loop_id"] != loopID {
		t.Fatalf("close outputs = %v", closed)
	}
	findings, ok := closed["findings"].(map[string]any)
	if !ok || findings["total"] != float64(0) || findings["blocking"] != float64(0) || findings["nits"] != float64(0) || findings["open"] != float64(0) {
		t.Fatalf("findings = %v", closed["findings"])
	}
	layers, _ := closed["prs"].([]any)
	if len(layers) != 1 {
		t.Fatalf("close prs = %v", closed["prs"])
	}
	layer := layers[0].(map[string]any)
	if layer["number"] != float64(12) || layer["state"] != "open" || layer["approved"] != false || layer["head_sha"] != "bbb222" {
		t.Fatalf("layer = %v", layer)
	}
	if path, _ := closed["summary_path"].(string); path == "" {
		t.Fatal("no summary file")
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("the claim is still held by %s", held.ID)
	}
	if rec, _ := r.worker.Store.GetReviewLoop(ctx, loopID); rec.State != store.ReviewLoopClosedState {
		t.Fatalf("loop state %q", rec.State)
	}
}

func TestAStepGivenAnotherRunsLoopIsRefusedAsNotOwned(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	holder := r.startFile(t, openOnlyFile, pr12)
	loopID, _ := outputs(t, holder, "open")["loop_id"].(string)
	if loopID == "" {
		t.Fatalf("no loop: %+v", stepOf(t, holder, "open"))
	}

	other := r.startFile(t, closeOnlyFile, map[string]string{"loop": loopID})
	close := stepOf(t, other, "close")
	if other.Run.Status != "failed" || close.ErrorCode != service.ReviewRefusalLoopNotOwned {
		t.Fatalf("run %q, close step %q (%s): %s", other.Run.Status, close.Status, close.ErrorCode, close.Error)
	}
	if rec, _ := r.worker.Store.GetReviewLoop(ctx, loopID); rec.State != store.ReviewLoopOpenState {
		t.Fatal("another run closed the loop")
	}

	unknown := r.startFile(t, closeOnlyFile, map[string]string{"loop": "rl-000000000000"})
	if code := stepOf(t, unknown, "close").ErrorCode; code != service.ReviewRefusalLoopNotFound {
		t.Fatalf("an unknown loop: code %q", code)
	}
}

func TestTheOpenStepRefusesBothOrNeitherSubjectAsInvalid(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	for _, tc := range []struct {
		name   string
		inputs map[string]string
	}{
		{"both", map[string]string{"repo": "acme/widget", "pr_number": "12", "prs": "[12]"}},
		{"neither", map[string]string{"repo": "acme/widget"}},
		{"more than twenty", map[string]string{"repo": "acme/widget", "prs": "[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := r.startFile(t, openCloseFile, tc.inputs)
			open := stepOf(t, view, "open")
			if view.Run.Status != "failed" || open.ErrorCode != service.ReviewRefusalInvalidSubject {
				t.Fatalf("run %q, open %q (%s): %s", view.Run.Status, open.Status, open.ErrorCode, open.Error)
			}
		})
	}
}

func TestTheOpenStepRefusesWhatAWorkflowMayNotSetWithACode(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	for _, tc := range []struct {
		name   string
		inputs map[string]string
		code   string
	}{
		{"steward without agent_author", map[string]string{"repo": "acme/widget", "pr_number": "12", "steward": "true"}, service.ReviewRefusalStewardNeedsAgentAuthor},
		{"a raised max_reentries", map[string]string{"repo": "acme/widget", "pr_number": "12", "max_reentries": "6"}, service.ReviewRefusalMaxReentriesRaised},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := r.startFile(t, openCloseFile, tc.inputs)
			if code := stepOf(t, view, "open").ErrorCode; view.Run.Status != "failed" || code != tc.code {
				t.Fatalf("run %q, code %q", view.Run.Status, code)
			}
		})
	}
}

func TestARefusedOpenCarriesItsCodeAndTheOtherLoopsID(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	holder := r.startFile(t, openOnlyFile, pr12)
	loopID, _ := outputs(t, holder, "open")["loop_id"].(string)

	second := r.startFile(t, openCloseFile, pr12)
	open := stepOf(t, second, "open")
	if second.Run.Status != "failed" || open.ErrorCode != service.ReviewRefusalLoopActive || !strings.Contains(open.Error, loopID) {
		t.Fatalf("run %q, open %q (%s): %s", second.Run.Status, open.Status, open.ErrorCode, open.Error)
	}
	if open.ErrorLink != "/workflows/runs/"+holder.Run.ID {
		t.Fatalf("link %q", open.ErrorLink)
	}
}

func TestTheOpenStepLowersMaxReentriesAndKeepsReadyAndSteward(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	withBoardAuthor(t, r)
	view := r.startFile(t, openCloseFile, map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "steward": "true", "max_reentries": "2"})
	open := outputs(t, view, "open")
	if open["linked_task"] != "board-1" {
		t.Fatalf("open outputs = %v (%s)", open, stepOf(t, view, "open").Error)
	}
	rec, err := r.worker.Store.GetReviewLoop(ctx, open["loop_id"].(string))
	if err != nil || rec.MaxReentries != 2 || !rec.Steward || !rec.AgentAuthor || rec.AuthorTask != "board-1" {
		t.Fatalf("loop = %+v, %v", rec, err)
	}
}

// openHoldCloseFile opens a loop, holds it at a gate while the tick
// reviews, then closes it.
const openHoldCloseFile = `schema_version: 2
name: review-hold-close
title: Review, hold, close
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
  - id: hold
    title: Hold
    type: gate
    with:
      title: Hold the loop
  - id: close
    title: Close
    type: github.pr.review.close
    with:
      loop: ${{ steps.open.outputs.loop_id }}
`

// A bottom layer whose approval from pass 1 stands (kept) is approved in
// the close outputs, and each layer says whether GitHub has it a draft.
func TestCloseOutputsCountAKeptApprovalAndReadTheDraftState(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	r.worker.ReviewGitHub.OpenStackedPR(21, "stack/one", "main", "sha21")
	r.worker.ReviewGitHub.OpenStackedPR(22, "stack/two", "stack/one", "sha22")
	r.worker.ReviewGitHub.Draft(21, true)
	view := r.startFile(t, openHoldCloseFile, map[string]string{"repo": "acme/widget", "pr_number": "22"})
	task := view.Run.TaskID
	r.tick()
	top := service.SubmittedFinding{PR: 22, Severity: "blocking", Title: "Greets nobody", Body: "An empty name prints a bare comma.",
		FailureScenario: "greet(\"\") prints \"Hello, \".", Place: &service.ReviewPlace{Path: "greet.go", Line: 4}}
	r.submit(t, task, service.ReviewSubmission{Summary: "One problem on the top.", Findings: []service.SubmittedFinding{top}})
	r.tick()
	r.worker.ReviewGitHub.Push(22, "sha22b")
	r.worker.Checkout.Change("sha22b", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	finding := r.findingsOf(t, task)[0].ID
	r.submit(t, task, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "guarded"}}})
	r.tick()
	r.tick()
	if loop, _ := r.worker.Store.GetHandoffReview(ctx, task); loop == nil || loop.ConfirmedAt == "" {
		t.Fatalf("the loop is not approved: %+v", loop)
	}
	if _, err := r.runs.Workflows.StepAction(ctx, view.Run.ID, "hold", "choose:Continue", ""); err != nil {
		t.Fatal(err)
	}
	r.tick()
	view = r.show(t, view.Run.ID)
	closed := outputs(t, view, "close")
	if closed["outcome"] != "approved" {
		t.Fatalf("close outputs = %v", closed)
	}
	layers, _ := closed["prs"].([]any)
	if len(layers) != 2 {
		t.Fatalf("prs = %v", closed["prs"])
	}
	bottom, top2 := layers[0].(map[string]any), layers[1].(map[string]any)
	if bottom["number"] != float64(21) || bottom["approved"] != true || bottom["draft"] != true {
		t.Fatalf("bottom = %v; want approved (kept) and a draft", bottom)
	}
	if top2["number"] != float64(22) || top2["approved"] != true || top2["draft"] != false {
		t.Fatalf("top = %v", top2)
	}
}

func (r reviewRun) findingsOf(t *testing.T, taskID string) []store.HandoffReviewFinding {
	t.Helper()
	findings, err := r.worker.Store.ListHandoffReviewFindings(context.Background(), taskID)
	if err != nil || len(findings) == 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	return findings
}
