package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// recordedAgent is an agent of kind on taskID in cwd whose conversation
// conv-<task> is recorded; reason ends its run (none keeps it live).
func recordedAgent(t *testing.T, s *store.Store, taskID, kind, cwd, reason string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: kind, Status: "running", Sandboxed: true})
	if err != nil {
		t.Fatal(err)
	}
	execution := "ex-" + taskID
	if err := s.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: kind, AgentSessionID: &id, WorkingDir: cwd, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, taskID, id, execution, kind, "conv-"+taskID, cwd, ""); err != nil {
		t.Fatal(err)
	}
	if reason != "" {
		if err := s.TerminalizeExecutionEndingRuns(ctx, execution, store.ExecutionLost, reason); err != nil {
			t.Fatal(err)
		}
	}
}

func historyService(t *testing.T, opts SessionHistoryOptions) (*SessionHistoryService, *store.Store) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return NewSessionHistoryService(s, opts), s
}

func entryOf(t *testing.T, h *SessionHistoryService, taskID string) SessionEntry {
	t.Helper()
	page, err := h.List(context.Background(), SessionQuery{TaskID: taskID})
	if err != nil || len(page.Sessions) != 1 {
		t.Fatalf("history of %s = %#v, %v", taskID, page, err)
	}
	return page.Sessions[0]
}

func TestEachEntrySaysHowItStandsAndWhetherItCanBeRestored(t *testing.T) {
	ctx := context.Background()
	h, s := historyService(t, SessionHistoryOptions{
		ToolConfigured: func(kind string) bool { return kind != "codex" },
		Away:           func(_ context.Context, taskID string) bool { return taskID == "away" },
	})
	here := t.TempDir()
	for _, task := range []string{"live", "stopped", "interrupted", "gone-folder", "deleted", "archived", "codex", "away"} {
		createTask(t, s, task)
	}
	recordedAgent(t, s, "live", "claude-code", here, "")
	recordedAgent(t, s, "stopped", "claude-code", here, store.RunStopped)
	recordedAgent(t, s, "interrupted", "claude-code", here, store.RunInterrupted)
	recordedAgent(t, s, "gone-folder", "claude-code", filepath.Join(here, "missing"), store.RunExited)
	recordedAgent(t, s, "deleted", "claude-code", here, store.RunStopped)
	recordedAgent(t, s, "archived", "claude-code", here, store.RunStopped)
	recordedAgent(t, s, "codex", "codex", here, store.RunStopped)
	recordedAgent(t, s, "away", "claude-code", here, store.RunStopped)
	if err := s.DeleteTask(ctx, "deleted"); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTask(ctx, "archived"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		state, endReason string
		restorable       bool
		newTaskOnly      bool
		freshStart       bool
		reason           string
	}{
		"live":        {state: "running", reason: "it is running"},
		"stopped":     {state: "ended", endReason: "stopped", restorable: true},
		"interrupted": {state: "interrupted", restorable: true},
		"gone-folder": {state: "ended", endReason: "exited", freshStart: true, reason: "its folder " + filepath.Join(here, "missing") + " is gone"},
		"deleted":     {state: "ended", endReason: "stopped", restorable: true, newTaskOnly: true, reason: "its task is gone, so it can be restored into a new task only"},
		"archived":    {state: "ended", endReason: "stopped", restorable: true, newTaskOnly: true, reason: "its task is archived, so it can be restored into a new task only"},
		"codex":       {state: "ended", endReason: "stopped", reason: "codex is not configured"},
		"away":        {state: "away", reason: "its task is away on a worker"},
	}
	for task, want := range cases {
		got := entryOf(t, h, task)
		if got.State != want.state || got.EndReason != want.endReason || got.Restorable != want.restorable ||
			got.NewTaskOnly != want.newTaskOnly || got.CanStartFresh != want.freshStart || got.Reason != want.reason {
			t.Errorf("%s = state %q/%q restorable %v new-task-only %v fresh %v reason %q; want %+v",
				task, got.State, got.EndReason, got.Restorable, got.NewTaskOnly, got.CanStartFresh, got.Reason, want)
		}
	}
}

func TestANamedSlotsConversationIsNotRestoredIntoItsSlot(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	createTask(t, s, "task-r")
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "task-r", Slot: SlotReviewer, TmuxSession: "legato-task-r_reviewer", AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-r", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-r", ExecutionTaskID: "task-r", AgentKind: "claude-code", AgentSessionID: &id, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, "task-r", id, "ex-r", "claude-code", "conv-r", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecution(ctx, "ex-r", store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	got := entryOf(t, h, "task-r")
	if !got.Restorable || !got.NewTaskOnly || got.Reason != "Legato starts that slot's agents itself, so it can be restored into a new task only" {
		t.Fatalf("reviewer entry = %+v", got)
	}
}

func TestSearchTermsAreQuotedAndMatchedAsPrefixes(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "t-1", Title: "Docker in the sandbox", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "t-2")
	recordedAgent(t, s, "t-1", "claude-code", "/repo", store.RunStopped)
	recordedAgent(t, s, "t-2", "claude-code", "/repo", store.RunStopped)
	for q, want := range map[string]int{"dock": 1, "dock sand": 1, `doc"ker`: 1, "AND": 0, "OR dock": 0, "repo": 2, "(": 0, "NEAR(": 0} {
		page, err := h.List(ctx, SessionQuery{Q: q})
		if err != nil {
			t.Fatalf("q %q: %v", q, err)
		}
		if len(page.Sessions) != want {
			t.Errorf("q %q = %d entries, want %d", q, len(page.Sessions), want)
		}
	}
}

func TestTheListPagesWithACursor(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	ctx := context.Background()
	for _, task := range []string{"t-1", "t-2", "t-3"} {
		createTask(t, s, task)
		recordedAgent(t, s, task, "claude-code", "/repo", store.RunStopped)
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		page, err := h.List(ctx, SessionQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Sessions {
			seen[e.TaskID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("paged through %v, want all three", seen)
	}
	if _, err := h.List(ctx, SessionQuery{Cursor: "not a cursor"}); !errors.Is(err, ErrBadSessionQuery) {
		t.Fatalf("bad cursor = %v, want ErrBadSessionQuery", err)
	}
	if page, _ := h.List(ctx, SessionQuery{Limit: 1000}); len(page.Sessions) != 3 {
		t.Fatalf("limit over the most = %d entries", len(page.Sessions))
	}
}

func TestAnEntryShowsItsRunsAndCanBeForgotten(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	ctx := context.Background()
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", "/repo", store.RunStopped)
	entry := entryOf(t, h, "t-1")
	detail, err := h.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].ExecutionID != "ex-t-1" || detail.Runs[0].EndReason != "stopped" ||
		detail.Runs[0].Sandboxed == nil || !*detail.Runs[0].Sandboxed || detail.Runs[0].EndedAt == nil {
		t.Fatalf("runs = %+v", detail.Runs)
	}
	if err := h.Forget(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Get(ctx, entry.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get forgotten = %v, want ErrNotFound", err)
	}
}

func TestARunningConversationCannotBeForgotten(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", "/repo", "")
	if err := h.Forget(context.Background(), entryOf(t, h, "t-1").ID); !errors.Is(err, ErrSessionInUse) {
		t.Fatalf("forget running = %v, want ErrSessionInUse", err)
	}
}
