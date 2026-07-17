package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
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
