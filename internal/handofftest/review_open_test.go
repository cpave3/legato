package handofftest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// The engine's Open and Close (spec LEG-559 § Loop record schema and
// claims, LEG-580): a loop is opened with its claims in one transaction,
// once per owner, and a refusal names the loop in the way.

// Two starts on one pull request at the same moment, in a process without
// the engine: the holder's tick opens one loop, and the other step fails
// with review_loop_active naming that loop. The opened one reviews.
func TestTwoStartsOnOnePullRequestAtOnceGiveOneLoopAndOneRefusalNamingIt(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	other := otherProcess(r.worker)
	example := workflowengine.OldReviewPassesCopy()
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view, err := other.Start(ctx, definition, pr12)
			if err == nil {
				ids[i] = view.Run.ID
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	r.tick()

	loop, err := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12)
	if err != nil || loop == nil {
		t.Fatalf("no loop holds #12: %+v, %v", loop, err)
	}
	var opened, refusedRun *store.WorkflowRunView
	for _, id := range ids {
		view := r.show(t, id)
		if id == loop.RunID {
			opened = view
		} else {
			refusedRun = view
		}
	}
	if opened == nil || refusedRun == nil {
		t.Fatalf("loop %s of run %s, runs %v", loop.ID, loop.RunID, ids)
	}
	refused(t, refusedRun, "review_loop_active")
	refused(t, refusedRun, loop.ID)
	refused(t, refusedRun, "run "+loop.RunID)
	waitingStep(t, opened)
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, opened.Run.TaskID, "reviewer"); err != nil {
		t.Fatalf("the opened loop has no reviewer: %v", err)
	}
	if phase := loop.Phase; phase == service.ReviewPhaseStuck {
		t.Fatalf("the opened loop is stuck: %s", loop.Reason)
	}
}

// A handoff's loop claims the pull requests its pass pins: a start on one
// is refused with review_loop_active, naming the handoff's loop.
func TestAStartOnAPullRequestAHandoffHoldsIsRefusedNamingItsLoop(t *testing.T) {
	h := newReviewed(t, ReviewPassOptions{})
	h.finish(t) // pass 1 of the handoff's loop pins #7
	ctx := context.Background()
	held, err := h.worker.Store.ActiveReviewLoop(ctx, "ACME/widget", 7)
	// The handoff's loop: since LEG-600 its review-loop run owns it, under
	// the handoff binding.
	if err != nil || held == nil || held.HostTask != h.card || held.HandoffID == "" {
		t.Fatalf("the claim on #7: %+v, %v", held, err)
	}
	runs := StartReviewRuns(t, h.worker)
	view, err := runs.Start(ctx, map[string]string{"repo": "acme/widget", "pr_number": "7"})
	if err != nil {
		t.Fatal(err)
	}
	refused(t, view, "review_loop_active")
	refused(t, view, held.ID)
	refused(t, view, "handed-off task "+h.card)
}

// Open is idempotent per owner: a run's step, or a handoff. The same owner
// gets its loop back; another handoff of the same card is another owner
// and gets a loop of its own.
func TestOpeningALoopAgainForTheSameOwnerReturnsTheSameLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	open := func(l store.ReviewLoop, prs ...int) (*store.ReviewLoop, bool, error) {
		req := store.ReviewLoopOpen{Loop: l}
		for _, n := range prs {
			req.PRs = append(req.PRs, store.ReviewLoopPR{Number: n})
		}
		return r.worker.Reviews.Open(ctx, req)
	}
	hosts(t, r.worker, "run-a", "card-1")
	run := store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-a", StepPath: "review", HostTask: "run-a", Repo: "acme/widget"}
	first, created, err := open(run, 12)
	if err != nil || !created {
		t.Fatalf("first open: %+v, %v, %v", first, created, err)
	}
	again, created, err := open(run, 12)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second open: %+v, %v, %v; want %s", again, created, err, first.ID)
	}

	// Another step of the same run is refused: a run has one open loop.
	other := run
	other.StepPath = "review-again"
	_, _, err = open(other, 13)
	if !strings.Contains(fmtErr(err), first.ID) {
		t.Fatalf("a second loop of the run: %v", err)
	}

	card := store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerHandoff, MainInstanceID: "main-1", HandoffID: "h-1", HostTask: "card-1", Repo: "acme/widget"}
	h1, created, err := open(card)
	if err != nil || !created {
		t.Fatalf("handoff open: %+v, %v, %v", h1, created, err)
	}
	if again, created, err := open(card); err != nil || created || again.ID != h1.ID {
		t.Fatalf("handoff open again: %+v, %v, %v", again, created, err)
	}
	if _, err := r.worker.Reviews.Close(ctx, h1.ID, "returned"); err != nil {
		t.Fatal(err)
	}
	second := card
	second.HandoffID = "h-2"
	h2, created, err := open(second)
	if err != nil || !created || h2.ID == h1.ID {
		t.Fatalf("the card's second handoff: %+v, %v, %v", h2, created, err)
	}
}

// A refusal is typed: its code and the loop in the way.
func TestARefusedOpenIsTypedWithItsCodeAndTheOtherLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	hosts(t, r.worker, "run-a", "run-b", "run-c")
	first, _, err := r.worker.Reviews.Open(ctx, store.ReviewLoopOpen{
		Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-a", StepPath: "review", HostTask: "run-a", Repo: "acme/widget",
			AgentAuthor: true, AuthorTask: "board-1"},
		PRs: []store.ReviewLoopPR{{Number: 12}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = r.worker.Reviews.Open(ctx, store.ReviewLoopOpen{
		Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-b", StepPath: "review", HostTask: "run-b", Repo: "Acme/Widget"},
		PRs:  []store.ReviewLoopPR{{Number: 12}}})
	var active *service.ReviewLoopActiveError
	if !errors.As(err, &active) || active.Code() != "review_loop_active" || active.LoopID != first.ID || active.Link != "/workflows/runs/wr-a" {
		t.Fatalf("err = %#v", err)
	}
	_, _, err = r.worker.Reviews.Open(ctx, store.ReviewLoopOpen{
		Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-c", StepPath: "review", HostTask: "run-c", Repo: "acme/widget",
			AgentAuthor: true, AuthorTask: "board-1"},
		PRs: []store.ReviewLoopPR{{Number: 13}}})
	var author *service.AuthorLoopActiveError
	if !errors.As(err, &author) || author.Code() != "author_loop_active" || author.LoopID != first.ID || service.ReviewRefusalCode(err) != "author_loop_active" {
		t.Fatalf("err = %#v", err)
	}
}

// hosts makes the tasks loops are hosted by.
func hosts(t *testing.T, w *Worker, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := w.Store.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "Doing",
			CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
