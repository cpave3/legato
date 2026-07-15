package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a git repo in a temp dir with an initial commit on branch
// "main" and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-b", "main")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "README.md", "hello\n")
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-m", "initial commit")
	return dir
}

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, message string) string {
	t.Helper()
	runGitT(t, dir, "add", "-A")
	runGitT(t, dir, "commit", "-m", message)
	return runGitT(t, dir, "rev-parse", "HEAD")
}

func TestMergeBaseFindsForkPoint(t *testing.T) {
	dir := initRepo(t)
	fork := runGitT(t, dir, "rev-parse", "HEAD")
	runGitT(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "a.go", "package a\n")
	commit(t, dir, "feature work")
	// main moves on after the branch point.
	runGitT(t, dir, "checkout", "main")
	writeFile(t, dir, "main.go", "package main\n")
	commit(t, dir, "main work")
	runGitT(t, dir, "checkout", "feature")

	got, err := MergeBase(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != fork {
		t.Fatalf("MergeBase = %q, want fork point %q", got, fork)
	}
}

func TestShowCommitReturnsPatchOnly(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	sha := commit(t, dir, "add package a\n\nbody prose that must not leak into the patch")

	patch, err := ShowCommit(context.Background(), dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "diff --git a/a.go b/a.go") {
		t.Fatalf("patch missing file header:\n%s", patch)
	}
	if !strings.Contains(patch, "+package a") {
		t.Fatalf("patch missing added line:\n%s", patch)
	}
	if strings.Contains(patch, "body prose") {
		t.Fatalf("patch contains commit message:\n%s", patch)
	}
}

func TestDiffWorktreeIncludesTrackedAndUntrackedChanges(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "README.md", "hello\nchanged\n")
	writeFile(t, dir, "new.go", "package new\n")

	diff, err := DiffWorktree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+changed") {
		t.Fatalf("diff missing tracked modification:\n%s", diff)
	}
	if !strings.Contains(diff, "+package new") {
		t.Fatalf("diff missing untracked file content:\n%s", diff)
	}
	if !strings.Contains(diff, "b/new.go") {
		t.Fatalf("diff missing untracked file header:\n%s", diff)
	}
}

func TestDiffWorktreeCleanReturnsEmpty(t *testing.T) {
	dir := initRepo(t)
	diff, err := DiffWorktree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("diff = %q, want empty", diff)
	}
}

func TestStatusPorcelainReturnsMachineReadableWorktreeState(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "README.md", "changed\n")
	writeFile(t, dir, "new.go", "package new\n")

	status, err := StatusPorcelain(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, " M README.md") || !strings.Contains(status, "?? new.go") {
		t.Fatalf("status = %q, want tracked and untracked entries", status)
	}
}

func TestDiffFilesScopesToRequestedPaths(t *testing.T) {
	dir := initRepo(t)
	base := runGitT(t, dir, "rev-parse", "HEAD")
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "b.go", "package b\n")
	commit(t, dir, "add a and b")

	diff, err := DiffFiles(context.Background(), dir, base, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+package a") {
		t.Fatalf("diff missing a.go:\n%s", diff)
	}
	if strings.Contains(diff, "package b") {
		t.Fatalf("diff leaked out-of-scope file:\n%s", diff)
	}
}

func TestCommitExists(t *testing.T) {
	dir := initRepo(t)
	sha := runGitT(t, dir, "rev-parse", "HEAD")

	if !CommitExists(context.Background(), dir, sha) {
		t.Fatalf("CommitExists(%q) = false, want true", sha)
	}
	if CommitExists(context.Background(), dir, "0000000000000000000000000000000000000000") {
		t.Fatal("CommitExists(zero sha) = true, want false")
	}
}

func TestNonRepoDirSurfacesGitError(t *testing.T) {
	_, err := MergeBase(context.Background(), t.TempDir(), "main")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want git stderr surfaced", err)
	}
}

func TestCommitsAheadReturnsCommitsInTopologicalOrder(t *testing.T) {
	dir := initRepo(t)
	base := runGitT(t, dir, "rev-parse", "HEAD")
	runGitT(t, dir, "checkout", "-b", "feature")

	writeFile(t, dir, "a.go", "package a\n")
	first := commit(t, dir, "add package a\n\nThis lays the groundwork for the a feature.")
	writeFile(t, dir, "b.go", "package b\n")
	second := commit(t, dir, "add package b")

	commits, err := CommitsAhead(context.Background(), dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(commits))
	}
	if commits[0].SHA != first || commits[1].SHA != second {
		t.Fatalf("order = %q, %q; want oldest first (%q, %q)", commits[0].SHA, commits[1].SHA, first, second)
	}
	if commits[0].Subject != "add package a" {
		t.Fatalf("Subject = %q, want %q", commits[0].Subject, "add package a")
	}
	if commits[0].Body != "This lays the groundwork for the a feature." {
		t.Fatalf("Body = %q", commits[0].Body)
	}
	if commits[1].Body != "" {
		t.Fatalf("second Body = %q, want empty", commits[1].Body)
	}
}
