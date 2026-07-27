package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

type fakePRFetcher struct{ status *github.PRStatus }

func (f fakePRFetcher) FetchPRByNumber(owner, repo string, number int) (*github.PRStatus, error) {
	return f.status, nil
}

type fakeExactFetcher struct{ dir, repo, sha, ref string }

func (f *fakeExactFetcher) Fetch(_ context.Context, dir, repo, sha, ref string) error {
	f.dir, f.repo, f.sha, f.ref = dir, repo, sha, ref
	return nil
}

type fakePRWorktrees struct{ task, dir, branch, base string }

func (f *fakePRWorktrees) Create(_ context.Context, task, dir, branch, base string) (*store.TaskWorktree, error) {
	f.task, f.dir, f.branch, f.base = task, dir, branch, base
	return &store.TaskWorktree{PrimaryDir: dir, Path: dir + "/.worktrees/" + branch, Branch: branch, BaseBranch: base}, nil
}
func (f *fakePRWorktrees) Remove(context.Context, string) error { return nil }

type fakePRLinker struct {
	task, owner, repo string
	number            int
}

func (f *fakePRLinker) LinkPR(_ context.Context, task, owner, repo string, number int) error {
	f.task, f.owner, f.repo, f.number = task, owner, repo, number
	return nil
}

func TestGitHubPRPrepareExecutorFetchesExactForkHeadBeforeWorktree(t *testing.T) {
	exact := &fakeExactFetcher{}
	worktrees := &fakePRWorktrees{}
	links := &fakePRLinker{}
	executor := NewGitHubPRPrepareExecutor(fakePRFetcher{&github.PRStatus{Number: 42, Title: "PR", URL: "url", HeadBranch: "feature", HeadSHA: "abc123", HeadRepo: "fork/repo"}}, exact, worktrees, links)
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", TaskID: "task-1", StepID: "prepare", With: map[string]any{"repo": "owner/repo", "number": int64(42), "primary_dir": "/repo", "base_branch": "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if exact.repo != "fork/repo" || exact.sha != "abc123" || worktrees.branch == "feature" {
		t.Fatalf("exact=%+v worktree=%+v", exact, worktrees)
	}
	if result.Outputs["worktree_path"] == "" || result.Outputs["head_sha"] != "abc123" || links.number != 42 {
		t.Fatalf("result=%+v links=%+v", result, links)
	}
}
