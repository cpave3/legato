package handofftest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// sweepGit is the git the real repository cache runs for a sweep: every
// fetch works and no clone holds unpushed commits, so the sweep removes
// whatever it does not keep on purpose.
type sweepGit struct{ service.RepoGit }

func (sweepGit) Fetch(context.Context, string, []byte, func(gitrepo.Progress)) error { return nil }
func (sweepGit) UnpushedTips(context.Context, string, string) ([]string, error)      { return nil, nil }

type sweepTokens struct{}

func (sweepTokens) GitReadToken(context.Context, string) ([]byte, string, error) {
	return []byte("t"), "https://github.com/acme/widget.git", nil
}
func (sweepTokens) PushesThroughProxy(string) bool { return true }
func (sweepTokens) CanRead(string) error           { return nil }

// sweeper is the worker's real job-clone sweep over its store, with its
// cache of acme/widget already made.
func sweeper(t *testing.T, w *Worker) *service.RepositoryService {
	t.Helper()
	repos := service.NewRepositoryService(service.RepositoryServiceOptions{
		DataDir: w.Dir, Store: w.Store, Git: sweepGit{}, Tokens: sweepTokens{},
		KeepIdle: time.Hour, Now: w.Clock.Now,
	})
	cache := repos.CachePath("acme/widget")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repos
}

// A loop the review-loop's open step handed to its run keeps its clones
// while it is open, whichever step of the run waits; once the run ends,
// they are no longer kept.
func TestAnOpenStepsLoopKeepsItsClones(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view, err := r.runs.StartReviewLoop(ctx, map[string]string{"repo": "acme/widget", "pr_number": "12"})
	if err != nil {
		t.Fatal(err)
	}
	r.tick()
	loops, err := r.worker.Store.ListOpenReviewLoops(ctx)
	if err != nil || len(loops) != 1 || loops[0].RunID != view.Run.ID {
		t.Fatalf("open loops %+v (%v)", loops, err)
	}
	host := loops[0].HostTask
	clone, err := r.worker.Store.CreateJobClone(ctx, store.JobClone{ExecutionID: "exec-host", TaskID: host, Repo: "Acme/Widget",
		Branch: "legato-review/pr-12", Path: filepath.Join(r.worker.Dir, "jobs", "exec-host", "widget"), CreatedAt: r.worker.Clock.Now(), Slot: service.SlotReviewer})
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.worker.Store.CreateJobClone(ctx, store.JobClone{ExecutionID: "exec-other", TaskID: host, Repo: "acme/other",
		Branch: "main", Path: filepath.Join(r.worker.Dir, "jobs", "exec-other", "other"), CreatedAt: r.worker.Clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := service.OpenReviewLoopClones(ctx, r.worker.Store)
	if err != nil {
		t.Fatal(err)
	}
	if !kept[clone] || kept[other] {
		t.Fatalf("kept %v; want %d and not %d", kept, clone, other)
	}

	if _, err := r.runs.Workflows.Cancel(ctx, view.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if kept, err := service.OpenReviewLoopClones(ctx, r.worker.Store); err != nil || kept[clone] {
		t.Fatalf("after the run ended, kept %v (%v)", kept, err)
	}
}

// AC 46: a loop waiting days for its author keeps its reviewer's clone past
// keep_idle; once the loop ends, the sweep may take it.
func TestTheJobCloneSweepKeepsAnOpenLoopsClone(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{
		{PR: 12, Severity: "blocking", Title: "Greets nobody", Body: "An empty name prints a bare comma.", FailureScenario: "greet(\"\") prints \"Hello, \".",
			Place: &service.ReviewPlace{Path: "greet.go", Line: 4}},
	}})
	if loop, _ := r.worker.Store.GetHandoffReview(ctx, task); loop == nil || loop.Phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("loop = %+v", loop)
	}
	// The cache records the clone it made for the run, as the real one does.
	req := r.reviewClone(t, view)
	path := filepath.Join(r.worker.Dir, "jobs", req.ExecutionID, "widget")
	if _, err := r.worker.Store.CreateJobClone(ctx, store.JobClone{ExecutionID: req.ExecutionID, TaskID: task, Repo: "acme/widget",
		Branch: req.FreshBranch, Path: path, CreatedAt: r.worker.Clock.Now(), Slot: service.SlotReviewer}); err != nil {
		t.Fatal(err)
	}
	repos := sweeper(t, r.worker)

	r.worker.Clock.Advance(10 * 24 * time.Hour)
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the sweep removed the clone of a loop waiting for its author: %v", err)
	}

	if _, err := r.runs.Workflows.Cancel(ctx, view.Run.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the clone of an ended loop is still there")
	}
}
