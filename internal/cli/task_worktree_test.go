package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestTaskWorktreeSetBroadcastsChangeAfterPersistence(t *testing.T) {
	db, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "one", Status: "todo"}); err != nil {
		t.Fatal(err)
	}

	runtimeDir, err := os.MkdirTemp("/tmp", "legato-wt-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	messages := make(chan ipc.Message, 1)
	srv := newTestIPCServer(t, filepath.Join(runtimeDir, "legato", "legato-test.sock"), func(msg ipc.Message) {
		messages <- msg
	})
	defer srv.Close()

	meta := store.TaskWorktree{Path: "/trees/one", PrimaryDir: "/repo", Branch: "task-1", BaseBranch: "main"}
	if err := cli.TaskWorktreeSet(db, "task-1", meta); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-messages:
		if msg.Type != "worktree_changed" || msg.TaskID != "task-1" {
			t.Fatalf("message = %+v, want worktree_changed for task-1", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worktree change broadcast")
	}
}

func TestTaskWorktreeClearBroadcastsChangeAfterPersistence(t *testing.T) {
	db, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "one", Status: "todo"}); err != nil {
		t.Fatal(err)
	}
	meta := store.TaskWorktree{Path: "/trees/one"}
	if err := db.SetTaskWorktree(ctx, "task-1", &meta); err != nil {
		t.Fatal(err)
	}

	runtimeDir, err := os.MkdirTemp("/tmp", "legato-wt-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	messages := make(chan ipc.Message, 1)
	srv := newTestIPCServer(t, filepath.Join(runtimeDir, "legato", "legato-test.sock"), func(msg ipc.Message) { messages <- msg })
	defer srv.Close()

	if err := cli.TaskWorktreeClear(db, "task-1"); err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.WorktreePath != nil {
		t.Fatalf("worktree path = %q, want cleared", *task.WorktreePath)
	}
	select {
	case msg := <-messages:
		if msg.Type != "worktree_changed" || msg.TaskID != "task-1" {
			t.Fatalf("message = %+v, want worktree_changed for task-1", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worktree clear broadcast")
	}
}

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
