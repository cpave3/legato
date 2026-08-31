package store

import (
	"context"
	"testing"
	"time"
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

func TestListAgentSessionsIncludesLinkedExecutionAttribution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "user-task")
	createTestTask(t, s, "workflow-task")

	sessionID, err := s.InsertAgentSessionReturningID(ctx, AgentSession{
		TaskID: "workflow-task", TmuxSession: "legato-workflow-task", Command: "shell", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, Execution{
		ID: "exec-workflow", Kind: ExecutionWorkflowAgent, Status: ExecutionRunning,
		TaskID: "user-task", ExecutionTaskID: "workflow-task", TaskTitle: "User task",
		WorkflowRunID: "run-1", AgentSessionID: &sessionID, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	sessions, err := s.ListAgentSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	got := sessions[0]
	if got.TaskID != "workflow-task" || got.ExecutionID != "exec-workflow" || got.AttributionTaskID != "user-task" || got.ExecutionKind != ExecutionWorkflowAgent || got.WorkflowRunID != "run-1" {
		t.Fatalf("linked agent session = %#v", got)
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

func TestClearChimeraDiscoveryForSessionMatchesExactRunningBinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "TASK-42")

	sessionID, err := s.InsertAgentSessionReturningID(ctx, AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, sessionID, "chimera-old", "http://127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraLastEventID(ctx, "TASK-42", "event-9"); err != nil {
		t.Fatal(err)
	}

	cleared, err := s.ClearChimeraDiscoveryForSession(ctx, sessionID, "chimera-other", "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if cleared {
		t.Fatal("mismatched binding was cleared")
	}
	before, err := s.GetAgentSessionByTaskID(ctx, "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if before.ChimeraSessionID == nil || before.LastEventID == nil {
		t.Fatalf("mismatched clear mutated binding: %#v", before)
	}

	cleared, err = s.ClearChimeraDiscoveryForSession(ctx, sessionID, "chimera-old", "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("exact binding was not cleared")
	}
	after, err := s.GetAgentSessionByTaskID(ctx, "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if after.ChimeraSessionID != nil || after.ChimeraEndpoint != nil || after.LastEventID != nil {
		t.Fatalf("binding was not fully cleared: %#v", after)
	}
}

func TestClearChimeraDiscoveryForSessionDoesNotClearInactiveRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "TASK-42")

	sessionID, err := s.InsertAgentSessionReturningID(ctx, AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, sessionID, "chimera-old", "http://127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentSessionStatus(ctx, "TASK-42", "dead"); err != nil {
		t.Fatal(err)
	}

	cleared, err := s.ClearChimeraDiscoveryForSession(ctx, sessionID, "chimera-old", "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if cleared {
		t.Fatal("inactive binding was cleared")
	}
}

func TestGetAgentActivityCounts_MixedStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")
	createTestTask(t, s, "task3")
	createTestTask(t, s, "task4")
	createTestTask(t, s, "task5")

	// 2 working, 1 waiting, 1 error, 1 idle
	for _, sess := range []AgentSession{
		{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"},
		{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"},
		{TaskID: "task3", TmuxSession: "legato-task3", Command: "shell", Status: "running"},
		{TaskID: "task4", TmuxSession: "legato-task4", Command: "shell", Status: "running"},
		{TaskID: "task5", TmuxSession: "legato-task5", Command: "shell", Status: "running"},
	} {
		if err := s.InsertAgentSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")
	s.UpdateAgentActivity(ctx, "task3", "waiting")
	s.UpdateAgentActivity(ctx, "task4", "error")

	working, waiting, errors, idle, err := s.GetAgentActivityCounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 2 {
		t.Errorf("working = %d, want 2", working)
	}
	if waiting != 1 {
		t.Errorf("waiting = %d, want 1", waiting)
	}
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
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

	working, _, _, _, err := s.GetAgentActivityCounts(ctx, "task1")
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

	working, waiting, errors, idle, err := s.GetAgentActivityCounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 0 || waiting != 0 || errors != 0 || idle != 0 {
		t.Errorf("got working=%d waiting=%d errors=%d idle=%d, want all 0", working, waiting, errors, idle)
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

	working, _, _, _, err := s.GetAgentActivityCounts(ctx, "")
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

func TestClearTaskChimeraSessionClearsMatchingTaskAndSessionMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "task-clean", Title: "Clean", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskChimeraSessionID(ctx, "task-clean", "chimera-old"); err != nil {
		t.Fatal(err)
	}
	sessionID, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "task-clean", TmuxSession: "dead", AgentKind: "chimera", Status: "dead"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ?, last_event_id = ? WHERE id = ?`, "chimera-old", "http://old", "event-1", sessionID); err != nil {
		t.Fatal(err)
	}

	cleared, err := s.ClearTaskChimeraSession(ctx, "task-clean", "chimera-old")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("metadata was not cleared")
	}
	task, _ := s.GetTask(ctx, "task-clean")
	if task.ChimeraSessionID != nil {
		t.Fatalf("task ChimeraSessionID = %q", *task.ChimeraSessionID)
	}
	agent, _ := s.GetAgentSessionByID(ctx, sessionID)
	if agent.ChimeraSessionID != nil || agent.ChimeraEndpoint != nil || agent.LastEventID != nil {
		t.Fatalf("agent metadata remains: %+v", agent)
	}
}

func TestClearTaskChimeraSessionRefusesRunningAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "task-running", Title: "Running", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskChimeraSessionID(ctx, "task-running", "chimera-old"); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, AgentSession{TaskID: "task-running", TmuxSession: "live", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearTaskChimeraSession(ctx, "task-running", "chimera-old"); err == nil {
		t.Fatal("expected active agent refusal")
	}
	task, _ := s.GetTask(ctx, "task-running")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chimera-old" {
		t.Fatalf("task ChimeraSessionID = %v", task.ChimeraSessionID)
	}
}

func TestClearTaskChimeraSessionPreservesReplacementSessionAndClearsOldBindings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "task-replaced", Title: "Replaced", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskChimeraSessionID(ctx, "task-replaced", "chimera-new"); err != nil {
		t.Fatal(err)
	}
	sessionID, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "task-replaced", TmuxSession: "dead", AgentKind: "chimera", Status: "dead"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ?, last_event_id = ? WHERE id = ?`, "chimera-old", "http://old", "event-1", sessionID); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.ClearTaskChimeraSession(ctx, "task-replaced", "chimera-old")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("old agent binding was not cleared")
	}
	task, _ := s.GetTask(ctx, "task-replaced")
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID != "chimera-new" {
		t.Fatalf("task ChimeraSessionID = %v", task.ChimeraSessionID)
	}
	agent, _ := s.GetAgentSessionByID(ctx, sessionID)
	if agent.ChimeraSessionID != nil || agent.ChimeraEndpoint != nil || agent.LastEventID != nil {
		t.Fatalf("old agent binding remains: %+v", agent)
	}
}
