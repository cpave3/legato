package cli_test

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestTaskWorktreeSetPersistsMetadata(t *testing.T) {
	db, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "one", Status: "todo"}); err != nil {
		t.Fatal(err)
	}
	meta := store.TaskWorktree{Path: "/trees/one", PrimaryDir: "/repo", Branch: "task-1", BaseBranch: "main"}
	if err := cli.TaskWorktreeSet(db, "task-1", meta); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorktreePath == nil || *got.WorktreePath != meta.Path || got.WorktreePrimaryDir == nil || *got.WorktreePrimaryDir != meta.PrimaryDir || got.WorktreeBranch == nil || *got.WorktreeBranch != meta.Branch || got.WorktreeBaseBranch == nil || *got.WorktreeBaseBranch != meta.BaseBranch {
		t.Fatalf("worktree metadata = %#v", got)
	}
}
