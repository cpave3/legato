package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestReviewDiscardRemovesSelectedTour(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "rewrite")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	configureReviewCLITest(t, dbPath)
	if code := runReviewCmd([]string{"discard", "--task", "task-1", "--name", "rewrite"}); code != 0 {
		t.Fatalf("runReviewCmd() = %d, want 0", code)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetReviewTour(ctx, tour.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetReviewTour() error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetTask(ctx, "task-1"); err != nil {
		t.Fatalf("discard removed task: %v", err)
	}
}

func TestReviewRestartReplacesPacketAndPreservesCaptureBoundary(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateReviewTour(ctx, tour.ID, func(rt *store.ReviewTour) {
		rt.BaseSHA = "base-before-rewrite"
		rt.RepositoryPath = "/repo"
		rt.Status = "ready"
		rt.Summary = "stale review"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewStep(ctx, store.ReviewStep{
		ID: "stale-step", TaskID: "task-1", TourID: tour.ID, Kind: "file", Title: "Stale", Files: "[]",
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	configureReviewCLITest(t, dbPath)
	if code := runReviewCmd([]string{"restart", "--task", "task-1"}); code != 0 {
		t.Fatalf("runReviewCmd() = %d, want 0", code)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restarted, err := s.GetReviewTour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Status != "capturing" || restarted.Summary != "" {
		t.Fatalf("restarted tour = %+v, want clean capturing packet", restarted)
	}
	if restarted.BaseSHA != "base-before-rewrite" || restarted.RepositoryPath != "/repo" {
		t.Fatalf("capture boundary lost: %+v", restarted)
	}
	steps, err := s.ListReviewSteps(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want stale artifacts removed", steps)
	}
}

func TestReviewChapterJSONVerbsExposeMetadataAndSelectedDiff(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewReviewService(s, nil, nil)
	stepID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{
		Title: "JSON chapter", Narration: "Machine-readable review context.",
		Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	listOutput := captureStdout(t, func() int {
		return runReviewCmd([]string{"chapters", "--json", "--task", "task-1"})
	})
	var list service.ReviewChaptersView
	if err := json.Unmarshal([]byte(listOutput), &list); err != nil {
		t.Fatalf("chapters JSON: %v\n%s", err, listOutput)
	}
	if len(list.Chapters) != 1 || list.Chapters[0].ID != stepID || len(list.Chapters[0].Hunks) != 1 {
		t.Fatalf("chapters = %+v", list.Chapters)
	}

	detailOutput := captureStdout(t, func() int {
		return runReviewCmd([]string{"chapter", "show", stepID[:8], "--json", "--task", "task-1"})
	})
	var detail service.ReviewChapterDetail
	if err := json.Unmarshal([]byte(detailOutput), &detail); err != nil {
		t.Fatalf("chapter detail JSON: %v\n%s", err, detailOutput)
	}
	if detail.ID != stepID || len(detail.Diff) != 1 || detail.Diff[0].NewPath != "chapter.go" {
		t.Fatalf("detail = %+v", detail)
	}
}

func initReviewCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	git("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "chapter.go"), []byte("package chapter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "add chapter")
	return dir
}

func captureStdout(t *testing.T, run func() int) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := run()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if code != 0 {
		t.Fatalf("runReviewCmd() = %d", code)
	}
	return string(out)
}

func reviewCLITestStore(t *testing.T) (string, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return dbPath, s
}

func configureReviewCLITest(t *testing.T, dbPath string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("db:\n  path: %q\n", dbPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "")
	t.Setenv("LEGATO_REVIEW_NAME", "")
}
