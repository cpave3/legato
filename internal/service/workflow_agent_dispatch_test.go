package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeTaskAgentDispatcher struct {
	store    *store.Store
	spawned  bool
	prompted bool
	opts     AgentSpawnOptions
}

func (f *fakeTaskAgentDispatcher) SpawnAgent(ctx context.Context, taskID string, _, _ int, opts ...AgentSpawnOptions) error {
	f.spawned = true
	f.opts = opts[0]
	_, err := f.store.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: opts[0].AgentKind, Status: "running"})
	return err
}
func (f *fakeTaskAgentDispatcher) SendMessage(context.Context, string, string) error {
	f.prompted = true
	return nil
}

func TestAgentDispatchExecutorSpawnsMissingTaskAgent(t *testing.T) {
	db := dispatchTestStore(t)
	defer db.Close()
	dispatcher := &fakeTaskAgentDispatcher{store: db}
	result, err := NewAgentDispatchExecutor(dispatcher, db).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"task": map[string]any{"id": "task-1"}, "prompt": "Create a plan", "working_dir": "/repo", "agent": "chimera", "tier": "large",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !dispatcher.spawned || dispatcher.prompted || result.Outputs["created"] != true {
		t.Fatalf("dispatcher=%+v outputs=%#v", dispatcher, result.Outputs)
	}
	if dispatcher.opts.WorkingDir != "/repo" || dispatcher.opts.Assignment != "Create a plan" {
		t.Fatalf("opts = %+v", dispatcher.opts)
	}
}

func TestAgentDispatchExecutorPromptsExistingTaskAgent(t *testing.T) {
	db := dispatchTestStore(t)
	defer db.Close()
	if _, err := db.InsertAgentSessionReturningID(context.Background(), store.AgentSession{TaskID: "task-1", TmuxSession: "legato-task-1", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeTaskAgentDispatcher{store: db}
	result, err := NewAgentDispatchExecutor(dispatcher, db).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"task": map[string]any{"id": "task-1"}, "prompt": "Create a plan", "working_dir": "/repo",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.spawned || !dispatcher.prompted || result.Outputs["created"] != false {
		t.Fatalf("dispatcher=%+v outputs=%#v", dispatcher, result.Outputs)
	}
}

func dispatchTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Task", Status: "Ready"}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
