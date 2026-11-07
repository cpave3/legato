package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheAuthorsCheckoutIsReadOnTheHost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ctx := context.Background()
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=a", "-c", "user.email=a@b"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "legato/abc")
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("bin/\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "one")
	sha := git("rev-parse", "HEAD")
	checkout := GitReviewCheckout{}

	if tip, err := checkout.Tip(ctx, dir, "legato/abc"); err != nil || tip != sha {
		t.Fatalf("tip = %q, %v; want %s", tip, err, sha)
	}
	// Ignored output does not make the tree dirty; a change does.
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0o644)
	if head, clean, err := checkout.State(ctx, dir); err != nil || head != sha || !clean {
		t.Fatalf("state = %q, %v, %v", head, clean, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("changed\n"), 0o644)
	if _, clean, _ := checkout.State(ctx, dir); clean {
		t.Fatal("a changed file left the tree clean")
	}
	if n, ok, err := checkout.Lines(ctx, dir, sha, "main.go"); err != nil || !ok || n != 3 {
		t.Fatalf("lines = %d, %v, %v", n, ok, err)
	}
	if _, ok, err := checkout.Lines(ctx, dir, sha, "missing.go"); err != nil || ok {
		t.Fatalf("missing file = %v, %v", ok, err)
	}
	if err := checkout.Pin(ctx, dir, "refs/legato/review/abc/1/7", sha); err != nil {
		t.Fatal(err)
	}
	if got := git("rev-parse", "refs/legato/review/abc/1/7"); got != sha {
		t.Fatalf("pinned = %s", got)
	}
}
