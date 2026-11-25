package handofftest

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The owning workflow run's end closes its review loop (spec LEG-559 B3,
// D1; LEG-581): a "run ended" hook in the workflow service, one
// subscription for every terminal status, with a sweep at startup and on
// each wake for runs that ended where no subscriber heard of it.

// endListener is a subscriber to the run-ended hook, as LEG-489's parent
// reconcile will be: it holds the runs it waits on until it is told each
// one ended.
type endListener struct {
	mu   sync.Mutex
	held []string
	ends []service.WorkflowRunEnd
}

func (l *endListener) hold(runID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held = append(l.held, runID)
}

func (l *endListener) RunEnded(_ context.Context, end service.WorkflowRunEnd) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ends = append(l.ends, end)
	l.held = slices.DeleteFunc(l.held, func(id string) bool { return id == end.RunID })
}

func (l *endListener) RunsHeld(context.Context) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.held), nil
}

// told is the status each run's end was told with, "" for none.
func (l *endListener) told(runID string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, end := range l.ends {
		if end.RunID == runID {
			out = append(out, end.Status)
		}
	}
	return out
}

func TestTheRunEndedHookTellsASubscriberOfEveryTerminalStatus(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	for n := 13; n <= 16; n++ {
		r.worker.ReviewGitHub.OpenPR(n, "feature/"+string(rune('a'+n-13)), "ccc333")
	}
	listener := &endListener{}
	r.runs.Workflows.OnRunEnded(listener)

	// succeeded: a clean pass approves and completes the run.
	succeeded := r.start(t, pr12)
	listener.hold(succeeded.Run.ID)
	r.tick()
	r.submit(t, succeeded.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	if got := r.show(t, succeeded.Run.ID).Run.Status; got != "succeeded" {
		t.Fatalf("run %q; want succeeded", got)
	}

	// failed: the operator fails its review step.
	failed := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "13"})
	listener.hold(failed.Run.ID)
	attempt := latestAttempt(t, r.show(t, failed.Run.ID), "review")
	if _, err := r.runs.Workflows.Fail(ctx, failed.Run.ID, "review", attempt.ID, "the operator failed it"); err != nil {
		t.Fatal(err)
	}

	// cancelled and stopped.
	cancelled := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "14"})
	listener.hold(cancelled.Run.ID)
	if _, err := r.runs.Workflows.Cancel(ctx, cancelled.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	stopped := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "15"})
	listener.hold(stopped.Run.ID)
	view, err := r.runs.Workflows.Stop(ctx, stopped.Run.ID, "the gate said stop")
	if err != nil || view.Run.Status != service.WorkflowRunStopped {
		t.Fatalf("stop = %+v, %v; want the run stopped", view, err)
	}

	// Each end in this process is told at once, with its status.
	for _, c := range []struct{ runID, status string }{
		{succeeded.Run.ID, "succeeded"}, {failed.Run.ID, "failed"}, {cancelled.Run.ID, "cancelled"}, {stopped.Run.ID, "stopped"},
	} {
		if got := listener.told(c.runID); !slices.Equal(got, []string{c.status}) {
			t.Errorf("run %s was told %v; want [%s]", c.runID, got, c.status)
		}
	}

	// An end in another process (a TUI without the engine, the CLI) is
	// told at this process's next sweep, for a run the subscriber holds.
	elsewhere := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "16"})
	listener.hold(elsewhere.Run.ID)
	if _, err := otherProcess(r.worker).Cancel(ctx, elsewhere.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	if got := listener.told(elsewhere.Run.ID); len(got) != 0 {
		t.Fatalf("told %v before a sweep", got)
	}
	r.runs.Workflows.SweepEndedRuns(ctx)
	if got := listener.told(elsewhere.Run.ID); !slices.Equal(got, []string{"cancelled"}) {
		t.Fatalf("the run cancelled elsewhere was told %v; want [cancelled]", got)
	}
	r.runs.Workflows.SweepEndedRuns(ctx)
	if got := listener.told(elsewhere.Run.ID); len(got) != 1 {
		t.Fatalf("a run the subscriber no longer holds was told again: %v", got)
	}
}

// closedAndFreed checks that the run's loop is closed, its pull request
// free for another loop, its reviewer stopped and its clone removed.
func (r reviewRun) closedAndFreed(t *testing.T, view *store.WorkflowRunView, loopID string, cloneID int64) {
	t.Helper()
	ctx := context.Background()
	loop, err := r.worker.Store.GetReviewLoop(ctx, loopID)
	if err != nil || loop.State != store.ReviewLoopClosedState {
		t.Fatalf("loop = %+v, %v; want it closed", loop, err)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("#12 is still claimed by %s", held.ID)
	}
	if r.reviewer(t, view.Run.TaskID) {
		t.Fatal("the reviewer still runs")
	}
	if !r.runs.Clones.Removed(cloneID) {
		t.Fatalf("clone %d was not removed: %v", cloneID, r.runs.Clones.removed)
	}
}

// runLoop is the open loop a run's task hosts, and its clone.
func (r reviewRun) runLoop(t *testing.T, view *store.WorkflowRunView) (string, int64) {
	t.Helper()
	loop, err := r.worker.Store.CurrentReviewLoop(context.Background(), view.Run.TaskID)
	if err != nil || loop == nil || loop.State != store.ReviewLoopOpenState || loop.CloneID == 0 {
		t.Fatalf("loop = %+v, %v; want it open with a clone", loop, err)
	}
	return loop.ID, loop.CloneID
}

// AC 8: a Cancel or a Stop while a pass is submitted posts no review, and
// the loop closes: its claims are released, its reviewer stopped and its
// clone removed, wherever the run was ended.
func TestEndingARunWhileAPassIsSubmittedClosesItsLoopAndFreesWhatItHeld(t *testing.T) {
	elsewhere := func(end func(*service.WorkflowService, string) error) func(t *testing.T, r reviewRun, runID string) func() {
		return func(t *testing.T, r reviewRun, runID string) func() {
			r.worker.ReviewGitHub.OnRead(0, func() {
				if err := end(otherProcess(r.worker), runID); err != nil {
					t.Errorf("ending the run: %v", err)
				}
			})
			return func() {}
		}
	}
	cancel := func(w *service.WorkflowService, runID string) error {
		_, err := w.Cancel(context.Background(), runID, "not needed")
		return err
	}
	stop := func(w *service.WorkflowService, runID string) error {
		_, err := w.Stop(context.Background(), runID, "the gate said stop")
		return err
	}
	for name, c := range map[string]struct {
		status string
		end    func(t *testing.T, r reviewRun, runID string) (wait func())
	}{
		"a cancel in a process without the engine": {"cancelled", elsewhere(cancel)},
		"a stop in a process without the engine":   {service.WorkflowRunStopped, elsewhere(stop)},
		// A Stop in the holder lands just before the review would be
		// posted, while the tick holds the engine.
		"a stop in the holder, mid-post": {service.WorkflowRunStopped, func(t *testing.T, r reviewRun, runID string) func() {
			done := make(chan struct{})
			r.worker.ReviewGitHub.OnRead(readsBeforeAPost, func() {
				go func() {
					defer close(done)
					if err := stop(r.runs.Workflows, runID); err != nil {
						t.Errorf("stop: %v", err)
					}
				}()
				waitFor(t, "the stop to close the loop", func() bool {
					view, _ := r.worker.Store.GetWorkflowRun(context.Background(), runID)
					loop, _ := r.worker.Store.CurrentReviewLoop(context.Background(), view.Run.TaskID)
					return loop != nil && loop.State == store.ReviewLoopClosedState
				})
			})
			return func() {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("the stop never returned")
				}
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{})
			view := r.start(t, pr12)
			r.tick()
			loopID, cloneID := r.runLoop(t, view)
			submittedPass(t, r, view.Run.TaskID)

			wait := c.end(t, r, view.Run.ID)
			r.tick()
			wait()
			r.runs.Workflows.SweepEndedRuns(context.Background())
			r.tick()

			if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 0 {
				t.Fatalf("posted %+v after the run ended", posted)
			}
			if got := r.show(t, view.Run.ID); got.Run.Status != c.status {
				t.Fatalf("run %q; want %s", got.Run.Status, c.status)
			}
			r.closedAndFreed(t, view, loopID, cloneID)
		})
	}
}

// Story 39: a failed run keeps its loop open and its pull request claimed,
// through the run-ended hook's sweep and the engine's ticks, and Retry
// carries the same loop on.
func TestAFailedRunKeepsItsLoopAndClaimsAndRetryCarriesItOn(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	loopID, _ := r.runLoop(t, view)
	attempt := latestAttempt(t, r.show(t, view.Run.ID), "review")
	if _, err := r.runs.Workflows.Fail(ctx, view.Run.ID, "review", attempt.ID, "the operator failed it"); err != nil {
		t.Fatal(err)
	}
	r.runs.Workflows.SweepEndedRuns(ctx)
	r.tick()
	r.runs.Workflows.SweepEndedRuns(ctx)

	if loop, err := r.worker.Store.GetReviewLoop(ctx, loopID); err != nil || loop.State != store.ReviewLoopOpenState {
		t.Fatalf("a failed run's loop = %+v, %v; want it kept open", loop, err)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held == nil || held.ID != loopID {
		t.Fatalf("#12 is claimed by %+v; want the failed run's loop %s", held, loopID)
	}
	other := r.start(t, pr12)
	refused(t, other, "already has an active review loop")

	r.retry(t, view.Run.ID)
	if got, _ := r.worker.Store.CurrentReviewLoop(ctx, view.Run.TaskID); got == nil || got.ID != loopID {
		t.Fatalf("the retried run's loop = %+v; want %s", got, loopID)
	}
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	findings, _ := r.worker.Store.ListHandoffReviewFindings(ctx, view.Run.TaskID)
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: findings[0].ID, Status: "resolved", Reason: "guarded"}}})
	if got := r.show(t, view.Run.ID); got.Run.Status != "succeeded" || outputs(t, got, "review")["passes"] != float64(2) {
		t.Fatalf("run %q: %s", got.Run.Status, got.Run.Error)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("#12 is still claimed by %s after the run succeeded", held.ID)
	}
}

// A run cancelled while Legato was down (the CLI on its database) has its
// loop closed when Legato starts again, by the run-ended hook's sweep at
// the start of the advance loop, before the review engine first ticks.
func TestARunThatEndedWhileLegatoWasDownHasItsLoopClosedAtStartup(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	loopID, cloneID := r.runLoop(t, view)

	r.worker.Stop()
	db, err := store.New(r.worker.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	cli := service.NewWorkflowService(db, []service.WorkflowExecutor{service.NewReviewPassesStep(), service.NewTaskNoteExecutor(db)})
	if _, err := cli.Cancel(ctx, view.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	r.worker.Restart()
	r = reviewRun{worker: r.worker, runs: StartReviewRuns(t, r.worker)}
	if loop, err := r.worker.Store.GetReviewLoop(ctx, loopID); err != nil || loop.State != store.ReviewLoopOpenState {
		t.Fatalf("loop = %+v, %v; want it still open before startup's sweep", loop, err)
	}
	stop := r.runs.Workflows.StartAdvanceLoop(ctx, r.worker.Bus)
	defer stop()

	r.closedAndFreed(t, view, loopID, cloneID)
	if loop, _ := r.worker.Store.GetReviewLoop(ctx, loopID); loop.Outcome != service.ReviewOutcomeCancelled {
		t.Fatalf("loop outcome %q; want cancelled", loop.Outcome)
	}
}
