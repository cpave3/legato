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
	steps := []WorkflowStep{{RunID: run.ID, StepID: "agent", StepOrder: 0, Title: "Agent", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"}}
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

// A scratch folder is in use while its attempt runs, or while the agent it
// kept does (LEG-530).
func TestWorkflowAttemptInUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "workflow-task")
	run := WorkflowRun{ID: "wr-1", WorkflowName: "test", WorkflowTitle: "Test", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "agent", TaskID: "workflow-task"}
	if err := s.CreateWorkflowRun(ctx, run, []WorkflowStep{{RunID: run.ID, StepID: "agent", Title: "Agent", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: run.ID, StepID: "agent", AttemptNumber: 1, ExecutorType: "agent.run", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"wa-1": true, "wa-gone": false} {
		if used, err := s.WorkflowAttemptInUse(ctx, id); err != nil || used != want {
			t.Fatalf("%s in use = %v, %v; want %v", id, used, err, want)
		}
	}
	if err := s.CompleteWorkflowStep(ctx, run.ID, "agent", "wa-1", `{}`, "after"); err != nil {
		t.Fatal(err)
	}
	if used, err := s.WorkflowAttemptInUse(ctx, "wa-1"); err != nil || used {
		t.Fatalf("finished attempt in use = %v, %v", used, err)
	}
}
