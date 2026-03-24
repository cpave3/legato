package store

import (
	"context"
	"testing"
)

func TestAgentSessionsTableExists(t *testing.T) {
	s := newTestStore(t)

	var tables []string
	err := s.db.Select(&tables, "SELECT name FROM sqlite_master WHERE type='table' AND name='agent_sessions'")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatal("agent_sessions table does not exist after migration")
	}
}

func TestInsertAndListAgentSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TaskID:      "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := s.ListAgentSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", sessions[0].TaskID, "REX-1238")
	}
	if sessions[0].TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", sessions[0].TmuxSession, "legato-REX-1238")
	}
	if sessions[0].Status != "running" {
		t.Errorf("Status = %q, want %q", sessions[0].Status, "running")
	}
}

func TestGetAgentSessionByTaskID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TaskID:      "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTaskID(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", got.TmuxSession, "legato-REX-1238")
	}

	_, err = s.GetAgentSessionByTaskID(ctx, "NOPE-999")
	if err == nil {
		t.Error("expected error for non-existent task, got nil")
	}
}

func TestGetAgentSessionByTmuxName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TaskID:      "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTmuxName(ctx, "legato-REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "REX-1238")
	}

	_, err = s.GetAgentSessionByTmuxName(ctx, "legato-NOPE")
	if err == nil {
		t.Error("expected error for non-existent tmux session, got nil")
	}
}

func TestUpdateAgentSessionStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TaskID:      "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateAgentSessionStatus(ctx, "REX-1238", "dead")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTmuxName(ctx, "legato-REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "dead" {
		t.Errorf("Status = %q, want %q", got.Status, "dead")
	}
	if got.EndedAt == nil {
		t.Error("EndedAt should be set when status changes to dead")
	}
}

func TestInsertDuplicateTmuxSessionFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "REX-1238")

	session := AgentSession{
		TaskID:      "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	}
	if err := s.InsertAgentSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, session); err == nil {
		t.Error("expected error for duplicate tmux_session, got nil")
	}
}

func TestGetAgentActivityCounts_MixedStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")
	createTestTask(t, s, "task3")
	createTestTask(t, s, "task4")

	// 2 working, 1 waiting, 1 idle
	for _, sess := range []AgentSession{
		{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"},
		{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"},
		{TaskID: "task3", TmuxSession: "legato-task3", Command: "shell", Status: "running"},
		{TaskID: "task4", TmuxSession: "legato-task4", Command: "shell", Status: "running"},
	} {
		if err := s.InsertAgentSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")
	s.UpdateAgentActivity(ctx, "task3", "waiting")

	working, waiting, idle, err := s.GetAgentActivityCounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 2 {
		t.Errorf("working = %d, want 2", working)
	}
	if waiting != 1 {
		t.Errorf("waiting = %d, want 1", waiting)
	}
	if idle != 1 {
		t.Errorf("idle = %d, want 1", idle)
	}
}

func TestGetAgentActivityCounts_ExcludeTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")

	s.InsertAgentSession(ctx, AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	working, _, _, err := s.GetAgentActivityCounts(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if working != 1 {
		t.Errorf("working = %d, want 1 (task1 excluded)", working)
	}
}

func TestGetAgentActivityCounts_NoSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	working, waiting, idle, err := s.GetAgentActivityCounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 0 || waiting != 0 || idle != 0 {
		t.Errorf("got working=%d waiting=%d idle=%d, want all 0", working, waiting, idle)
	}
}

func TestGetAgentActivityCounts_IgnoresDeadSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")

	s.InsertAgentSession(ctx, AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	// Kill task2
	s.UpdateAgentSessionStatus(ctx, "task2", "dead")

	working, _, _, err := s.GetAgentActivityCounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 1 {
		t.Errorf("working = %d, want 1 (dead session excluded)", working)
	}
}

func createTestTask(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTask(ctx, Task{
		ID:        id,
		Title:     "Test task " + id,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}
