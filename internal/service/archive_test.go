package service

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/attachments"
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

func TestArchiveDoneCardsRemovesOnlyArchivedAttachmentCaches(t *testing.T) {
	s, bus, _ := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, task := range []store.Task{{ID: "done", Title: "Done", Status: "Done", CreatedAt: now, UpdatedAt: now}, {ID: "active", Title: "Active", Status: "In Progress", CreatedAt: now, UpdatedAt: now}} {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	cache := attachments.NewCache(t.TempDir(), 1024)
	dl := &archiveDownloader{}
	for _, id := range []string{"done", "active"} {
		if err := cache.Reconcile(ctx, id, []attachments.Metadata{{ID: "1", Filename: "screen.png", MimeType: "image/png", Size: 3}}, dl); err != nil {
			t.Fatal(err)
		}
	}
	doneItems, _ := cache.List("done")
	activeItems, _ := cache.List("active")

	svc := NewBoardService(s, bus, cache)
	if _, err := svc.ArchiveDoneCards(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(doneItems[0].Path); !os.IsNotExist(err) {
		t.Fatalf("done cache remains: %v", err)
	}
	if _, err := os.Stat(activeItems[0].Path); err != nil {
		t.Fatalf("active cache removed: %v", err)
	}
}

type archiveDownloader struct{}

func (*archiveDownloader) DownloadAttachment(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("PNG")), nil
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

func TestArchiveTaskRemovesAttachmentCache(t *testing.T) {
	s, bus, _ := setupTestBoard(t)
	seedColumns(t, s)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(ctx, store.Task{ID: "done", Title: "Done", Status: "Done", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cache := attachments.NewCache(t.TempDir(), 1024)
	if err := cache.Reconcile(ctx, "done", []attachments.Metadata{{ID: "1", Filename: "screen.png", MimeType: "image/png", Size: 3}}, &archiveDownloader{}); err != nil {
		t.Fatal(err)
	}
	items, _ := cache.List("done")

	if err := NewBoardService(s, bus, cache).ArchiveTask(ctx, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(items[0].Path); !os.IsNotExist(err) {
		t.Fatalf("attachment remains: %v", err)
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
