package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSetCodeContextPreservesPRMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	pr := `{"pr_number":42,"state":"OPEN"}`
	if err := s.UpdatePRMeta(ctx, "task-1", &pr); err != nil {
		t.Fatal(err)
	}
	want := &CodeContext{Repo: "acme/widget", Branch: "feature/task-1", RepoSource: "manual", BranchSource: "manual"}
	if err := s.SetCodeContext(ctx, "task-1", want); err != nil {
		t.Fatal(err)
	}

	task, err := s.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Repo != want.Repo || got.Branch != want.Branch {
		t.Fatalf("code context = %#v, want %#v", got, want)
	}
	if task.PRMeta == nil || *task.PRMeta != pr {
		t.Fatalf("PR metadata changed to %v", task.PRMeta)
	}
}

func TestListPRSyncCandidatesSkipsArchivedAndEphemeralTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"active", "archived"} {
		createTestTask(t, s, id)
	}
	if err := s.CreateTask(ctx, Task{ID: "ephemeral", Title: "Ephemeral", Status: "Doing", Ephemeral: true, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTask(ctx, "archived"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListPRSyncCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("candidates = %+v, want only active", got)
	}
}

func TestMigrationBackfillsCodeContextAndPinsNumberedPR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestTask(t, legacy, "task-1")
	meta := `{"repo":"acme/widget","branch":"feature/TASK-1","head_sha":"abc123","linked_at":"2026-07-01T12:00:00Z","pr_number":42,"pr_url":"https://github.com/acme/widget/pull/42","state":"OPEN"}`
	if err := legacy.UpdatePRMeta(context.Background(), "task-1", &meta); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.DB().Exec(`ALTER TABLE tasks DROP COLUMN code_context`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.DB().Exec(`DELETE FROM schema_migrations WHERE version > 46; PRAGMA user_version = 46`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := New(path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	defer migrated.Close()

	task, err := migrated.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	context, err := ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if context == nil || context.Repo != "acme/widget" || context.Branch != "feature/TASK-1" || context.HeadSHA != "abc123" {
		t.Fatalf("code context = %#v", context)
	}
	pr, err := ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.PRNumber != 42 || pr.PRURL == "" || !pr.Pinned {
		t.Fatalf("PR metadata = %#v", pr)
	}
}
