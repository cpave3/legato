package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBlobExcerptPinsCommitAndRejectsUnsafeObjects(t *testing.T) {
	repo := handoffRepo(t)
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("source.go", "one\ntwo\nthree\n")
	runHandoffGit(t, repo, "add", ".")
	runHandoffGit(t, repo, "commit", "-m", "source")
	sha := runHandoffGit(t, repo, "rev-parse", "HEAD")
	write("source.go", "changed\n")

	got, err := ReadBlobExcerpt(context.Background(), repo, sha, "source.go", 2, 3, 1024)
	if err != nil || got != "two\nthree" {
		t.Fatalf("excerpt = %q, err = %v", got, err)
	}
	for _, name := range []string{"../source.go", "/source.go", "a\\b"} {
		if _, err := ReadBlobExcerpt(context.Background(), repo, sha, name, 1, 1, 1024); err == nil {
			t.Fatalf("unsafe path %q accepted", name)
		}
	}

	if err := os.Symlink("source.go", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	runHandoffGit(t, repo, "add", "link")
	runHandoffGit(t, repo, "commit", "-m", "link")
	linkSHA := runHandoffGit(t, repo, "rev-parse", "HEAD")
	if _, err := ReadBlobExcerpt(context.Background(), repo, linkSHA, "link", 1, 1, 1024); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestChangedFilesAndAncestorUseExactCommits(t *testing.T) {
	repo := handoffRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHandoffGit(t, repo, "add", ".")
	runHandoffGit(t, repo, "commit", "-m", "base")
	base := runHandoffGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "space name.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHandoffGit(t, repo, "add", ".")
	runHandoffGit(t, repo, "commit", "-m", "head")
	head := runHandoffGit(t, repo, "rev-parse", "HEAD")
	files, err := ChangedFiles(context.Background(), repo, base, head)
	if err != nil || len(files) != 1 || files[0] != "space name.txt" {
		t.Fatalf("files = %#v, err = %v", files, err)
	}
	ancestor, err := IsAncestor(context.Background(), repo, base, head)
	if err != nil || !ancestor {
		t.Fatalf("ancestor = %v, err = %v", ancestor, err)
	}
}

func TestReadBlobExcerptUsesLiteralPathsAndBoundsTheExcerpt(t *testing.T) {
	repo := handoffRepo(t)
	for name, text := range map[string]string{"large.go": strings.Repeat("a line\n", 15000), "literal*": "literal\n", "literal-other": "not selected\n", "binary": "a\x00b\n", "invalid": string([]byte{0xff, '\n'})} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runHandoffGit(t, repo, "add", ".")
	runHandoffGit(t, repo, "commit", "-m", "sources")
	sha := runHandoffGit(t, repo, "rev-parse", "HEAD")
	for _, test := range []struct{ path, want string }{{"large.go", "a line"}, {"literal*", "literal"}} {
		got, err := ReadBlobExcerpt(context.Background(), repo, sha, test.path, 1, 1, 32)
		if err != nil || got != test.want {
			t.Fatalf("%s=%q err=%v", test.path, got, err)
		}
	}
	for _, name := range []string{"binary", "invalid"} {
		if _, err := ReadBlobExcerpt(context.Background(), repo, sha, name, 1, 1, 32); err == nil {
			t.Fatalf("nontext source accepted: %s", name)
		}
	}
	if _, err := ReadBlobExcerpt(context.Background(), repo, sha, "large.go", 1, 20, 32); err == nil {
		t.Fatal("oversized excerpt accepted")
	}
	runHandoffGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+sha+",module")
	runHandoffGit(t, repo, "commit", "-m", "submodule")
	sha = runHandoffGit(t, repo, "rev-parse", "HEAD")
	if _, err := ReadBlobExcerpt(context.Background(), repo, sha, "module", 1, 1, 32); err == nil {
		t.Fatal("submodule accepted")
	}
}

func handoffRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runHandoffGit(t, dir, "init", "-b", "main")
	runHandoffGit(t, dir, "config", "user.email", "test@example.com")
	runHandoffGit(t, dir, "config", "user.name", "Test")
	return dir
}

func runHandoffGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
