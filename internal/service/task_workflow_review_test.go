package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// refusingReviewStep refuses every start as an active loop would.
type refusingReviewStep struct{ starts []ResolvedWorkflowStep }

func (r *refusingReviewStep) Type() string { return ReviewPassesStepType }
func (r *refusingReviewStep) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	r.starts = append(r.starts, step)
	return WorkflowExecutionResult{}, errors.New("pull request #12 of acme/widget already has an active review loop: run wr-1 (/workflows/runs/wr-1)")
}
func (r *refusingReviewStep) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func launcherWithReviewPasses(t *testing.T, prMeta *store.PRMeta) (*TaskWorkflowLauncher, *refusingReviewStep) {
	t.Helper()
	dir := t.TempDir()
	example, _ := workflowengine.ExampleNamed("review-pr-passes")
	if err := os.WriteFile(filepath.Join(dir, "review-pr-passes.workflow.yaml"), example.YAML, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Card", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if prMeta != nil {
		raw, _ := json.Marshal(prMeta)
		meta := string(raw)
		if err := db.UpdatePRMeta(context.Background(), "task-1", &meta); err != nil {
			t.Fatal(err)
		}
	}
	step := &refusingReviewStep{}
	workflows := NewWorkflowService(db, []WorkflowExecutor{step, NewTaskNoteExecutor(db)})
	return NewTaskWorkflowLauncher(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) }), step
}

func TestACardWithAPullRequestOffersReviewPassesWithItsAgentAsAuthor(t *testing.T) {
	launcher, step := launcherWithReviewPasses(t, &store.PRMeta{Repo: "acme/widget", Branch: "feature/greeting", PRNumber: 12, State: "OPEN"})
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Review passes · #12" || items[0].Values["repo"] != "acme/widget" || items[0].Values["pr_number"] != "12" || items[0].Values["agent_author"] != "true" {
		t.Fatalf("items = %#v", items)
	}
	// The loop already running on #12 is named instead of a new one.
	_, err = launcher.Start(context.Background(), items[0].Name, items[0].Values)
	if err == nil || err.Error() != "pull request #12 of acme/widget already has an active review loop: run wr-1 (/workflows/runs/wr-1)" {
		t.Fatalf("start = %v", err)
	}
	if len(step.starts) != 1 || step.starts[0].With["agent_author"] != true {
		t.Fatalf("starts = %+v", step.starts)
	}
}

func TestACardWithNoPullRequestOffersNoReviewPasses(t *testing.T) {
	launcher, _ := launcherWithReviewPasses(t, nil)
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %#v, %v", items, err)
	}
}

func TestACardWhosePullRequestsAreAStackOffersTheStackFirst(t *testing.T) {
	launcher, step := launcherWithReviewPasses(t, nil)
	ctx := context.Background()
	db := launcher.store
	var refs []store.BranchRef
	for _, b := range []string{"s1", "s2", "s3"} {
		refs = append(refs, store.BranchRef{Repo: "acme/widget", Branch: b})
	}
	if _, err := db.TrackTaskBranches(ctx, "task-1", refs, BranchSourceStack); err != nil {
		t.Fatal(err)
	}
	for i, b := range []string{"s1", "s2", "s3"} {
		if err := db.SetTaskBranchPR(ctx, "task-1", "acme/widget", b, &store.PRMeta{Repo: "acme/widget", Branch: b, PRNumber: 137 + i, State: "OPEN"}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := launcher.List(ctx, "task-1")
	if err != nil || len(items) != 4 {
		t.Fatalf("items = %#v, %v", items, err)
	}
	if items[0].Title != "Review passes · stack #137–#139" || items[0].Values["scope"] != "stack" || items[0].Values["pr_number"] != "137" || items[0].Values["agent_author"] != "true" {
		t.Fatalf("the stack item = %#v", items[0])
	}
	if items[2].Title != "Review passes · #138" || items[2].Values["scope"] != "pr" {
		t.Fatalf("a layer's item = %#v", items[2])
	}
	if _, err := launcher.Start(ctx, items[0].Name, items[0].Values); err == nil || len(step.starts) != 1 || step.starts[0].With["scope"] != "stack" {
		t.Fatalf("start = %v, %+v", err, step.starts)
	}
}
