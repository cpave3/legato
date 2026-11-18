package handofftest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// ReviewRuns is a worker's workflows with review loops on any pull request:
// the review-pr-passes example, run on the worker's review-pass engine as the
// run caller, against its fake GitHub and checkout.
type ReviewRuns struct {
	w         *Worker
	Workflows *service.WorkflowService
	Caller    *service.ReviewRunCaller
	Clones    *FakeRunClones
}

// StartReviewRuns gives a worker with review passes workflows and the run
// caller, as legato serve wires them.
func StartReviewRuns(t *testing.T, w *Worker) *ReviewRuns {
	t.Helper()
	if w.Reviews == nil {
		t.Fatal("the worker has no review passes")
	}
	clones := &FakeRunClones{repos: w.Repos, checkout: w.Checkout, removed: map[int64]bool{}}
	step := service.NewReviewPassesStep()
	workflows := service.NewWorkflowService(w.Store, []service.WorkflowExecutor{step, service.NewTaskNoteExecutor(w.Store)})
	caller := service.NewReviewRunCaller(w.Reviews, service.ReviewRunOptions{
		GitHub: w.ReviewGitHub, Clones: clones, Owners: service.NewOwnerLookupService(w.Store, w.Agents, ""),
		SummaryDir: filepath.Join(w.Dir, "review-summaries"),
	})
	caller.SetWorkflows(workflows)
	step.Attach(caller)
	return &ReviewRuns{w: w, Workflows: workflows, Caller: caller, Clones: clones}
}

// Start runs review-pr-passes with inputs.
func (r *ReviewRuns) Start(ctx context.Context, inputs map[string]string) (*store.WorkflowRunView, error) {
	example, ok := workflowengine.ExampleNamed("review-pr-passes")
	if !ok {
		return nil, fmt.Errorf("no review-pr-passes example")
	}
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		return nil, err
	}
	return r.Workflows.Start(ctx, definition, inputs)
}

// FakeRunClones are a run's review clones: the fake repository cache's job
// clones, reset by setting the fake checkout.
type FakeRunClones struct {
	repos    *FakeRepos
	checkout *FakeCheckout
	removed  map[int64]bool
	heads    []int
}

func (f *FakeRunClones) PullHead(_ context.Context, _ string, number int) error {
	f.heads = append(f.heads, number)
	return nil
}

func (f *FakeRunClones) JobClone(ctx context.Context, req service.JobCloneRequest) (store.JobClone, error) {
	return f.repos.JobClone(ctx, req)
}

func (f *FakeRunClones) RemoveJobClone(_ context.Context, id int64) error {
	f.removed[id] = true
	return nil
}

func (f *FakeRunClones) Reset(_ context.Context, dir, branch, sha string) error {
	f.checkout.Set(dir, branch, sha)
	return nil
}

// Removed says whether the clone was removed.
func (f *FakeRunClones) Removed(id int64) bool { return f.removed[id] }
