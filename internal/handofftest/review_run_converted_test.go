package handofftest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// A Legato from before the loop record kept a run's review loop in
// handoff_review* and the run task's legato.review_loop metadata. Upgraded
// mid-review, the conversion copies it onto the loop record, and the loop
// finishes there as it would have: the author's push starts pass 2, under
// the same markers and pin refs, and a clean pass approves and completes
// the run.

// convertedRun is the run an older Legato left with its pass 1 posted (one
// blocking finding) and its loop waiting for the branch's owner; its run
// task.
type convertedRun struct {
	reviewRun
	runID, task string
}

func newConvertedRun(t *testing.T) convertedRun {
	t.Helper()
	var runID, task string
	worker := StartWorker(t, WorkerOptions{Name: "erebus", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &ReviewPassOptions{},
		Database: func(t testing.TB, path string) {
			runID, task = olderLegatoMidReview(t, path)
		}})
	worker.ReviewGitHub.OpenPR(12, "feature/greeting", "bbb222")
	worker.Checkout.File("greet.go", 30)
	r := reviewRun{worker: worker, runs: StartReviewRuns(t, worker)}
	return convertedRun{reviewRun: r, runID: runID, task: task}
}

// olderLegatoMidReview writes, at the schema before the conversion, what an
// older Legato wrote for a review-pr-passes run on acme/widget#12 after
// pass 1: the run waiting on its review step, the loop in handoff_review*,
// and the run's record in its task's metadata.
func olderLegatoMidReview(t testing.TB, path string) (runID, task string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.NewBefore(path, "_review_loop_conversion.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	example, ok := workflowengine.ExampleNamed("review-pr-passes")
	if !ok {
		t.Fatal("no review-pr-passes example")
	}
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	// A process without the review engine starts the step: it waits.
	workflows := service.NewWorkflowService(db, []service.WorkflowExecutor{service.NewReviewPassesStep(), service.NewTaskNoteExecutor(db)})
	var view *store.WorkflowRunView
	writeAsOlderLegato(t, db, func() { view, err = workflows.Start(ctx, definition, pr12) })
	if err != nil || view.Run.Status != "running" {
		t.Fatalf("start: %+v, %v", view, err)
	}
	runID, task = view.Run.ID, view.Run.TaskID
	attempt := latestAttempt(t, view, "review")
	dir := filepath.Join(filepath.Dir(path), "jobs", attempt.ID, "widget")
	if err := db.SetTaskMetadata(ctx, task, []string{"legato", "review_loop"}, map[string]any{
		"run_id": runID, "step_id": attempt.Path, "attempt_id": attempt.ID, "repo": "acme/widget", "pr": 12,
		"branch": "feature/greeting", "clone_id": 1, "dir": dir, "linked_task": "", "scope": "stack",
	}); err != nil {
		t.Fatal(err)
	}
	at := "2026-11-05T08:50:00Z"
	if _, err := db.DB().ExecContext(ctx, `
		INSERT INTO handoff_reviews (task_id, settings, phase, pass, updated_at, delivered_pass) VALUES (?, '{}', 'waiting_for_author', 1, ?, 1);
		INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, checkout, head_sha, started_at, ended_at, posting_started_at)
			VALUES (?, 1, 'posted', 'changes', 'One problem.', ?, 'bbb222', ?, ?, ?);
		INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, url, head_sha, review_id, review_url, author, event)
			VALUES (?, 1, 'acme/widget', 12, 'feature/greeting', 'main', 'PR 12', 'https://github.com/acme/widget/pull/12', 'bbb222', 1,
				'https://github.com/review/1', 'legato-author[bot]', 'COMMENT');
		INSERT INTO handoff_review_findings (id, task_id, pass, pr, severity, title, body, path, line, side, comment_id, status)
			VALUES ('f-1', ?, 1, 12, 'blocking', 'Greets nobody', 'An empty name prints a bare comma.', 'greet.go', 4, 'RIGHT', 101, 'open');
		INSERT INTO handoff_review_items (id, task_id, kind, pass, repo, pr, path, line, version, title, body, thread_comment_id, created_at, delivered)
			VALUES ('f-1', ?, 'blocking', 1, 'acme/widget', 12, 'greet.go', 4, 1, 'Greets nobody', 'An empty name prints a bare comma.', 101, ?, 1);`,
		task, at, task, dir, at, at, at, task, task, task, at); err != nil {
		t.Fatal(err)
	}
	return runID, task
}

func TestARunLoopConvertedMidReviewFinishesOnTheLoopRecord(t *testing.T) {
	r := newConvertedRun(t)
	ctx := context.Background()

	loop, err := r.worker.Store.CurrentReviewLoop(ctx, r.task)
	if err != nil || loop == nil || loop.State != store.ReviewLoopOpenState || loop.MarkerKey != r.task || loop.RunID != r.runID {
		t.Fatalf("converted loop = %+v, %v", loop, err)
	}
	if got := r.loopPhase(t, r.task); got.Phase != service.ReviewPhaseWaitingForAuthor || got.Pass != 1 {
		t.Fatalf("converted loop = %+v; want pass 1 waiting for its author", got)
	}
	r.tick()
	if r.reviewer(t, r.task) || len(r.worker.ReviewGitHub.Posted()) != 0 {
		t.Fatal("the converted loop reviewed again with nothing from its author")
	}

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if got := r.loopPhase(t, r.task); !r.reviewer(t, r.task) || got.Pass != 2 || got.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("no pass 2 after the push: %+v", got)
	}
	// The old task ID keys the pin refs and markers still.
	if r.worker.Checkout.Pins()["refs/legato/review/"+r.task+"/2/12"] != "ccc333" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}
	if brief := r.lastBrief(t); !strings.Contains(brief, "Greets nobody") {
		t.Fatalf("pass 2's brief does not name the converted finding:\n%s", brief)
	}

	r.submit(t, r.task, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: "f-1", Status: "resolved", Reason: "guarded"}}})
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewApprove || posted[0].CommitID != "ccc333" || posted[0].Marker != r.task+"/pass-2/pr-12" {
		t.Fatalf("posted = %+v", posted)
	}
	view := r.show(t, r.runID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	out := outputs(t, view, "review")
	if out["outcome"] != "approved" || out["passes"] != float64(2) || out["findings"] != float64(1) {
		t.Fatalf("outputs = %+v", out)
	}
	if reviews, _ := out["reviews"].([]any); len(reviews) != 2 {
		t.Fatalf("reviews = %+v; want pass 1's, from before the upgrade, and pass 2's", out["reviews"])
	}
	findings, _ := r.worker.Store.ListHandoffReviewFindings(ctx, r.task)
	if len(findings) != 1 || findings[0].Status != "resolved" || findings[0].RuledPass != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	loop, _ = r.worker.Store.CurrentReviewLoop(ctx, r.task)
	if loop.State != store.ReviewLoopClosedState || loop.Outcome != service.ReviewOutcomeApproved {
		t.Fatalf("loop = %+v; want it closed approved", loop)
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("#12 is still claimed by %s", held.ID)
	}
	if !r.runs.Clones.Removed(1) {
		t.Fatal("the converted loop's clone was not removed")
	}
}

// A run cancelled in a process without the review engine never reaches the
// run caller's Cancel: the holder's next tick closes its loop record, so
// the pull request it claimed is free for another loop.
func TestARunCancelledWithoutTheEngineFreesItsPullRequest(t *testing.T) {
	r, view := afterPassOne(t, ReviewPassOptions{})
	ctx := context.Background()
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held == nil {
		t.Fatal("the run's loop holds no claim on #12")
	}
	if _, err := otherProcess(r.worker).Cancel(ctx, view.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if held, _ := r.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("#12 is still claimed by %s after its run was cancelled", held.ID)
	}
	loop, _ := r.worker.Store.CurrentReviewLoop(ctx, view.Run.TaskID)
	if loop == nil || loop.State != store.ReviewLoopClosedState || loop.Outcome != service.ReviewOutcomeCancelled || loop.Phase != service.ReviewPhaseEnded {
		t.Fatalf("loop = %+v; want it closed cancelled", loop)
	}
	again := r.start(t, pr12)
	if again.Run.Status != "running" {
		t.Fatalf("another run on #12: %q: %s", again.Run.Status, again.Run.Error)
	}
}
