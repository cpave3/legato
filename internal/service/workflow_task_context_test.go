package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestTaskContextExecutorExportsFullContext(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Plan this", DescriptionMD: "Task body", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertTaskNote(ctx, store.TaskNote{ID: "note-1", TaskID: "task-1", Body: "Triage result", Source: "workflow"}); err != nil {
		t.Fatal(err)
	}
	executor := NewTaskContextExecutor(NewBoardService(db, nil))
	result, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := result.Outputs["content"].(string)
	if content == "" || !containsAll(content, "Plan this", "Task body", "Triage result") {
		t.Fatalf("content = %q", content)
	}
}

func TestTaskWorkingDirResolvePrefersWorktreeAndFallsBackToRepository(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, task := range []store.Task{
		{ID: "with-worktree", Title: "One", Status: "Ready"},
		{ID: "without-worktree", Title: "Two", Status: "Ready"},
	} {
		if err := db.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetTaskWorktree(ctx, "with-worktree", &store.TaskWorktree{PrimaryDir: "/repo", Path: "/repo/.worktrees/task", Branch: "task", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	executor := NewTaskWorkingDirResolveExecutor(db)
	for _, test := range []struct {
		task, path, source string
	}{
		{"with-worktree", "/repo/.worktrees/task", "worktree"},
		{"without-worktree", "/repo", "repository"},
	} {
		result, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": test.task}, "fallback": "/repo"}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outputs["path"] != test.path || result.Outputs["source"] != test.source {
			t.Fatalf("task %s outputs = %#v", test.task, result.Outputs)
		}
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
