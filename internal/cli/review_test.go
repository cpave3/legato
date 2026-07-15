package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newReviewCLIFixture(t *testing.T) (*service.ReviewService, string) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	git("checkout", "-b", "feature")

	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "CLI test", Status: "Doing",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{
		PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "add a\n\nbecause reasons")

	return service.NewReviewService(s, nil, nil), repo
}

func TestReviewShowHumanReadable(t *testing.T) {
	svc, _ := newReviewCLIFixture(t)

	if err := ReviewReady(svc, "task-1", "all done"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := ReviewShow(svc, "task-1", false, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"task-1 — ready", "all done", "[ ] 1. rs-", "add a", "because reasons"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestReviewShowJSON(t *testing.T) {
	svc, _ := newReviewCLIFixture(t)

	var out strings.Builder
	if err := ReviewShow(svc, "task-1", true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"steps"`) || !strings.Contains(out.String(), `"add a"`) {
		t.Fatalf("json output:\n%s", out.String())
	}
}

func TestReviewAnnotateReturnsStepID(t *testing.T) {
	svc, _ := newReviewCLIFixture(t)

	stepID, err := ReviewAnnotate(svc, "task-1", service.AnnotateArgs{Text: "careful here", Risk: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stepID, "rs-") {
		t.Fatalf("stepID = %q", stepID)
	}
}
