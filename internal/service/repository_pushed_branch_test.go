package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// pushBranch pushes a new branch with a commit of its own to the fake
// GitHub, as a person pushes work they began, and returns its commit.
func (g repoUpstream) pushBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	work := t.TempDir()
	gitIn(t, work, "clone", "-q", g.url(repo), ".")
	gitIn(t, work, "checkout", "-q", "-b", branch)
	gitIn(t, work, "commit", "-q", "--allow-empty", "-m", "begun")
	gitIn(t, work, "push", "-q", "origin", branch)
	return gitIn(t, work, "rev-parse", "HEAD")
}

func TestAPushedBranchIsLookedForAfterAFetch(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if _, err := f.svc.Ensure(ctx, "acme/widget"); err != nil {
		t.Fatal(err)
	}
	// Pushed a minute after the cache was fetched: Ensure would not fetch.
	gh.pushBranch(t, "acme/widget", "legato/k7q2-begun")

	base, err := f.svc.PushedBranch(ctx, "acme/widget", "legato/k7q2-begun")
	if err != nil {
		t.Fatalf("PushedBranch: %v", err)
	}
	if base != "main" {
		t.Fatalf("base = %q, want the repository's default branch", base)
	}
	if f.git.fetches.Load() != 1 {
		t.Fatalf("%d fetches, want the cache fetched before looking", f.git.fetches.Load())
	}
}

func TestABranchNotOnGitHubIsSaidSo(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)

	_, err := f.svc.PushedBranch(context.Background(), "acme/widget", "legato/never-pushed")
	if !errors.Is(err, ErrBranchNotOnGitHub) {
		t.Fatalf("err = %v, want ErrBranchNotOnGitHub", err)
	}
}

func TestAJobCloneOfAPushedBranchChecksOutGitHubsBranch(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	pushed := gh.pushBranch(t, "acme/widget", "legato/k7q2-begun")
	if _, err := f.svc.PushedBranch(ctx, "acme/widget", "legato/k7q2-begun"); err != nil {
		t.Fatal(err)
	}

	clone, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget",
		Branch: "legato/k7q2-begun", PushedBranch: true})
	if err != nil {
		t.Fatalf("JobClone: %v", err)
	}
	if clone.Branch != "legato/k7q2-begun" || gitIn(t, clone.Path, "rev-parse", "HEAD") != pushed {
		t.Fatalf("the clone is on %q at %s, want GitHub's legato/k7q2-begun at %s", clone.Branch, gitIn(t, clone.Path, "rev-parse", "HEAD"), pushed)
	}
	if upstream := gitIn(t, clone.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); upstream != "origin/legato/k7q2-begun" {
		t.Fatalf("the branch tracks %q, want GitHub's", upstream)
	}
	task, _ := f.store.GetTask(ctx, "task1")
	cc, _ := store.ParseCodeContext(task.CodeContext)
	if cc == nil || cc.Branch != "legato/k7q2-begun" || cc.BaseBranch != "main" {
		t.Fatalf("code context = %+v, want the pushed branch on main", cc)
	}
}

func TestAJobCloneOfAPushedBranchThatIsGoneMakesNoBranch(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget",
		Branch: "legato/never-pushed", PushedBranch: true})
	if !errors.Is(err, ErrBranchNotOnGitHub) {
		t.Fatalf("err = %v, want ErrBranchNotOnGitHub", err)
	}
	if _, statErr := os.Stat(filepath.Join(f.data, "jobs", "ex-abc", "widget")); !os.IsNotExist(statErr) {
		t.Fatalf("a clone was made: %v", statErr)
	}
	if clones, _ := f.svc.ListJobClones(ctx); len(clones) != 0 {
		t.Fatalf("job clones recorded: %+v", clones)
	}
}
