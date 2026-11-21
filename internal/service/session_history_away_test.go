package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAnAwayTasksHistoryIsOnItsWorker(t *testing.T) {
	summary := `{"handoff_id": "h-1", "worker_task_id": "w-task", "state": "in_progress", "url": "/board?highlight_task=w-task"}`
	got := WorkerHistoryURL(store.AwayTask{WorkerTaskID: "w-task", WorkerAddress: "https://worker.lan:7432/", LastSummary: &summary})
	if got != "https://worker.lan:7432/agents/history?task=w-task" {
		t.Fatalf("history link = %q", got)
	}
	if got := WorkerHistoryURL(store.AwayTask{WorkerTaskID: "w-task"}); got != "" {
		t.Fatalf("with no worker address = %q, want none", got)
	}
}

func TestAnAwayEntryLinksToTheWorkersHistory(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{
		Away:           func(context.Context, string) bool { return true },
		AwayHistoryURL: func(_ context.Context, taskID string) string { return "https://worker/agents/history?task=w-" + taskID },
	})
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", t.TempDir(), store.RunStopped)
	entry := entryOf(t, h, "t-1")
	if entry.State != "away" || entry.Restorable || entry.AwayHistoryURL != "https://worker/agents/history?task=w-t-1" {
		t.Fatalf("away entry = %+v", entry)
	}
}
