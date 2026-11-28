package handofftest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Draft until approved (LEG-712): an agent-authored review loop that marks
// its stack ready keeps GitHub's draft state as its signal, so "ready for
// review" means approved at the head. It turns its pull requests into
// drafts when it opens, and again whenever they go back to review; only an
// approval at the head marks them ready.

var draftInputs = map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "ready": "true"}

var draftStewardInputs = map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "ready": "true", "steward": "true"}

// layerRecord is the loop's record of one layer.
func (r reviewRun) layerRecord(t *testing.T, view *store.WorkflowRunView, number int) store.ReviewLoopPR {
	t.Helper()
	layers, err := r.worker.Store.ListReviewLoopPRs(context.Background(), r.record(t, view).ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range layers {
		if l.Number == number {
			return l
		}
	}
	t.Fatalf("no layer #%d in %+v", number, layers)
	return store.ReviewLoopPR{}
}

func wantFlips(t *testing.T, r reviewRun, want ...string) {
	t.Helper()
	if got := r.worker.ReviewGitHub.Flips(); !slices.Equal(got, want) {
		t.Fatalf("flips %v; want %v", got, want)
	}
}

func TestAnAgentLoopOpenedOnAReadyPRTurnsItIntoADraftUntilApprovedAtTheHead(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.startLoop(t, draftInputs)
	r.tick()
	wantFlips(t, r, "draft 12")
	if layer := r.layerRecord(t, view, 12); layer.LegatoDraftedAt == "" || layer.DraftProblem != "" {
		t.Fatalf("layer %+v; want it recorded as drafted by Legato", layer)
	}

	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	wantFlips(t, r, "draft 12", "ready 12")
	if view = r.show(t, view.Run.ID); view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s (%s)", view.Run.Status, view.Run.Error, stepStates(view))
	}
}

func TestABlockingPassTurnsAPRTheAuthorMarkedReadyBackIntoADraft(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.startLoop(t, draftInputs)
	r.tick()
	r.worker.ReviewGitHub.Draft(12, false) // the author ran gh pr ready itself
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	wantFlips(t, r, "draft 12", "draft 12")
}

func TestAPushAfterApprovalTurnsThePRBackIntoADraftUntilApprovedAgain(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, draftStewardInputs)
	wantFlips(t, r, "draft 12", "ready 12")

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	wantFlips(t, r, "draft 12", "ready 12", "draft 12")

	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Still right."})
	r.tick()
	r.tick()
	r.tick()
	wantFlips(t, r, "draft 12", "ready 12", "draft 12", "ready 12")
}

func TestANewFailingCheckAfterApprovalTurnsThePRBackIntoADraft(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.stewarded(t, draftStewardInputs)
	wantFlips(t, r, "draft 12", "ready 12")

	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	r.watchTick()
	wantFlips(t, r, "draft 12", "ready 12", "draft 12")
}

func TestALoopThatDoesNotMarkReadyOrIsNotAnAgentsLeavesDraftStateAlone(t *testing.T) {
	for name, inputs := range map[string]map[string]string{
		"a person's pull request": {"repo": "acme/widget", "pr_number": "12", "ready": "true"},
		"no ready":                {"repo": "acme/widget", "pr_number": "12", "agent_author": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			withBoardAuthor(t, r)
			view := r.startLoop(t, inputs)
			r.tick()
			r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
			r.tick()
			if got := r.worker.ReviewGitHub.Drafted(); len(got) != 0 {
				t.Fatalf("drafted %v", got)
			}
		})
	}
}

func TestADraftGitHubRefusesIsSurfacedAndTheLoopGoesOn(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	r.worker.ReviewGitHub.RefuseDraft(12)
	view := r.startLoop(t, draftInputs)
	r.tick()
	if layer := r.layerRecord(t, view, 12); !strings.Contains(layer.DraftProblem, "Resource not accessible by integration") || layer.LegatoDraftedAt != "" {
		t.Fatalf("layer %+v; want the refusal recorded", layer)
	}
	d := r.pageDetails(t, view.Run.ID)
	if len(d.Layers) != 1 || !strings.Contains(d.Layers[0].DraftProblem, "cannot turn #12 into a draft") {
		t.Fatalf("run page layers %+v", d.Layers)
	}
	shipping, err := r.runs.Caller.Shipping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(shipping.Loops) != 1 || len(shipping.Loops[0].Layers) != 1 || shipping.Loops[0].Layers[0].DraftProblem == "" {
		t.Fatalf("shipping %+v", shipping)
	}

	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	if view = r.show(t, view.Run.ID); view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s (%s)", view.Run.Status, view.Run.Error, stepStates(view))
	}
}

// Without a steward nothing watches after approval, so a later push leaves
// the pull request ready: the open step says so.
func TestAnAgentLoopThatMarksReadyWithoutAStewardWarnsAtOpen(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.startLoop(t, draftInputs)
	r.tick()
	warnings, _ := outputs(t, r.show(t, view.Run.ID), "open")["warnings"].([]any)
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w.(string), "no steward")
	}
	if !found {
		t.Fatalf("open warnings %v", warnings)
	}

	r2 := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r2)
	view = r2.startLoop(t, draftStewardInputs)
	r2.tick()
	warnings, _ = outputs(t, r2.show(t, view.Run.ID), "open")["warnings"].([]any)
	for _, w := range warnings {
		if strings.Contains(w.(string), "no steward") {
			t.Fatalf("a stewarded loop warned: %v", warnings)
		}
	}
}
