package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestAgentSlotsKeepTheirOwnSessionsAndResumeRecords(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	createTestTask(t, s, "task-1")

	author := nativeSessionOwner(t, s, "task-1", "claude-code", "ex-author", nil)
	reviewer := nativeSessionOwner(t, s, "task-1", "claude-code", "ex-reviewer", func(a *AgentSession, _ *Execution) {
		a.Slot = "reviewer"
		a.TmuxSession = "legato-task-1_reviewer"
	})

	// The task's agent, addressed without a slot, is the main slot's.
	main, err := s.GetAgentSessionByTaskID(ctx, "task-1")
	if err != nil || main.ID != author || main.Slot != SlotMain {
		t.Fatalf("task's agent = %+v, %v; want the main slot's session %d", main, err, author)
	}
	got, err := s.GetAgentSessionInSlot(ctx, "task-1", "reviewer")
	if err != nil || got.ID != reviewer {
		t.Fatalf("reviewer slot = %+v, %v; want session %d", got, err, reviewer)
	}
	// Inside a session, the execution names the slot.
	got, err = s.GetRunningAgentSession(ctx, "task-1", "ex-reviewer")
	if err != nil || got.ID != reviewer {
		t.Fatalf("session of ex-reviewer = %+v, %v; want %d", got, err, reviewer)
	}
	got, err = s.GetRunningAgentSession(ctx, "task-1", "")
	if err != nil || got.ID != author {
		t.Fatalf("session with no execution = %+v, %v; want main %d", got, err, author)
	}

	// Each slot saves its own conversation; the main slot's is the task's.
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", author, "ex-author", "claude-code", "author-conv", "/repo", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, "task-1", reviewer, "ex-reviewer", "claude-code", "reviewer-conv", "/repo", ""); err != nil {
		t.Fatalf("reviewer saving its conversation: %v", err)
	}
	saved, err := s.GetTaskNativeSession(ctx, "task-1", "claude-code")
	if err != nil || saved == nil || saved.ProviderSessionID != "author-conv" {
		t.Fatalf("main slot's conversation = %+v, %v; want author-conv", saved, err)
	}
	saved, err = s.GetTaskNativeSessionInSlot(ctx, "task-1", "reviewer", "claude-code")
	if err != nil || saved == nil || saved.ProviderSessionID != "reviewer-conv" {
		t.Fatalf("reviewer slot's conversation = %+v, %v; want reviewer-conv", saved, err)
	}
	if list, err := s.ListTaskNativeSessions(ctx, "task-1"); err != nil || len(list) != 1 || list[0].ProviderSessionID != "author-conv" {
		t.Fatalf("the task's resumable conversations = %+v, %v; want the author's only", list, err)
	}

	// Ending the reviewer leaves the author running.
	if err := s.UpdateAgentSessionStatusInSlot(ctx, "task-1", "reviewer", "dead"); err != nil {
		t.Fatal(err)
	}
	if main, err := s.GetAgentSessionByTaskID(ctx, "task-1"); err != nil || main.ID != author {
		t.Fatalf("after the reviewer ended, task's agent = %+v, %v", main, err)
	}
	if _, err := s.GetAgentSessionInSlot(ctx, "task-1", "reviewer"); err == nil {
		t.Fatal("reviewer slot still running after it ended")
	}
}

func TestAgentSlotMigrationMovesChimeraSelectorAndKeepsSessionsMain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-slots.db")
	db, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	before := migrationIndex(t, "089_agent_slots.sql")
	for _, name := range migrations[:before] {
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sqlText, err := prepareMigration(db, name, string(data))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(sqlText); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO tasks (id, title, status, chimera_session_id) VALUES ('t1', 'One', 'Backlog', 'chimera-1'), ('t2', 'Two', 'Backlog', NULL);
		INSERT INTO agent_sessions (task_id, tmux_session, status) VALUES ('t1', 'legato-t1', 'running');
		INSERT INTO task_native_sessions (task_id, agent_kind, provider_session_id, cwd, agent_session_id, execution_id)
			VALUES ('t2', 'codex', 'thread-1', '/repo', 7, 'ex-1');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, before)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if session, err := s.GetAgentSessionByTaskID(ctx, "t1"); err != nil || session.Slot != SlotMain {
		t.Fatalf("existing session = %+v, %v; want slot main", session, err)
	}
	if id, err := s.TaskChimeraSessionID(ctx, "t1"); err != nil || id != "chimera-1" {
		t.Fatalf("migrated Chimera selector = %q, %v", id, err)
	}
	if saved, err := s.GetTaskNativeSession(ctx, "t2", "codex"); err != nil || saved == nil || saved.ProviderSessionID != "thread-1" || saved.Slot != SlotMain {
		t.Fatalf("migrated codex record = %+v, %v", saved, err)
	}
}

func migrationIndex(t *testing.T, name string) int {
	t.Helper()
	for i, m := range migrations {
		if m == name {
			return i
		}
	}
	t.Fatalf("no migration %s", name)
	return 0
}
