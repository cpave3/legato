package service

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeChimeraSessionRemover struct {
	removed bool
	err     error
	ids     []string
}

func (f *fakeChimeraSessionRemover) Remove(_ context.Context, sessionID string) (bool, error) {
	f.ids = append(f.ids, sessionID)
	return f.removed, f.err
}

func TestChimeraSessionRemoveExecutorRemovesExactSessionAndClearsResumeMetadata(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Done", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskChimeraSessionID(ctx, "task-1", "chimera-full-id"); err != nil {
		t.Fatal(err)
	}
	remover := &fakeChimeraSessionRemover{removed: true}
	executor := NewChimeraSessionRemoveExecutor(db, remover)

	result, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.ids) != 1 || remover.ids[0] != "chimera-full-id" {
		t.Fatalf("removed ids = %v", remover.ids)
	}
	if result.Status != "succeeded" || result.Outputs["removed"] != true || result.Outputs["metadata_cleared"] != true {
		t.Fatalf("result = %+v", result)
	}
	task, _ := db.GetTask(ctx, "task-1")
	if task.ChimeraSessionID != nil {
		t.Fatalf("task session = %v", task.ChimeraSessionID)
	}
}

func TestChimeraSessionRemoveExecutorPreservesMetadataOnProviderRefusal(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Done", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskChimeraSessionID(ctx, "task-1", "chimera-full-id"); err != nil {
		t.Fatal(err)
	}
	_, err = NewChimeraSessionRemoveExecutor(db, &fakeChimeraSessionRemover{err: errors.New("session has children; delete children first")}).Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err == nil {
		t.Fatal("expected provider refusal")
	}
	task, _ := db.GetTask(ctx, "task-1")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chimera-full-id" {
		t.Fatalf("task session = %v", task.ChimeraSessionID)
	}
}

func TestChimeraCLISessionRemoverUsesExactIdempotentStructuredCommand(t *testing.T) {
	remover := NewChimeraCLISessionRemover("/opt/chimera")
	var binary string
	var args []string
	remover.run = func(_ context.Context, name string, commandArgs ...string) ([]byte, error) {
		binary = name
		args = append([]string(nil), commandArgs...)
		return []byte(`{"removed":false}`), nil
	}
	removed, err := remover.Remove(context.Background(), "chimera-full-id")
	if err != nil {
		t.Fatal(err)
	}
	if removed || binary != "/opt/chimera" {
		t.Fatalf("removed=%v binary=%q", removed, binary)
	}
	want := []string{"sessions", "rm", "chimera-full-id", "--exact", "--if-exists", "--json"}
	if !slices.Equal(args, want) {
		t.Fatalf("args=%v want=%v", args, want)
	}
}
