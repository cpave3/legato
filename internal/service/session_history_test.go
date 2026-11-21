package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// historyEntry records a conversation on taskID as an agent's bridge does,
// and ends its run for reason (none keeps it live).
func historyEntry(t *testing.T, s *store.Store, taskID, reason string) {
	t.Helper()
	ctx := context.Background()
	createTask(t, s, taskID)
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	execution := "ex-" + taskID
	if err := s.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: "claude-code", AgentSessionID: &id, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, taskID, id, execution, "claude-code", "conv-"+taskID, "/repo", ""); err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		if err := s.TerminalizeExecutionEndingRuns(ctx, execution, store.ExecutionLost, reason); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSessionHistoryKeepsEntriesForTheSetting(t *testing.T) {
	ctx := context.Background()
	day := 24 * time.Hour
	s, err := store.New(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	historyEntry(t, s, "stopped", store.RunStopped)
	historyEntry(t, s, "interrupted", store.RunInterrupted)
	historyEntry(t, s, "running", "")
	recorded := time.Now()
	sweepAt := func(keep, after time.Duration) int {
		t.Helper()
		history := NewSessionHistoryService(s, SessionHistoryOptions{Keep: keep, Now: func() time.Time { return recorded.Add(after) }})
		removed, err := history.Sweep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return removed
	}

	if removed := sweepAt(180*day, 179*day); removed != 0 {
		t.Fatalf("at 179 days removed %d, want none", removed)
	}
	if removed := sweepAt(0, 10000*day); removed != 0 {
		t.Fatalf("keeping forever removed %d, want none", removed)
	}
	if removed := sweepAt(180*day, 181*day); removed != 1 {
		t.Fatalf("at 181 days removed %d, want the stopped one", removed)
	}
	for task, want := range map[string]int{"stopped": 0, "interrupted": 1, "running": 1} {
		if got, _ := s.ListTaskConversations(ctx, task); len(got) != want {
			t.Errorf("%s: %d conversations, want %d", task, len(got), want)
		}
	}
	if got, err := s.MatchAgentConversations(ctx, `"stopped"`, 10); err != nil || len(got) != 0 {
		t.Fatalf("search after sweep = %#v, %v; want its row gone", got, err)
	}
}
