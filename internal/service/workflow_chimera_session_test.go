package service

import (
	"context"
	"errors"
	"path/filepath"
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

	result, err := executor.Start(ctx, ResolvedWorkflowStep{RunID: "run-1", StepID: "remove", With: map[string]any{"task": map[string]any{"id": "task-1"}}})
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
	_, err = NewChimeraSessionRemoveExecutor(db, &fakeChimeraSessionRemover{err: errors.New("session has children; delete children first")}).Start(ctx, ResolvedWorkflowStep{RunID: "run-1", StepID: "remove", With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err == nil {
		t.Fatal("expected provider refusal")
	}
	task, _ := db.GetTask(ctx, "task-1")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chimera-full-id" {
		t.Fatalf("task session = %v", task.ChimeraSessionID)
	}
}

func TestChimeraSessionRemoveExecutorDoesNotCallProviderWithoutRetainedSession(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Done", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	remover := &fakeChimeraSessionRemover{removed: true}
	result, err := NewChimeraSessionRemoveExecutor(db, remover).Start(ctx, ResolvedWorkflowStep{RunID: "run-1", StepID: "remove", With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.ids) != 0 || result.Outputs["removed"] != false || result.Outputs["metadata_cleared"] != false {
		t.Fatalf("ids=%v result=%+v", remover.ids, result)
	}
}

func TestChimeraSessionRemoveExecutorDoesNotCallProviderForRunningAgent(t *testing.T) {
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
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "task-1", TmuxSession: "live", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	remover := &fakeChimeraSessionRemover{removed: true}
	_, err = NewChimeraSessionRemoveExecutor(db, remover).Start(ctx, ResolvedWorkflowStep{RunID: "run-1", StepID: "remove", With: map[string]any{"task": map[string]any{"id": "task-1"}}})
	if err == nil || len(remover.ids) != 0 {
		t.Fatalf("err=%v ids=%v", err, remover.ids)
	}
}
