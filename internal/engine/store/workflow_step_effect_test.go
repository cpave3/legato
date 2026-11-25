package store

import (
	"context"
	"testing"
	"time"
)

// A step that makes something outside Legato (a ticket) records it, so a
// retry of the step finds it instead of making it again (LEG-640).
func TestAStepEffectIsBegunOnceAndFinished(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	started := time.Date(2026, 11, 25, 8, 0, 0, 123_000_000, time.UTC)

	first, begun, err := s.BeginWorkflowStepEffect(ctx, WorkflowStepEffect{RunID: "wr-1", Path: "prepare", Effect: "ticket.create",
		AttemptID: "wa-1", Provider: "jira", Target: "AG", Summary: "Login fails", StartedAt: started})
	if err != nil || !begun {
		t.Fatalf("begin: %v, begun %v", err, begun)
	}
	if first.State != WorkflowEffectPending || !first.StartedAt.Equal(started) {
		t.Fatalf("first = %+v", first)
	}

	again, begun, err := s.BeginWorkflowStepEffect(ctx, WorkflowStepEffect{RunID: "wr-1", Path: "prepare", Effect: "ticket.create",
		AttemptID: "wa-2", Provider: "burnlite", Target: "LEG", Summary: "Other", StartedAt: started.Add(time.Hour)})
	if err != nil || begun {
		t.Fatalf("second begin: %v, begun %v", err, begun)
	}
	if again.AttemptID != "wa-1" || again.Provider != "jira" || again.Target != "AG" || again.Summary != "Login fails" {
		t.Fatalf("second begin gave %+v; want the first record", again)
	}

	if err := s.FinishWorkflowStepEffect(ctx, "wr-1", "prepare", "ticket.create", "AG-12", "10042", "https://jira/browse/AG-12"); err != nil {
		t.Fatal(err)
	}
	done, err := s.GetWorkflowStepEffect(ctx, "wr-1", "prepare", "ticket.create")
	if err != nil {
		t.Fatal(err)
	}
	if done == nil || done.State != WorkflowEffectDone || done.Key != "AG-12" || done.RemoteID != "10042" || done.URL != "https://jira/browse/AG-12" {
		t.Fatalf("done = %+v", done)
	}

	// Another effect of the same step, and the same effect of another
	// step, are their own.
	if _, begun, _ := s.BeginWorkflowStepEffect(ctx, WorkflowStepEffect{RunID: "wr-1", Path: "prepare", Effect: "ticket.comment", AttemptID: "wa-1", Provider: "jira", StartedAt: started}); !begun {
		t.Fatal("another effect of the step was not begun")
	}
	if _, begun, _ := s.BeginWorkflowStepEffect(ctx, WorkflowStepEffect{RunID: "wr-1", Path: "review", Effect: "ticket.create", AttemptID: "wa-3", Provider: "jira", StartedAt: started}); !begun {
		t.Fatal("the effect of another step was not begun")
	}
}

// A refusal made nothing: the record goes, and the next attempt begins
// afresh.
func TestADroppedStepEffectCanBeBegunAgain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	effect := WorkflowStepEffect{RunID: "wr-1", Path: "prepare", Effect: "ticket.create", AttemptID: "wa-1", Provider: "jira", StartedAt: time.Now()}
	if _, _, err := s.BeginWorkflowStepEffect(ctx, effect); err != nil {
		t.Fatal(err)
	}
	if err := s.DropWorkflowStepEffect(ctx, "wr-1", "prepare", "ticket.create"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetWorkflowStepEffect(ctx, "wr-1", "prepare", "ticket.create"); err != nil || got != nil {
		t.Fatalf("after drop: %+v, %v", got, err)
	}
	effect.AttemptID = "wa-2"
	if got, begun, err := s.BeginWorkflowStepEffect(ctx, effect); err != nil || !begun || got.AttemptID != "wa-2" {
		t.Fatalf("begin after drop: %+v, %v, %v", got, begun, err)
	}
}
