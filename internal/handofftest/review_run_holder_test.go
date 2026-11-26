package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// The review engine runs in the broker holder only (spec LEG-559 D9). These
// tests start review steps in another process on the same store: a second
// workflow service, with or without an engine of its own that never ticks,
// as a TUI beside `legato serve` has.

// startReviewIn starts review-pr-passes on workflows, as another process
// would, without opening it.
func startReviewIn(t *testing.T, workflows *service.WorkflowService, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	example := workflowengine.OldReviewPassesCopy()
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(context.Background(), definition, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// otherProcess is a second workflow service on the worker's store whose
// review step has no engine.
func otherProcess(w *Worker) *service.WorkflowService {
	return service.NewWorkflowService(w.Store, []service.WorkflowExecutor{service.NewReviewPassesStep(), service.NewTaskNoteExecutor(w.Store)})
}

func waitingStep(t *testing.T, view *store.WorkflowRunView) {
	t.Helper()
	if view.Run.Status != "running" || view.Steps[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("run %q, step %q: %s %s", view.Run.Status, view.Steps[0].Status, view.Run.Error, view.Steps[0].Error)
	}
}

func TestAReviewStartedWhereThereIsNoEngineWaitsAndTheHoldersTickRunsItsPass(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	other := otherProcess(r.worker)

	view := startReviewIn(t, other, pr12)
	waitingStep(t, view)
	task := view.Run.TaskID
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer"); err == nil {
		t.Fatal("a reviewer started before the holder ticked")
	}

	r.tick()
	reviewer, err := r.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer")
	if err != nil || reviewer.TmuxSession != "legato-"+task+"_reviewer" {
		t.Fatalf("reviewer = %+v, %v", reviewer, err)
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+task+"/1/12"] != "bbb222" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "Looks right."})
	if got := r.show(t, view.Run.ID); got.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", got.Run.Status, got.Run.Error)
	}
}

func TestAProcessWithoutTheEngineNeverFailsAWaitingReviewStep(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	other := otherProcess(r.worker)
	view := startReviewIn(t, other, pr12)

	for i := 0; i < 3; i++ {
		if _, err := other.AdvanceDue(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := other.Show(ctx, view.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		waitingStep(t, got)
		if len(got.Attempts) != 1 {
			t.Fatalf("attempts = %+v", got.Attempts)
		}
	}
	r.tick()
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, view.Run.TaskID, "reviewer"); err != nil {
		t.Fatalf("the holder did not pick the step up: %v", err)
	}
}

// A process that has the engine but does not hold the broker (the TUI while
// `legato serve` holds it) only writes the step: the holder's tick opens it,
// with the holder's GitHub, and runs its pass.
func TestAReviewStartedFromTheTUIWhileServeHoldsTheBrokerRunsItsPass(t *testing.T) {
	serve := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	tuiEngine := service.NewReviewPassService(serve.worker.Store, service.ReviewPassOptions{
		Agents: serve.worker.Agents.(service.ReviewAgents), GitHub: &FakeReviewGitHub{}, Checkout: serve.worker.Checkout,
		Now: serve.worker.Clock.Now, Run: serve.worker.run, Overlay: func() bool { return false },
		Config: func() service.ReviewPassConfig { return service.ReviewPassConfig{Enabled: true, MaxConcurrent: 2} },
	})
	tuiGitHub := &FakeReviewGitHub{}
	tuiStep := service.NewReviewPassesStep()
	tui := service.NewWorkflowService(serve.worker.Store, []service.WorkflowExecutor{tuiStep, service.NewTaskNoteExecutor(serve.worker.Store)})
	tuiCaller := service.NewReviewRunCaller(tuiEngine, service.ReviewRunOptions{GitHub: tuiGitHub, Clones: serve.runs.Clones,
		Owners: service.NewOwnerLookupService(serve.worker.Store, serve.worker.Agents, "")})
	tuiCaller.SetWorkflows(tui)
	tuiStep.Attach(tuiCaller)

	view := startReviewIn(t, tui, pr12)
	waitingStep(t, view)
	if len(serve.runs.Clones.heads) != 0 {
		t.Fatalf("the TUI fetched heads %v: only the holder's tick does GitHub work", serve.runs.Clones.heads)
	}

	serve.tick()
	task := view.Run.TaskID
	if _, err := serve.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer"); err != nil {
		t.Fatalf("no reviewer after the holder's tick: %v", err)
	}
	serve.submit(t, task, service.ReviewSubmission{Summary: "Fine."})
	posted := serve.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewApprove || posted[0].Number != 12 {
		t.Fatalf("posted = %+v", posted)
	}
	if got := serve.show(t, view.Run.ID); got.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", got.Run.Status, got.Run.Error)
	}
}

// When the holder changes (the TUI exits, serve takes the broker), the new
// holder's tick picks up a step nobody opened.
func TestANewHolderPicksUpAReviewStepNoProcessOpened(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := startReviewIn(t, otherProcess(r.worker), pr12)
	// The first holder exited without ticking; this engine holds now.
	r.tick()
	if _, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), view.Run.TaskID, "reviewer"); err != nil {
		t.Fatalf("no reviewer: %v", err)
	}
}

// A refusal the holder finds when it opens the step fails it there, with the
// same words a start gave.
func TestAStepTheHolderCannotOpenFailsAtItsTick(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.ClosePR(12)
	view := startReviewIn(t, otherProcess(r.worker), pr12)
	waitingStep(t, view)
	r.tick()
	got := r.show(t, view.Run.ID)
	if got.Run.Status != "failed" || !strings.Contains(got.Steps[0].Error, "is closed: a review loop needs an open pull request") {
		t.Fatalf("run %q, step error %q", got.Run.Status, got.Steps[0].Error)
	}
}

// Two starts on one pull request before the holder ticks: the second is
// refused at once where the process has the engine, naming the first.
func TestASecondStartOnAPullRequestWaitingToOpenIsRefused(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	first := startReviewIn(t, otherProcess(r.worker), pr12)
	second := startReviewIn(t, r.runs.Workflows, pr12)
	refused(t, second, "already has an active review loop: run "+first.Run.ID)
}

// holderExitsAfterTasks makes every write after a task's own fail, as a
// holder that exits there leaves the store: the loop is never written, nor
// the step's failure. The returned func is the restart.
func holderExitsAfterTasks(t *testing.T, w *Worker) (restart func()) {
	t.Helper()
	db := w.Store.DB()
	var names []string
	for _, table := range []string{"review_loops", "workflow_step_attempts", "workflow_steps", "workflow_runs"} {
		for _, op := range []string{"INSERT", "UPDATE"} {
			name := "holder_gone_" + strings.ToLower(op) + "_" + table
			if _, err := db.Exec(`CREATE TRIGGER ` + name + ` BEFORE ` + op + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT, 'the holder exited'); END`); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
	}
	return func() {
		for _, name := range names {
			if _, err := db.Exec(`DROP TRIGGER ` + name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// A holder that exits while it opens a step leaves the step waiting, and
// the next holder opens it and runs its pass: the step is never recorded as
// opened without its loop.
func TestAHolderThatExitsWhileOpeningLeavesTheStepForTheNextHolder(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := startReviewIn(t, otherProcess(r.worker), pr12)
	restart := holderExitsAfterTasks(t, r.worker)
	r.runs.Open(ctx)
	restart()
	waitingStep(t, r.show(t, view.Run.ID))

	r.tick()
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, view.Run.TaskID, "reviewer"); err != nil {
		t.Fatalf("the next holder ran no pass: %v", err)
	}
}
