package store

import (
	"context"
	"errors"
	"testing"
)

// waitingAgentFixture is wr-1 on its agent.run step "review", whose
// attempt wa-2 waits on agent session 7.
func waitingAgentFixture(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "prepare", AttemptNumber: 1, ExecutorType: "github.pr.prepare", TaskID: "wf-task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteWorkflowStep(ctx, "wr-1", "prepare", "wa-1", `{}`, "review"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-2", RunID: "wr-1", StepID: "review", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "wf-task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkflowStepWaiting(ctx, "wr-1", "review", "wa-2", &AgentSession{ID: 7, TmuxSession: "legato-wf-task", AgentKind: "claude-code"}, "/work", ""); err != nil {
		t.Fatal(err)
	}
}

func workflowStatuses(t *testing.T, s *Store) (run, step, attempt string, view *WorkflowRunView) {
	t.Helper()
	view, err := s.GetWorkflowRun(context.Background(), "wr-1")
	if err != nil {
		t.Fatal(err)
	}
	return view.Run.Status, view.Steps[1].Status, view.Attempts[1].Status, view
}

func TestASuspendedAgentStepResumesOnItsSameAttemptWithTheNewSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	waitingAgentFixture(t, s)

	if err := s.SuspendWorkflowStep(ctx, "wr-1", "review", "wa-2", "Legato stopped while the agent was working"); err != nil {
		t.Fatalf("SuspendWorkflowStep: %v", err)
	}
	run, step, attempt, view := workflowStatuses(t, s)
	if run != WorkflowSuspended || step != WorkflowSuspended || attempt != WorkflowSuspended {
		t.Fatalf("after suspend: run %s, step %s, attempt %s; want all suspended", run, step, attempt)
	}
	if view.Run.Error != "Legato stopped while the agent was working" || view.Attempts[1].EndedAt != nil {
		t.Fatalf("suspended run error %q, attempt ended %v", view.Run.Error, view.Attempts[1].EndedAt)
	}
	suspended, err := s.ListSuspendedWorkflowAttemptsByAgentSession(ctx, 7)
	if err != nil || len(suspended) != 1 || suspended[0].ID != "wa-2" {
		t.Fatalf("suspended attempts of session 7 = %+v, %v", suspended, err)
	}
	if waiting, _ := s.ListWaitingWorkflowAttemptsByAgentSession(ctx, 7); len(waiting) != 0 {
		t.Fatalf("waiting attempts of session 7 = %+v; want none while suspended", waiting)
	}

	if err := s.ResumeWorkflowStep(ctx, "wr-1", "review", "wa-2", AgentSession{ID: 9, TmuxSession: "legato-wf-task", AgentKind: "claude-code"}); err != nil {
		t.Fatalf("ResumeWorkflowStep: %v", err)
	}
	run, step, attempt, view = workflowStatuses(t, s)
	if run != "running" || step != "waiting_agent" || attempt != "waiting_agent" || view.Run.Error != "" {
		t.Fatalf("after resume: run %s (%q), step %s, attempt %s", run, view.Run.Error, step, attempt)
	}
	if got := view.Attempts[1].AgentSessionID; got == nil || *got != 9 {
		t.Fatalf("attempt session = %v; want the new session 9", got)
	}
	if err := s.ResumeWorkflowStep(ctx, "wr-1", "review", "wa-2", AgentSession{ID: 10}); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("second resume = %v; want a state conflict", err)
	}
	// The same attempt completes as any waiting agent step does.
	if err := s.CompleteWorkflowStep(ctx, "wr-1", "review", "wa-2", `{}`, ""); err != nil {
		t.Fatalf("completing the resumed attempt: %v", err)
	}
}

func TestOnlyAWaitingAgentStepIsSuspended(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "prepare", AttemptNumber: 1, ExecutorType: "github.pr.prepare", TaskID: "wf-task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.SuspendWorkflowStep(ctx, "wr-1", "prepare", "wa-1", "stopped"); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("suspending a running step = %v; want a state conflict", err)
	}
}

func TestASuspendedStepCanBeFailedOrCancelled(t *testing.T) {
	for _, end := range []string{"failed", "cancelled"} {
		t.Run(end, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			waitingAgentFixture(t, s)
			if err := s.SuspendWorkflowStep(ctx, "wr-1", "review", "wa-2", "stopped"); err != nil {
				t.Fatal(err)
			}
			var err error
			if end == "failed" {
				err = s.FailWorkflowStep(ctx, "wr-1", "review", "wa-2", "could not resume")
			} else {
				err = s.CancelWorkflowRun(ctx, "wr-1", "cancelled by the operator")
			}
			if err != nil {
				t.Fatal(err)
			}
			run, step, attempt, _ := workflowStatuses(t, s)
			if run != end || step != end || attempt != end {
				t.Fatalf("run %s, step %s, attempt %s; want all %s", run, step, attempt, end)
			}
		})
	}
}

func TestAFailedAgentStepCanBeReopenedAsSuspended(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	waitingAgentFixture(t, s)
	if err := s.FailWorkflowStep(ctx, "wr-1", "review", "wa-2", "agent exited before step completion"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenWorkflowStepSuspended(ctx, "wr-1", "review", "wa-2", "Legato stopped while the agent was working"); err != nil {
		t.Fatalf("ReopenWorkflowStepSuspended: %v", err)
	}
	run, step, attempt, view := workflowStatuses(t, s)
	if run != WorkflowSuspended || step != WorkflowSuspended || attempt != WorkflowSuspended {
		t.Fatalf("run %s, step %s, attempt %s; want all suspended", run, step, attempt)
	}
	if view.Attempts[1].EndedAt != nil || view.Steps[1].CompletedAt != nil || view.Run.CompletedAt != nil {
		t.Fatalf("a reopened step keeps its end times: %+v", view)
	}
	if err := s.ReopenWorkflowStepSuspended(ctx, "wr-1", "review", "wa-2", "again"); !errors.Is(err, ErrWorkflowStateConflict) {
		t.Fatalf("reopening a suspended step = %v; want a state conflict", err)
	}
}
