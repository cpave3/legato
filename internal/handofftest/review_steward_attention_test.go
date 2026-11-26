package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// The steward's Attention items (spec LEG-559 § The steward › Surfaces,
// § Approval after re-entry; LEG-593): stuck, an author that cannot be
// resumed, a person's dismissal of the approval without a push, held, and
// the idle end, each saying whether legato-reviews' approval still stands.

// oneItem is the only Attention item the workflows producer raises.
func (r reviewRun) oneItem(t *testing.T) service.AttentionInput {
	t.Helper()
	items := r.attention(t)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	return items[0]
}

// act takes an Attention item's action, as the TUI and the web UI do.
func (r reviewRun) act(t *testing.T, item service.AttentionInput, action string) {
	t.Helper()
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(context.Background(),
		service.AttentionActionRequest{SourceKey: item.SourceKey, ActionID: action, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
}

func actionNamed(item service.AttentionInput, id string) service.AttentionAction {
	for _, action := range item.Actions {
		if action.ID == id {
			return action
		}
	}
	return service.AttentionAction{}
}

func TestAStewardStuckAtItsReEntryBudgetNeedsYouAndSaysItsApprovalStands(t *testing.T) {
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

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewStuck || item.Navigation.Target != view.Run.ID {
		t.Fatalf("item = %+v", item)
	}
	for _, want := range []string{"re-entr", "legato-reviews' approval at re1 still stands"} {
		if !strings.Contains(item.Reason, want) {
			t.Fatalf("reason %q says no %q", item.Reason, want)
		}
	}
	if got := actionIDs(item); got != "open,watch_again,message,end" {
		t.Fatalf("actions = %s", got)
	}
	end := actionNamed(item, "end")
	if !end.RequiresConfirm || strings.Contains(end.Confirmation, "as stuck") || !strings.Contains(end.Confirmation, "steward") {
		t.Fatalf("End's confirmation %q", end.Confirmation)
	}

	// Watch again allows one more re-entry: the push re-enters, and the item
	// goes.
	r.act(t, item, "watch_again")
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Pass != 3 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("after Watch again: loop %+v", loop)
	}
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("items after Watch again: %+v", items)
	}
}

func TestAHeldStewardSaysItsApprovalStandsAndOffersWatchAgain(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("a watching steward raised %+v", items)
	}
	if err := r.stewardAct(t, view, "stop_watching"); err != nil {
		t.Fatal(err)
	}

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewSteward || !strings.Contains(item.Reason, "legato-reviews' approval at bbb222 still stands") ||
		!strings.Contains(item.Reason, "held") {
		t.Fatalf("item = %+v", item)
	}
	if got := actionIDs(item); got != "open,watch_again,end" {
		t.Fatalf("actions = %s", got)
	}
	r.act(t, item, "watch_again")
	if rec := r.record(t, view); rec.Held {
		t.Fatal("Watch again did not resume the steward")
	}
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("items after Watch again: %+v", items)
	}
}

func TestAPersonDismissingTheApprovalWithoutAPushNeedsYouAndGetsNoReApproval(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	posted := len(r.worker.ReviewGitHub.Posted())

	if id := r.worker.ReviewGitHub.DismissReview(12); id == 0 {
		t.Fatal("no review to dismiss")
	}
	for range 3 {
		r.watchTick()
		r.worker.Clock.Advance(3 * time.Minute)
		r.tick()
	}

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewSteward || item.Navigation.Target != view.Run.ID {
		t.Fatalf("item = %+v", item)
	}
	for _, want := range []string{"dismissed", "#12", "bbb222", "without a push"} {
		if !strings.Contains(item.Reason, want) {
			t.Fatalf("reason %q says no %q", item.Reason, want)
		}
	}
	if strings.Contains(item.Reason, "still stands") {
		t.Fatalf("a dismissed approval said to stand: %q", item.Reason)
	}
	if got := len(r.worker.ReviewGitHub.Posted()); got != posted {
		t.Fatalf("%d reviews posted after the dismissal: %+v", got-posted, r.worker.ReviewGitHub.Posted()[posted:])
	}
	if rec := r.record(t, view); rec.Pass != 1 || rec.Reentries != 0 {
		t.Fatalf("the dismissal moved the loop: pass %d, re-entries %d", rec.Pass, rec.Reentries)
	}
	if !r.watching(t, r.show(t, view.Run.ID)) {
		t.Fatal("the steward stopped watching")
	}
}

// A push dismisses the approval too (a base that dismisses stale reviews):
// that is a re-entry, which the next clean pass answers with a fresh
// APPROVE, and no item.
func TestADismissalByAPushIsAReEntryNotAnItem(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	r.worker.ReviewGitHub.DismissReview(12)
	r.reenterClean(t, view.Run.TaskID, "ccc333")
	r.watchTick()
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
}

func TestAStewardWhoseAuthorCannotBeResumedNamesWhatWaits(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	_ = r.worker.Tmux.Kill("legato-board-1")

	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	r.watchTick()
	r.worker.Clock.Advance(3 * time.Minute) // the quiet time
	r.tick()

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewStuck || item.Navigation.Target != view.Run.ID {
		t.Fatalf("item = %+v", item)
	}
	for _, want := range []string{"cannot be resumed", "1 failing check", "legato-reviews' approval at bbb222 still stands"} {
		if !strings.Contains(item.Reason, want) {
			t.Fatalf("reason %q says no %q", item.Reason, want)
		}
	}
	if got := actionIDs(item); got != "open,watch_again,end" {
		t.Fatalf("actions = %s", got)
	}
}

func TestAnAuthorThatCannotBeResumedNamesWhatWaitsForIt(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	_ = r.worker.Tmux.Kill("legato-board-1")
	agentPassOne(t, r)

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewStuck {
		t.Fatalf("item = %+v", item)
	}
	for _, want := range []string{"cannot be resumed", "1 blocking finding"} {
		if !strings.Contains(item.Reason, want) {
			t.Fatalf("reason %q says no %q", item.Reason, want)
		}
	}
	if got := actionIDs(item); got != "open,another_pass,end" {
		t.Fatalf("actions = %s", got)
	}
}

func TestAStewardThatEndsIdleLeavesANotice(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.Clock.Advance(7*24*time.Hour + time.Minute)
	r.watchTick()
	r.tick()
	if got := r.show(t, view.Run.ID); got.Run.Status != "succeeded" || runOutputs(t, got)["outcome"] != "idle" {
		t.Fatalf("run %q, outputs %s", got.Run.Status, got.Run.OutputsJSON)
	}

	item := r.oneItem(t)
	if item.SignalKind != service.AttentionSignalWorkflowReviewSteward || item.Navigation.Target != view.Run.ID ||
		!strings.Contains(item.Reason, "seven days") || !strings.Contains(item.Title, "#12") {
		t.Fatalf("item = %+v", item)
	}
	if got := actionIDs(item); got != "open" {
		t.Fatalf("actions = %s", got)
	}

	// A notice, not a wait: it goes by itself a week on.
	r.worker.Clock.Advance(8 * 24 * time.Hour)
	if items := r.attention(t); len(items) != 0 {
		t.Fatalf("items a week on: %+v", items)
	}
}
