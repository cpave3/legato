package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The steward (spec LEG-559 § The steward; LEG-590): review-loop with
// steward on carries on after approval, watching the pull requests for its
// author agent.

var stewardInputs = map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "steward": "true"}

// stewarded runs review-loop with inputs to its first approval (pass 1
// clean, confirmed at GitHub's heads), so the steward watches. The board
// task board-1's agent is the author.
func (r reviewRun) stewarded(t *testing.T, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.StartReviewLoop(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "running" || !r.watching(t, view) {
		t.Fatalf("run %q (%s) is not watching: %s", view.Run.Status, view.Run.Error, stepStates(view))
	}
	return view
}

// watching says the steward's newest watch step waits.
func (r reviewRun) watching(t *testing.T, view *store.WorkflowRunView) bool {
	t.Helper()
	watches := stepsOf(view, "watch")
	return len(watches) > 0 && watches[len(watches)-1].Status == service.WorkflowStepWaiting
}

func stepStates(view *store.WorkflowRunView) string {
	var states []string
	for _, s := range view.Steps {
		states = append(states, s.Path+"="+s.Status)
	}
	return strings.Join(states, " ")
}

// watchTick moves the clock past the steward's next watch, then ticks.
func (r reviewRun) watchTick() {
	r.worker.Clock.Advance(90 * time.Second)
	r.tick()
}

// record is the run's loop record.
func (r reviewRun) record(t *testing.T, view *store.WorkflowRunView) *store.ReviewLoop {
	t.Helper()
	rec, err := r.worker.Store.CurrentReviewLoop(context.Background(), view.Run.TaskID)
	if err != nil || rec == nil {
		t.Fatalf("loop of %s: %v", view.Run.TaskID, err)
	}
	return rec
}

func TestAFailingCheckAfterApprovalReachesTheAuthorOnceTaggedSteward(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	r.watchTick()
	r.worker.Clock.Advance(3 * time.Minute) // the quiet time
	r.tick()
	r.watchTick()
	r.watchTick()

	typed := r.worker.typedTo("legato-board-1")
	if n := strings.Count(typed, "[legato steward] 1 failing check"); n != 1 {
		t.Fatalf("the author was told %d times; typed %q", n, typed)
	}
	if strings.Contains(typed, "[legato review pass] 1 failing check") {
		t.Fatalf("told as a review pass: %q", typed)
	}
	feedback, err := service.ReviewFeedback(context.Background(), r.worker.Store, "board-1")
	if err != nil || len(feedback.Items) != 1 || feedback.Items[0].ID != "k-12-go" {
		t.Fatalf("feedback %+v, %v", feedback, err)
	}
	operation := feedback.Items[0].Operation
	for _, want := range []string{"git.push", "actions.write", "re-run"} {
		if !strings.Contains(operation, want) {
			t.Fatalf("the item names no %s: %q", want, operation)
		}
	}
	// Routing starts no pass, and the approval stands.
	if rec := r.record(t, r.show(t, view.Run.ID)); rec.Pass != 1 || rec.Reentries != 0 {
		t.Fatalf("loop at pass %d with %d re-entries", rec.Pass, rec.Reentries)
	}
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseClean {
		t.Fatalf("loop %+v", loop)
	}
	if got := r.show(t, view.Run.ID); !r.watching(t, got) {
		t.Fatalf("not watching: %s", stepStates(got))
	}
}
