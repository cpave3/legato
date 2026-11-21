package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// newTaskHistory is the fixture's history with restores into a new task,
// made on a board with columns.
func (f restartFixture) newTaskHistory(t *testing.T) *SessionHistoryService {
	t.Helper()
	seedColumns(t, f.store)
	return f.history(SessionHistoryOptions{NewTask: BoardNewTask(NewBoardService(f.store, events.New()), f.store)})
}

// onlyNewTask is the one task other than except.
func (f restartFixture) onlyNewTask(t *testing.T, except string) store.Task {
	t.Helper()
	var found []store.Task
	for _, column := range []string{"Backlog", "In Progress", "Done", "Doing"} {
		tasks, err := f.store.ListTasksByStatus(context.Background(), column)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if task.ID != except {
				found = append(found, task)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("new tasks = %#v, want one", found)
	}
	return found[0]
}

func TestADeletedTasksConversationIsRestoredIntoANewCard(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	history := f.newTaskHistory(t)
	workspace, err := f.store.CreateWorkspace(ctx, store.Workspace{Name: "Legato"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateTask(ctx, store.Task{ID: "task-1", Title: "scn testing gh app", Status: "In Progress", WorkspaceID: &workspace}); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	f.ranAndStopped(t, "task-1", cwd, "conv-1")
	id := f.conversationID(t, "task-1", "conv-1")
	if err := f.store.DeleteTask(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if refused := refusal(t, restoreErr(history, ctx, id, RestoreIntoTask)); !refused.NewTaskPossible {
		t.Fatalf("restore into the deleted task = %+v, want a new task offered", refused)
	}
	f.clearSent()

	if err := restoreErr(history, ctx, id, RestoreIntoNewTask); err != nil {
		t.Fatal(err)
	}
	task := f.onlyNewTask(t, "")
	if task.Title != "scn testing gh app" || task.Status != "Backlog" || task.WorkspaceID == nil || *task.WorkspaceID != workspace || task.Ephemeral {
		t.Fatalf("new task = %#v, want a card titled as the original in its workspace and the first column", task)
	}
	lines := f.linesEventually(t, task.ID, 2)
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "claude --resume conv-1") || !strings.HasPrefix(lines[1], "[legato restored] ") {
		t.Fatalf("lines = %q, want the conversation resumed in the new task", lines)
	}
	// Its bridge reports it from the new task, which now owns it.
	f.reportAs(t, task.ID, cwd, "conv-1")
	if saved, err := f.store.GetTaskNativeSession(ctx, task.ID, "claude-code"); err != nil || saved == nil || saved.ProviderSessionID != "conv-1" {
		t.Fatalf("new task's saved conversation = %#v, %v", saved, err)
	}
	entry, err := history.Get(ctx, id)
	if err != nil || entry.TaskID != task.ID || len(entry.Runs) != 2 {
		t.Fatalf("entry = %+v, %v; want it on the new task with both runs", entry, err)
	}
}

func TestABusyTasksConversationIsRestoredIntoANewTask(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	history := f.newTaskHistory(t)
	createTask(t, f.store, "task-1")
	cwd := t.TempDir()
	f.ranAndStopped(t, "task-1", cwd, "conv-old")
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportAs(t, "task-1", cwd, "conv-new")
	if err := restoreErr(history, ctx, f.conversationID(t, "task-1", "conv-old"), RestoreIntoNewTask); err != nil {
		t.Fatal(err)
	}
	task := f.onlyNewTask(t, "task-1")
	if lines := f.linesEventually(t, task.ID, 1); len(lines) == 0 || !strings.HasPrefix(lines[0], "claude --resume conv-old") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestAnEphemeralConversationIsRestoredIntoAnEphemeralTask(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	history := f.newTaskHistory(t)
	original, err := f.store.CreateEphemeralTask(ctx, "quick look")
	if err != nil {
		t.Fatal(err)
	}
	f.ranAndStopped(t, original, t.TempDir(), "conv-e")
	if err := f.store.DeleteTask(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := restoreErr(history, ctx, f.conversationID(t, original, "conv-e"), RestoreIntoNewTask); err != nil {
		t.Fatal(err)
	}
	conversations, _ := f.store.ListAgentConversations(ctx, store.ConversationFilter{})
	moved, err := f.store.GetTask(ctx, conversations[0].TaskID)
	if err != nil || !moved.Ephemeral || moved.Title != "quick look" {
		t.Fatalf("new task = %#v, %v; want an ephemeral one titled as the original", moved, err)
	}
}

func TestAFailedRestoreIntoANewTaskLeavesNothing(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	history := f.newTaskHistory(t)
	createTask(t, f.store, "task-1")
	f.ranAndStopped(t, "task-1", t.TempDir(), "conv-1")
	id := f.conversationID(t, "task-1", "conv-1")
	f.tmux.mu.Lock()
	f.tmux.spawnErr = errors.New("tmux is gone")
	f.tmux.mu.Unlock()

	if err := restoreErr(history, ctx, id, RestoreIntoNewTask); err == nil {
		t.Fatal("restore with a failing spawn succeeded")
	}
	f.onlyNewTask(t, "") // only task-1 is left
	if entry, err := history.Get(ctx, id); err != nil || entry.TaskID != "task-1" {
		t.Fatalf("entry after a failed restore = %+v, %v; want it still on task-1", entry, err)
	}
}
