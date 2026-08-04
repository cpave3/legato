package store

import (
	"context"
	"testing"
)

func TestListActiveWorkflowAgentAttemptsByTaskTargetsWaitingAgentAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "workflow-task")
	run := WorkflowRun{ID: "wr-1", WorkflowName: "test", WorkflowTitle: "Test", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "agent", TaskID: "workflow-task"}
	steps := []WorkflowStepRun{{RunID: run.ID, StepID: "agent", StepOrder: 0, Title: "Agent", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"}}
	if err := s.CreateWorkflowRun(ctx, run, steps); err != nil {
		t.Fatal(err)
	}
	attempt := WorkflowStepAttempt{ID: "wa-1", RunID: run.ID, StepID: "agent", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "workflow-task", Status: "waiting_agent"}
	if err := s.StartWorkflowStepAttempt(ctx, attempt, `{}`); err != nil {
		t.Fatal(err)
	}
	attempts, err := s.ListActiveWorkflowAgentAttemptsByTask(ctx, "workflow-task")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].RunID != "wr-1" || attempts[0].StepID != "agent" {
		t.Fatalf("attempts=%+v", attempts)
	}
	unrelated, err := s.ListActiveWorkflowAgentAttemptsByTask(ctx, "other")
	if err != nil || len(unrelated) != 0 {
		t.Fatalf("unrelated=%+v err=%v", unrelated, err)
	}
}
