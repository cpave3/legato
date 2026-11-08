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

	// What changed between two commits: the lines of the later one, by
	// file. A deletion marks the lines around it; a deleted file has none.
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"os\"\n\nfunc main() { os.Exit(0) }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0o644)
	_ = os.RemoveAll(filepath.Join(dir, "bin"))
	_ = os.Remove(filepath.Join(dir, ".gitignore"))
	git("add", "-A")
	git("commit", "-q", "-m", "two")
	later := git("rev-parse", "HEAD")
	changed, err := checkout.Changed(ctx, dir, sha, later)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][][2]int{"main.go": {{3, 5}}, "new.go": {{1, 1}}}
	if len(changed) != len(want) || len(changed["main.go"]) != 1 || changed["main.go"][0] != want["main.go"][0] || len(changed["new.go"]) != 1 || changed["new.go"][0] != want["new.go"][0] {
		t.Fatalf("changed = %v; want %v", changed, want)
	}
}
