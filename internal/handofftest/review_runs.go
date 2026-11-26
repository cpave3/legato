package handofftest

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cpave3/legato/config"
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
	// Steps are the review loop steps (github.pr.review.open and the
	// rest), run on the same caller.
	Steps  *service.ReviewLoopSteps
	Clones *FakeRunClones
	// Discover is the PR tracking a run asks to look again when no task
	// is linked to an agent-authored loop's pull request.
	Discover *FakeDiscover
	// Burnlite is the Burnlite burnlite.task.close closes tickets in.
	Burnlite *FakeBurnliteClose
}

// FakeBurnliteClose records each Burnlite ticket closed, as
// "<key>|<reason>|<summary>".
type FakeBurnliteClose struct {
	mu     sync.Mutex
	closed []string
}

func (f *FakeBurnliteClose) CloseTask(_ context.Context, key, reason, summary string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, key+"|"+reason+"|"+summary)
	return nil
}

// Closed are the tickets closed, oldest first.
func (f *FakeBurnliteClose) Closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

// FakeDiscover records each repository a run asked PR tracking to look in
// again, and runs OnDiscover, which may link a task as a real look would.
type FakeDiscover struct {
	mu         sync.Mutex
	calls      []string
	OnDiscover func(repo string)
}

func (f *FakeDiscover) DiscoverRepository(_ context.Context, repo string) error {
	f.mu.Lock()
	f.calls = append(f.calls, repo)
	hook := f.OnDiscover
	f.mu.Unlock()
	if hook != nil {
		hook(repo)
	}
	return nil
}

// Calls are the repositories looked in, in order.
func (f *FakeDiscover) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// StartReviewRuns is a worker's workflows and run caller, which a worker
// with review passes is started with, as legato serve wires them: its
// handoffs' review-loop runs run on them too (LEG-600).
func StartReviewRuns(t *testing.T, w *Worker) *ReviewRuns {
	t.Helper()
	if w.Reviews == nil || w.ReviewRuns == nil {
		t.Fatal("the worker has no review passes")
	}
	return w.ReviewRuns
}

// newReviewRuns wires a worker's workflows and the run caller on its
// review-pass engine.
func newReviewRuns(w *Worker) *ReviewRuns {
	clones := &FakeRunClones{repos: w.Repos, checkout: w.Checkout, removed: map[int64]bool{}}
	step := service.NewReviewPassesStep()
	steps := service.NewReviewLoopSteps()
	steps.SetMaxReentries(func() int { return 5 })
	// The steps ticket-to-merged-pr runs around review-loop: the card's
	// agent (agent.run, agent.stop, on the built-in macros), notes and a
	// fake Burnlite close.
	agents := service.NewWorkflowAgentRunner(w.Agents, w.Store)
	run := service.NewAgentRunExecutor(agents)
	run.SetMacros(service.NewMacroService(service.NewSettingsService(w.Store, func() (*config.Config, error) { return config.LoadBytes(nil) })))
	burnlite := &FakeBurnliteClose{}
	executors := append([]service.WorkflowExecutor{step, service.NewTaskNoteExecutor(w.Store), run, service.NewAgentStopExecutor(agents),
		service.NewBurnliteCloseExecutor(w.Store, burnlite)}, steps.Executors()...)
	workflows := service.NewWorkflowService(w.Store, executors)
	discover := &FakeDiscover{}
	caller := service.NewReviewRunCaller(w.Reviews, service.ReviewRunOptions{
		GitHub: w.ReviewGitHub, Clones: clones, Owners: service.NewOwnerLookupService(w.Store, w.Agents, ""), Discover: discover,
		SummaryDir: filepath.Join(w.Dir, "review-summaries"),
	})
	caller.SetWorkflows(workflows)
	step.Attach(caller)
	steps.Attach(caller)
	// workflow.run finds the built-ins, review-loop among them, and the
	// review-pr-passes example, as Legato's discovery does.
	workflows.SetDefinitions(func() workflowengine.DiscoveryResult {
		var found workflowengine.DiscoveryResult
		if example, ok := workflowengine.ExampleNamed("review-pr-passes"); ok {
			if definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML); err == nil {
				found.Definitions = append(found.Definitions, definition)
			}
		}
		return workflowengine.WithBuiltIns(found, service.BuiltInWorkflows()...)
	})
	return &ReviewRuns{w: w, Workflows: workflows, Caller: caller, Steps: steps, Clones: clones, Discover: discover, Burnlite: burnlite}
}

// Start runs review-pr-passes with inputs, and opens its review step as
// the broker holder's tick does at once (Open).
func (r *ReviewRuns) Start(ctx context.Context, inputs map[string]string) (*store.WorkflowRunView, error) {
	example, ok := workflowengine.ExampleNamed("review-pr-passes")
	if !ok {
		return nil, fmt.Errorf("no review-pr-passes example")
	}
	definition, err := workflowengine.Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		return nil, err
	}
	view, err := r.Workflows.Start(ctx, definition, inputs)
	if err != nil || view.Run.Status != "running" {
		return view, err
	}
	r.Open(ctx)
	return r.Workflows.Show(ctx, view.Run.ID)
}

// StartExample runs a bundled example workflow (legato workflow add's
// templates, ticket-to-merged-pr among them) with inputs, and opens its
// review steps as the broker holder's tick does at once.
func (r *ReviewRuns) StartExample(ctx context.Context, name string, inputs map[string]string) (*store.WorkflowRunView, error) {
	example, ok := workflowengine.ExampleNamed(name)
	if !ok {
		return nil, fmt.Errorf("no %s example", name)
	}
	return r.StartFile(ctx, string(example.YAML), inputs)
}

// StartReviewLoop runs the built-in review-loop with inputs, top level, and
// opens its open step as the broker holder's tick does at once.
func (r *ReviewRuns) StartReviewLoop(ctx context.Context, inputs map[string]string) (*store.WorkflowRunView, error) {
	view, err := r.Workflows.Start(ctx, workflowengine.BuiltInReviewLoop(), inputs)
	if err != nil || view.Run.Status != "running" {
		return view, err
	}
	r.Open(ctx)
	return r.Workflows.Show(ctx, view.Run.ID)
}

// StartFile runs a workflow file with inputs, and opens its review steps as
// the broker holder's tick does at once (Open).
func (r *ReviewRuns) StartFile(ctx context.Context, yaml string, inputs map[string]string) (*store.WorkflowRunView, error) {
	definition, err := workflowengine.Parse("steps.workflow.yaml", []byte(yaml))
	if err != nil {
		return nil, err
	}
	view, err := r.Workflows.Start(ctx, definition, inputs)
	if err != nil || view.Run.Status != "running" {
		return view, err
	}
	r.Open(ctx)
	return r.Workflows.Show(ctx, view.Run.ID)
}

// Open opens the review steps waiting to open, as the first part of the
// engine's tick does, without moving any loop on.
func (r *ReviewRuns) Open(ctx context.Context) {
	r.Caller.OpenWaiting(ctx)
	r.w.Settle()
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
