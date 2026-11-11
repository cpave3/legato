package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAnEngramThatDidNotBootIsAnAttentionItemOnItsTask(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", TaskTitle: "Fix the login", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExecutionEngramFailure(ctx, "ex-1", "mneme", "engram's store is missing"); err != nil {
		t.Fatal(err)
	}
	items, err := NewEngramAttentionAdapter(s).Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v %v", items, err)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalEngramBootFailed || item.TaskID != "task-1" || item.Navigation.Kind != "task" || item.Navigation.Target != "task-1" {
		t.Fatalf("item = %+v", item)
	}
	if item.Title != "mneme did not boot" || item.Detail != "engram's store is missing" {
		t.Fatalf("item words = %q / %q / %q", item.Title, item.Reason, item.Detail)
	}
	if !knownAttentionSignals[item.SignalKind] {
		t.Fatal("the signal is not known to the attention registry")
	}
}

// The Reeve's task is hidden: its item says it is the Reeve's engram and
// leads to the Reeve's engram in Settings.
func TestTheReevesEngramThatDidNotBootLeadsToItsSettings(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	taskID, err := s.CreateReeveTask(ctx, "The Reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-r", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "The Reeve", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExecutionEngramFailure(ctx, "ex-r", "reeve-1a2b3c4d", "engram was not found"); err != nil {
		t.Fatal(err)
	}
	items, err := NewEngramAttentionAdapter(s).Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v %v", items, err)
	}
	item := items[0]
	if item.Title != "The Reeve's engram did not boot" || item.Navigation.Kind != "settings" || item.Navigation.Target != "reeve-engram" || item.Detail != "engram was not found" {
		t.Fatalf("item = %+v", item)
	}
	if item.Actions[0].ID != "open" {
		t.Fatalf("actions = %+v", item.Actions)
	}
}
