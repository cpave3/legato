package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func conversationsOf(t *testing.T, s *Store, taskID string) []AgentConversation {
	t.Helper()
	got, err := s.ListTaskConversations(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func runsOf(t *testing.T, s *Store, conversationID int64) []AgentConversationRun {
	t.Helper()
	got, err := s.ListConversationRuns(context.Background(), conversationID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func endReason(run AgentConversationRun) string {
	if run.EndReason == nil {
		return ""
	}
	return *run.EndReason
}

func TestRecordingAConversationKeepsItWithItsRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	if _, err := s.CreateJobClone(ctx, JobClone{ExecutionID: "exec-1", TaskID: "task-1", Repo: "cpave3/legato", Branch: "legato/leg-1", Path: "/jobs/exec-1/legato", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	owner := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	for range 2 {
		if _, err := s.RecordTaskNativeSession(ctx, "task-1", owner, "exec-1", "claude-code", "conv-1", "/jobs/exec-1/legato", "/home/.claude"); err != nil {
			t.Fatal(err)
		}
	}

	conversations := conversationsOf(t, s, "task-1")
	if len(conversations) != 1 {
		t.Fatalf("conversations = %#v, want one", conversations)
	}
	c := conversations[0]
	if c.AgentKind != "claude-code" || c.ProviderSessionID != "conv-1" || c.StorageDir != "/home/.claude" ||
		c.Slot != SlotMain || c.TaskTitle != "Test task task-1" || c.CWD != "/jobs/exec-1/legato" ||
		c.Repo != "cpave3/legato" || c.Branch != "legato/leg-1" || c.Source != ConversationRecorded ||
		c.TranscriptState != TranscriptUnknown || c.Ephemeral || c.Reeve || c.FirstSeenAt.IsZero() {
		t.Fatalf("conversation = %#v", c)
	}
	runs := runsOf(t, s, c.ID)
	if len(runs) != 1 || runs[0].ExecutionID != "exec-1" || runs[0].EndedAt != nil || runs[0].EndReason != nil {
		t.Fatalf("runs = %#v, want one live run of exec-1", runs)
	}
}

func TestANewIdentityInTheSameRunReplacesTheOldOne(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	owner := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	record := func(id string) {
		t.Helper()
		if _, err := s.RecordTaskNativeSession(ctx, "task-1", owner, "exec-1", "claude-code", id, "/repo", ""); err != nil {
			t.Fatal(err)
		}
	}
	record("conv-1")
	record("conv-2") // a /clear

	conversations := conversationsOf(t, s, "task-1")
	if len(conversations) != 2 {
		t.Fatalf("conversations = %#v, want both", conversations)
	}
	byID := map[string]AgentConversation{}
	for _, c := range conversations {
		byID[c.ProviderSessionID] = c
	}
	old := runsOf(t, s, byID["conv-1"].ID)
	if len(old) != 1 || endReason(old[0]) != RunReplaced || old[0].EndedAt == nil {
		t.Fatalf("old runs = %#v, want replaced", old)
	}
	current := runsOf(t, s, byID["conv-2"].ID)
	if len(current) != 1 || current[0].EndReason != nil {
		t.Fatalf("new runs = %#v, want live", current)
	}

	record("conv-1") // a /resume back to the first
	if got := runsOf(t, s, byID["conv-1"].ID); len(got) != 1 || got[0].EndReason != nil || got[0].EndedAt != nil {
		t.Fatalf("resumed runs = %#v, want live again", got)
	}
	if got := runsOf(t, s, byID["conv-2"].ID); endReason(got[0]) != RunReplaced {
		t.Fatalf("second runs = %#v, want replaced", got)
	}
}

func TestAConversationResumedInANewExecutionIsOneEntryWithTwoRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	first := nativeSessionOwner(t, s, "task-1", "codex", "exec-1", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", first, "exec-1", "codex", "thread-1", "/repo", "/codex"); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecution(ctx, "exec-1", ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAgentSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := nativeSessionOwner(t, s, "task-1", "codex", "exec-2", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", second, "exec-2", "codex", "thread-1", "/repo", "/codex"); err != nil {
		t.Fatal(err)
	}
	conversations := conversationsOf(t, s, "task-1")
	if len(conversations) != 1 {
		t.Fatalf("conversations = %#v, want one", conversations)
	}
	runs := runsOf(t, s, conversations[0].ID)
	if len(runs) != 2 || runs[0].ExecutionID != "exec-1" || endReason(runs[0]) != RunStopped || runs[1].ExecutionID != "exec-2" || runs[1].EndReason != nil {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestEndingAnExecutionEndsItsRunsWithTheReason(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		end  func(s *Store) error
		want string
	}{
		{"stopped", func(s *Store) error { return s.TerminalizeExecution(ctx, "exec-1", ExecutionCancelled) }, RunStopped},
		{"exited", func(s *Store) error { return s.TerminalizeExecution(ctx, "exec-1", ExecutionSucceeded) }, RunExited},
		{"lost", func(s *Store) error { return s.TerminalizeExecution(ctx, "exec-1", ExecutionLost) }, RunLost},
		{"gone pane", func(s *Store) error { return s.TerminalizeExecutionEndingRuns(ctx, "exec-1", ExecutionLost, RunExited) }, RunExited},
		{"interrupted", func(s *Store) error {
			return s.TerminalizeExecutionEndingRuns(ctx, "exec-1", ExecutionLost, RunInterrupted)
		}, RunInterrupted},
		{"dismissed", func(s *Store) error {
			if err := s.TerminalizeExecutionEndingRuns(ctx, "exec-1", ExecutionLost, RunInterrupted); err != nil {
				return err
			}
			return s.DismissConversationRuns(ctx, "exec-1")
		}, RunDismissed},
		{"reconciled", func(s *Store) error {
			if err := s.FinishAgentSession(ctx, 1); err != nil {
				return err
			}
			_, err := s.ReconcileLostExecutions(ctx)
			return err
		}, RunLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			createTestTask(t, s, "task-1")
			owner := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
			if owner != 1 {
				t.Fatalf("owner = %d, want 1", owner)
			}
			if _, err := s.RecordTaskNativeSession(ctx, "task-1", owner, "exec-1", "claude-code", "conv-1", "/repo", ""); err != nil {
				t.Fatal(err)
			}
			if err := tc.end(s); err != nil {
				t.Fatal(err)
			}
			runs := runsOf(t, s, conversationsOf(t, s, "task-1")[0].ID)
			if len(runs) != 1 || endReason(runs[0]) != tc.want || runs[0].EndedAt == nil {
				t.Fatalf("runs = %#v, want ended %s", runs, tc.want)
			}
		})
	}
}

func TestDismissingLeavesRunsThatWereNotInterrupted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	owner := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", owner, "exec-1", "claude-code", "conv-1", "/repo", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecution(ctx, "exec-1", ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissConversationRuns(ctx, "exec-1"); err != nil {
		t.Fatal(err)
	}
	if runs := runsOf(t, s, conversationsOf(t, s, "task-1")[0].ID); endReason(runs[0]) != RunStopped {
		t.Fatalf("runs = %#v, want still stopped", runs)
	}
}

func TestAConversationOutlivesItsTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	owner := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", owner, "exec-1", "claude-code", "conv-1", "/repo", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecution(ctx, "exec-1", ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTask(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	conversations := conversationsOf(t, s, "task-1")
	if len(conversations) != 1 || conversations[0].TaskTitle != "Test task task-1" {
		t.Fatalf("conversations after delete = %#v", conversations)
	}
	if runs := runsOf(t, s, conversations[0].ID); len(runs) != 1 {
		t.Fatalf("runs after delete = %#v", runs)
	}
}

func TestAConversationIsFoundByTitleAndByFolder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "task-1", Title: "scn testing gh app", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	createTestTask(t, s, "task-2")
	one := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", one, "exec-1", "claude-code", "conv-1", "/home/me/projects/burnlite", ""); err != nil {
		t.Fatal(err)
	}
	two := nativeSessionOwner(t, s, "task-2", "codex", "exec-2", nil)
	if _, err := s.RecordTaskNativeSession(ctx, "task-2", two, "exec-2", "codex", "thread-2", "/elsewhere", ""); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`"testing"`, `"burnlite"*`} {
		got, err := s.MatchAgentConversations(ctx, query, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ProviderSessionID != "conv-1" {
			t.Fatalf("match %s = %#v, want conv-1 only", query, got)
		}
	}
}

func TestEphemeralAndReeveConversationsAreMarked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ephemeral, err := s.CreateEphemeralTask(ctx, "quick look")
	if err != nil {
		t.Fatal(err)
	}
	reeve, err := s.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	for i, taskID := range []string{ephemeral, reeve} {
		execution := []string{"exec-e", "exec-r"}[i]
		owner := nativeSessionOwner(t, s, taskID, "claude-code", execution, nil)
		if _, err := s.RecordTaskNativeSession(ctx, taskID, owner, execution, "claude-code", "conv-"+execution, "/repo", ""); err != nil {
			t.Fatal(err)
		}
	}
	if c := conversationsOf(t, s, ephemeral); len(c) != 1 || !c[0].Ephemeral || c[0].Reeve {
		t.Fatalf("ephemeral conversation = %#v", c)
	}
	if c := conversationsOf(t, s, reeve); len(c) != 1 || !c[0].Reeve {
		t.Fatalf("Reeve conversation = %#v", c)
	}
}

func TestALegacyChimeraSelectorIsKeptAsAConversation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	nativeSessionOwner(t, s, "task-1", "chimera", "exec-1", nil)
	if err := s.SetChimeraResumeInSlot(ctx, "task-1", SlotMain, "chimera-1"); err != nil {
		t.Fatal(err)
	}
	conversations := conversationsOf(t, s, "task-1")
	if len(conversations) != 1 || conversations[0].AgentKind != "chimera" || conversations[0].ProviderSessionID != "chimera-1" {
		t.Fatalf("conversations = %#v", conversations)
	}
	if runs := runsOf(t, s, conversations[0].ID); len(runs) != 1 || runs[0].ExecutionID != "exec-1" || runs[0].EndReason != nil {
		t.Fatalf("runs = %#v, want the live execution", runs)
	}
	// Forgetting the selector leaves the history as it is.
	if err := s.SetChimeraResumeInSlot(ctx, "task-1", SlotMain, ""); err != nil {
		t.Fatal(err)
	}
	if got := conversationsOf(t, s, "task-1"); len(got) != 1 {
		t.Fatalf("conversations after forget = %#v", got)
	}
}

func TestTheMigrationBackfillsSavedConversations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	s := newStoreAtMigration(t, path, "130_agent_stages.sql")
	createTestTask(t, s, "task-1")
	createTestTask(t, s, "task-2")
	// The release before kept only the resume records; written as it wrote them.
	saved := func(taskID string, owner int, execution, kind, provider, cwd, storage string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO task_native_sessions (task_id, slot, agent_kind, provider_session_id, cwd, storage_dir, agent_session_id, execution_id, updated_at)
			VALUES (?, 'main', ?, ?, ?, ?, ?, ?, ?)`, taskID, kind, provider, cwd, storage, owner, execution, formatExecutionTime(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	stopped := nativeSessionOwner(t, s, "task-1", "claude-code", "exec-1", nil)
	saved("task-1", stopped, "exec-1", "claude-code", "conv-1", "/repo", "/claude")
	if _, err := s.db.ExecContext(ctx, `UPDATE executions SET status = 'cancelled', ended_at = started_at WHERE id = 'exec-1'`); err != nil {
		t.Fatal(err)
	}
	running := nativeSessionOwner(t, s, "task-2", "codex", "exec-2", nil)
	saved("task-2", running, "exec-2", "codex", "thread-2", "/other", "")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()

	one := conversationsOf(t, migrated, "task-1")
	if len(one) != 1 || one[0].ProviderSessionID != "conv-1" || one[0].StorageDir != "/claude" || one[0].CWD != "/repo" || one[0].TaskTitle != "Test task task-1" {
		t.Fatalf("backfilled = %#v", one)
	}
	if runs := runsOf(t, migrated, one[0].ID); len(runs) != 1 || endReason(runs[0]) != RunStopped {
		t.Fatalf("backfilled runs = %#v, want stopped", runs)
	}
	two := conversationsOf(t, migrated, "task-2")
	if runs := runsOf(t, migrated, two[0].ID); len(runs) != 1 || runs[0].EndReason != nil {
		t.Fatalf("backfilled live runs = %#v", runs)
	}
	if got, err := migrated.MatchAgentConversations(ctx, `"other"`, 10); err != nil || len(got) != 1 || got[0].ProviderSessionID != "thread-2" {
		t.Fatalf("backfilled search = %#v, %v", got, err)
	}
}
