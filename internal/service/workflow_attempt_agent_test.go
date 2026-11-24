package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// An interview borrows another task's agent: its attempt keeps the run's
// task, so the run page needs the agent's own ID to link to it.
func TestShowNamesTheAgentAnInterviewBorrows(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTask(t, db, "run-task")
	createTask(t, db, "LEG-2")
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "LEG-2", TmuxSession: "legato-LEG-2", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetAgentSessionByTaskID(ctx, "LEG-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateWorkflowRun(ctx, store.WorkflowRun{ID: "wr-1", WorkflowName: "learn", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "ask", TaskID: "run-task"},
		[]store.WorkflowStep{{RunID: "wr-1", StepID: "ask", Title: "Ask", ExecutorType: "agent.interview", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "ask", AttemptNumber: 1, ExecutorType: "agent.interview", TaskID: "run-task", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkWorkflowStepWaiting(ctx, "wr-1", "ask", "wa-1", session, "", ""); err != nil {
		t.Fatal(err)
	}

	view, err := NewWorkflowService(db, nil).Show(ctx, "wr-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Attempts[0]; got.TaskID != "run-task" || got.AgentID != "LEG-2" {
		t.Fatalf("attempt = task %q, agent %q; want task run-task, agent LEG-2", got.TaskID, got.AgentID)
	}
}
