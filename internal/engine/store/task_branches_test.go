package store

import (
	"context"
	"reflect"
	"testing"
)

func branchNames(t *testing.T, s *Store, taskID string) []string {
	t.Helper()
	rows, err := s.ListTaskBranches(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Branch)
	}
	return names
}

func refs(repo string, branches ...string) []BranchRef {
	out := make([]BranchRef, 0, len(branches))
	for _, b := range branches {
		out = append(out, BranchRef{Repo: repo, Branch: b})
	}
	return out
}

func TestTrackTaskBranchesKeepsStackOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")

	// The code context's branch comes first; a stack that does not hold it
	// goes after it, in stack order.
	added, err := s.TrackTaskBranches(ctx, "task-1", refs("acme/widget", "feature/start"), "code_context")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added, refs("acme/widget", "feature/start")) {
		t.Fatalf("added = %v", added)
	}
	if _, err := s.TrackTaskBranches(ctx, "task-1", refs("acme/widget", "legato/task-1-a", "legato/task-1-b"), "stack"); err != nil {
		t.Fatal(err)
	}
	if got, want := branchNames(t, s, "task-1"), []string{"feature/start", "legato/task-1-a", "legato/task-1-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}

	// The stack grows and is reordered: its branches stay together, where the
	// first of them was, in the new order; only the new one is added.
	added, err = s.TrackTaskBranches(ctx, "task-1", refs("acme/widget", "legato/task-1-b", "legato/task-1-a", "legato/task-1-c"), "stack")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added, refs("acme/widget", "legato/task-1-c")) {
		t.Fatalf("added = %v", added)
	}
	if got, want := branchNames(t, s, "task-1"), []string{"feature/start", "legato/task-1-b", "legato/task-1-a", "legato/task-1-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}

	// Adding what is tracked already changes nothing.
	added, err = s.TrackTaskBranches(ctx, "task-1", refs("acme/widget", "feature/start"), "api")
	if err != nil || len(added) != 0 {
		t.Fatalf("added = %v, %v", added, err)
	}
	rows, _ := s.ListTaskBranches(ctx, "task-1")
	if rows[0].Source != "code_context" || rows[3].Source != "stack" {
		t.Fatalf("sources = %q, %q", rows[0].Source, rows[3].Source)
	}
}

func TestTaskBranchPRIsKeptPerBranchAndCleared(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	createTestTask(t, s, "task-2")
	if _, err := s.TrackTaskBranches(ctx, "task-1", refs("acme/widget", "a", "b"), "stack"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrackTaskBranches(ctx, "task-2", refs("acme/widget", "c"), "api"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskBranchPR(ctx, "task-1", "acme/widget", "b", &PRMeta{PRNumber: 8, State: "OPEN", IsDraft: true}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListTrackedTaskBranches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all["task-1"]) != 2 || len(all["task-2"]) != 1 {
		t.Fatalf("tracked = %v", all)
	}
	if pr := all["task-1"][1].PR; pr == nil || pr.PRNumber != 8 || !pr.IsDraft {
		t.Fatalf("b's PR = %#v", pr)
	}
	if all["task-1"][0].PR != nil {
		t.Fatalf("a has a PR: %#v", all["task-1"][0].PR)
	}
	if err := s.SetTaskBranchPR(ctx, "task-1", "acme/widget", "missing", &PRMeta{PRNumber: 1}); err != ErrNotFound {
		t.Fatalf("PR of an untracked branch = %v, want ErrNotFound", err)
	}

	if err := s.RemoveTaskBranches(ctx, "task-1", "api"); err != nil {
		t.Fatal(err)
	}
	if got := branchNames(t, s, "task-1"); len(got) != 2 {
		t.Fatalf("removing another source's branches removed %v", got)
	}
	if err := s.RemoveTaskBranch(ctx, "task-1", "acme/widget", "a"); err != nil {
		t.Fatal(err)
	}
	if got := branchNames(t, s, "task-1"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("branches = %v", got)
	}
	if err := s.RemoveTaskBranches(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if got := branchNames(t, s, "task-1"); len(got) != 0 {
		t.Fatalf("branches = %v", got)
	}

	// A deleted task takes its branches with it.
	if err := s.DeleteTask(ctx, "task-2"); err != nil {
		t.Fatal(err)
	}
	if got := branchNames(t, s, "task-2"); len(got) != 0 {
		t.Fatalf("deleted task's branches = %v", got)
	}
}
