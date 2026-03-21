package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestArchiveDoneCards(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Seed some done tasks
	tasks := []store.Task{
		{ID: "d1", Title: "Done 1", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "d2", Title: "Done 2", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "ip1", Title: "In Progress", Status: "In Progress", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	count, err := svc.ArchiveDoneCards(ctx)
	if err != nil {
		t.Fatalf("ArchiveDoneCards: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 archived, got %d", count)
	}

	// Done column should now be empty
	cards, err := svc.ListCards(ctx, "Done")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 done cards, got %d", len(cards))
	}

	// In Progress should be untouched
	cards, err = svc.ListCards(ctx, "In Progress")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 in-progress card, got %d", len(cards))
	}
}

func TestArchiveDoneCards_NoDone(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)

	count, err := svc.ArchiveDoneCards(context.Background())
	if err != nil {
		t.Fatalf("ArchiveDoneCards: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestArchiveTask_OnlyDone(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []store.Task{
		{ID: "d1", Title: "Done task", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "ip1", Title: "In Progress", Status: "In Progress", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	// Archiving a done task should succeed
	if err := svc.ArchiveTask(ctx, "d1"); err != nil {
		t.Fatalf("ArchiveTask done: %v", err)
	}

	// Archiving a non-done task should fail
	if err := svc.ArchiveTask(ctx, "ip1"); err == nil {
		t.Fatal("expected error when archiving non-done task")
	}
}

func TestCountDoneCards(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []store.Task{
		{ID: "d1", Title: "Done 1", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "d2", Title: "Done 2", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "ip1", Title: "In Progress", Status: "In Progress", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	count, err := svc.CountDoneCards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}

	// Archive one, count should drop
	if err := svc.ArchiveTask(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	count, err = svc.CountDoneCards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}

func TestSearchCards_ExcludesArchived(t *testing.T) {
	s, _, svc := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	tasks := []store.Task{
		{ID: "d1", Title: "Findable done", Status: "Done", CreatedAt: now, UpdatedAt: now},
		{ID: "d2", Title: "Also findable", Status: "Done", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	// Archive d1
	if err := svc.ArchiveTask(ctx, "d1"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.SearchCards(ctx, "findable")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].ID != "d2" {
		t.Fatalf("expected d2, got %s", results[0].ID)
	}
}
