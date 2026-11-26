package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The steward's budget and the operator's hold on it (spec LEG-559 § The
// steward › Budgets, Held and End; LEG-592): the re-entry budget, Stop
// watching, Watch again, End, and the idle end.

// stewardAct takes an operator's action on the steward's waiting step, as
// the run page's waiting-step action route does.
func (r reviewRun) stewardAct(t *testing.T, view *store.WorkflowRunView, action string) error {
	t.Helper()
	view = r.show(t, view.Run.ID)
	for i := len(view.Attempts) - 1; i >= 0; i-- {
		attempt := view.Attempts[i]
		if attempt.Status == service.WorkflowStepWaiting && strings.HasPrefix(attempt.Path, "steward/") {
			_, err := r.runs.Workflows.StepAction(context.Background(), view.Run.ID, attempt.Path, action, "")
			return err
		}
	}
	t.Fatalf("no steward step waits: %s", stepStates(view))
	return nil
}

// reenterClean pushes head to #12 and reviews it clean: one re-entry,
// approved again at head.
func (r reviewRun) reenterClean(t *testing.T, task, head string) {
	t.Helper()
	r.worker.ReviewGitHub.Push(12, head)
	r.worker.Checkout.Change(head, "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "Still right."})
	r.tick()
	r.tick()
	posted := r.worker.ReviewGitHub.Posted()
	if last := posted[len(posted)-1]; last.Event != service.ReviewApprove || last.CommitID != head {
		t.Fatalf("posted %+v; want an APPROVE at %s", last, head)
	}
}

func TestTheSixthReEntryMakesTheStewardStuckAndWatchAgainAllowsExactlyOneMore(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	task := view.Run.TaskID

	for i := 1; i <= 5; i++ {
		r.reenterClean(t, task, fmt.Sprintf("re%d", i))
	}
	if rec := r.record(t, view); rec.Reentries != 5 || rec.MaxReentries != 5 {
		t.Fatalf("re-entries %d of %d", rec.Reentries, rec.MaxReentries)
	}

	// The sixth push goes past the budget: stuck, no pass, no fresh agent.
	spawns := len(r.worker.Tmux.Spawned())
	r.worker.ReviewGitHub.Push(12, "re6")
	r.worker.Checkout.Change("re6", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	loop := r.loopPhase(t, task)
	if loop.Phase != service.ReviewPhaseStuck || loop.Pass != 6 || !strings.Contains(loop.Reason, "re-entr") {
		t.Fatalf("after the sixth push: loop %+v", loop)
	}
	if got := len(r.worker.Tmux.Spawned()); got != spawns {
		t.Fatalf("%d agents started while stuck", got-spawns)
	}
	view = r.show(t, view.Run.ID)
	if rec := r.record(t, view); rec.Reentries != 5 {
		t.Fatalf("re-entries %d", rec.Reentries)
	}
	if view.Run.Status != "running" || !r.watching(t, view) {
		t.Fatalf("run %q is not waiting at the watch: %s", view.Run.Status, stepStates(view))
	}

	// Watch again grants one more: the push re-enters, and is approved.
	if err := r.stewardAct(t, view, "watch_again"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Pass != 7 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("after Watch again: loop %+v", loop)
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Still right."})
	r.tick()
	r.tick()
	if rec := r.record(t, view); rec.Reentries != 6 || rec.CleanPass != 7 {
		t.Fatalf("re-entries %d, clean pass %d", rec.Reentries, rec.CleanPass)
	}

	// Exactly one: the next push is stuck again.
	r.worker.ReviewGitHub.Push(12, "re7")
	r.worker.Checkout.Change("re7", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseStuck || loop.Pass != 7 {
		t.Fatalf("after the seventh push: loop %+v", loop)
	}
}

func TestStopWatchingSendsNothingAndWatchAgainReEntersOnceForPushesWhileHeld(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	task := view.Run.TaskID

	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}
	if rec := r.record(t, view); !rec.Held {
		t.Fatal("Stop watching did not hold the steward")
	}
	typedBefore := r.worker.typedTo("legato-board-1")
	spawns := len(r.worker.Tmux.Spawned())

	// While held: a failing check, a new comment and two pushes.
	r.worker.ReviewGitHub.SetChecks("ccc333", failingGo)
	r.worker.ReviewGitHub.SetComments(12, service.PRComment{Kind: "issue", ID: 601, Author: "octocat", Association: "MEMBER", Body: "Is this the final name?"})
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	for range 3 {
		r.watchTick()
		r.worker.Clock.Advance(3 * time.Minute) // the quiet time
		r.tick()
	}
	r.worker.ReviewGitHub.Push(12, "ddd444")
	r.worker.Checkout.Change("ddd444", "greet.go", 3, 5)
	r.watchTick()
	r.tick()

	if typed := strings.TrimPrefix(r.worker.typedTo("legato-board-1"), typedBefore); strings.Contains(typed, "[legato") {
		t.Fatalf("a held steward sent the author %q", typed)
	}
	if got := len(r.worker.Tmux.Spawned()); got != spawns {
		t.Fatalf("a held steward started %d agents", got-spawns)
	}
	if loop := r.loopPhase(t, task); loop.Pass != 1 || loop.Phase != service.ReviewPhaseClean {
		t.Fatalf("a held steward moved the loop: %+v", loop)
	}
	if rec := r.record(t, view); rec.Reentries != 0 {
		t.Fatalf("re-entries %d while held", rec.Reentries)
	}
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 {
		t.Fatalf("a held steward posted %+v", posted)
	}

	// Watch again: the pushes made while held re-enter once.
	if err := r.stewardAct(t, view, "watch_again"); err != nil {
		t.Fatal(err)
	}
	r.watchTick()
	r.tick()
	rec := r.record(t, view)
	if rec.Held || rec.Reentries != 1 || rec.LastWake != "pushed" {
		t.Fatalf("held %v, re-entries %d, last wake %q", rec.Held, rec.Reentries, rec.LastWake)
	}
	if loop := r.loopPhase(t, task); loop.Pass != 2 {
		t.Fatalf("no pass after Watch again: %+v", loop)
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Still right."})
	r.tick()
	r.tick()
	if rec := r.record(t, view); rec.Reentries != 1 || rec.CleanPass != 2 {
		t.Fatalf("re-entries %d, clean pass %d", rec.Reentries, rec.CleanPass)
	}
}

func TestAHeldStewardStillEndsWhenAPullRequestCloses(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}

	r.worker.ReviewGitHub.ClosePR(12)
	r.watchTick()
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "closed" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
}

func TestEndOnAStewardGivesEnded(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	if err := r.stewardAct(t, view, "end"); err != nil {
		t.Fatal(err)
	}
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "ended" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
	if watches := stewardSteps(view, "watch"); len(watches) != 1 || stepOutputs(t, watches[0])["woke"] != "ended" {
		t.Fatalf("watch steps: %s", stepStates(view))
	}
}

func TestEndOnAStuckStewardGivesEnded(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	inputs := map[string]string{"max_reentries": "1"}
	for k, v := range stewardInputs {
		inputs[k] = v
	}
	view := r.stewarded(t, inputs)
	task := view.Run.TaskID
	r.reenterClean(t, task, "re1")
	r.worker.ReviewGitHub.Push(12, "re2")
	r.worker.Checkout.Change("re2", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Phase != service.ReviewPhaseStuck {
		t.Fatalf("not stuck past one re-entry: %+v", loop)
	}

	if err := r.stewardAct(t, view, "end"); err != nil {
		t.Fatal(err)
	}
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "ended" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
}

// End while a re-entry's pass waits for its author ends the loop too, and
// the steps waiting on it go on.
func TestEndOnAStewardDuringAReEntryGivesEnded(t *testing.T) {
	r, view := reenteredWithAFinding(t)
	if err := r.stewardAct(t, view, "end"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "ended" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
}

func TestStopWatchingDuringAReEntryIsRefused(t *testing.T) {
	r, view := reenteredWithAFinding(t)
	if err := r.stewardAct(t, view, "stop_watching"); err == nil {
		t.Fatal("Stop watching was taken while the author fixes a re-entry's findings")
	}
	if rec := r.record(t, view); rec.Held {
		t.Fatal("held during a re-entry")
	}
}

func TestSevenQuietDaysEndTheStewardIdle(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	// A comment on day six moves the deadline on.
	r.worker.Clock.Advance(6 * 24 * time.Hour)
	r.watchTick()
	r.worker.ReviewGitHub.SetComments(12, service.PRComment{Kind: "issue", ID: 701, Author: "octocat", Association: "MEMBER", Body: "Any news?"})
	r.watchTick()
	r.worker.Clock.Advance(2 * 24 * time.Hour)
	r.watchTick()
	if view := r.show(t, view.Run.ID); view.Run.Status != "running" {
		t.Fatalf("ended %s though a comment came within seven days: %s", view.Run.OutputsJSON, stepStates(view))
	}

	r.worker.Clock.Advance(6 * 24 * time.Hour)
	r.watchTick()
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "idle" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
	if watches := stewardSteps(view, "watch"); len(watches) != 1 || stepOutputs(t, watches[0])["woke"] != "idle" {
		t.Fatalf("watch steps: %s", stepStates(view))
	}
}

func TestAHeldStewardStillEndsIdle(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}

	r.worker.Clock.Advance(7*24*time.Hour + time.Minute)
	r.watchTick()
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "idle" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
}
