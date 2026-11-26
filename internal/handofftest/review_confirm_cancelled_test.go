package handofftest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// Invariant 9 across the upgrade: an older Legato's review-pr-passes run
// posted its clean pass on acme/widget#12 and was cancelled before its
// confirm read GitHub's heads. Upgraded, its loop is history that ended
// cancelled: nothing reports it approved, not the run page, not the step's
// outputs, not its review block, and no tick confirms it.

// olderLegatoCleanThenCancelled writes, at the schema before the
// conversion, what an older Legato left: the run cancelled, its loop clean
// at pass 1 with its confirm still to come (confirm_clean), and pass 1's
// APPROVE posted.
func olderLegatoCleanThenCancelled(t testing.TB, path string) (runID, task string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.NewBefore(path, "_review_loop_conversion.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	example := workflowengine.OldReviewPassesCopy()
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
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
		"branch": "feature/greeting", "clone_id": 1, "dir": dir, "linked_task": "", "scope": "stack", "confirm_clean": true,
	}); err != nil {
		t.Fatal(err)
	}
	at := "2026-11-05T08:50:00Z"
	if _, err := db.DB().ExecContext(ctx, `
		INSERT INTO handoff_reviews (task_id, settings, phase, pass, updated_at, delivered_pass) VALUES (?, '{}', 'clean', 1, ?, 1);
		INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, checkout, head_sha, started_at, ended_at, posting_started_at)
			VALUES (?, 1, 'posted', 'clean', 'Looks right.', ?, 'bbb222', ?, ?, ?);
		INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, url, head_sha, review_id, review_url, author, event)
			VALUES (?, 1, 'acme/widget', 12, 'feature/greeting', 'main', 'PR 12', 'https://github.com/acme/widget/pull/12', 'bbb222', 1,
				'https://github.com/review/1', 'legato-author[bot]', 'APPROVE');`,
		task, at, task, dir, at, at, at, task); err != nil {
		t.Fatal(err)
	}
	writeAsOlderLegato(t, db, func() { view, err = workflows.Cancel(ctx, runID, "not needed") })
	if err != nil || view.Run.Status != "cancelled" {
		t.Fatalf("cancel: %+v, %v", view, err)
	}
	return runID, task
}

func TestARunCancelledBetweenItsCleanPostAndConfirmIsNotApprovedAfterTheUpgrade(t *testing.T) {
	ctx := context.Background()
	var runID, task string
	worker := StartWorker(t, WorkerOptions{Name: "erebus", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &ReviewPassOptions{},
		Database: func(t testing.TB, path string) {
			runID, task = olderLegatoCleanThenCancelled(t, path)
		}})
	worker.ReviewGitHub.OpenPR(12, "feature/greeting", "bbb222")
	worker.Checkout.File("greet.go", 30)
	r := reviewRun{worker: worker, runs: StartReviewRuns(t, worker)}
	r.tick()

	loop, err := worker.Store.CurrentReviewLoop(ctx, task)
	if err != nil || loop == nil || loop.State != store.ReviewLoopClosedState || loop.Outcome == service.ReviewOutcomeApproved || loop.ConfirmedAt != "" {
		t.Fatalf("the converted loop = %+v, %v; want it closed, not approved, never confirmed", loop, err)
	}
	if posted := worker.ReviewGitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted = %+v; want nothing after the upgrade", posted)
	}

	// The run page.
	d := r.details(t, runID)
	if d.Phase != service.ReviewPhaseEnded || d.Summary != "Cancelled" {
		t.Fatalf("the run page = %+v; want it ended, cancelled", d)
	}
	// The step's outputs.
	view := r.show(t, runID)
	if view.Run.Status != "cancelled" {
		t.Fatalf("run %q", view.Run.Status)
	}
	for _, step := range view.Steps {
		if strings.Contains(step.OutputsJSON, service.ReviewOutcomeApproved) {
			t.Fatalf("step %s has outputs %s", step.StepID, step.OutputsJSON)
		}
	}
	// The review block.
	block := worker.Reviews.ReviewBlock(ctx, task)
	if block == nil || block.Phase != service.ReviewPhaseEnded || strings.Contains(block.Summary(), "pproved") {
		t.Fatalf("the review block = %+v (%q); want it ended, not approved", block, block.Summary())
	}
}
