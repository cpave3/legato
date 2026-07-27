package store

import (
	"context"
	"testing"
)

func TestWorkflowFailureRetryAndArtifactHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "wf-task")
	createWorkflowFixture(t, s)
	attempt := WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "prepare", AttemptNumber: 1, ExecutorType: "github.pr.prepare", TaskID: "wf-task", Status: "running"}
	if err := s.StartWorkflowStepAttempt(ctx, attempt, `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.FailWorkflowStep(ctx, "wr-1", "prepare", "wa-1", "network failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetWorkflowStepForRetry(ctx, "wr-1", "prepare"); err != nil {
		t.Fatal(err)
	}
	attempt.ID, attempt.AttemptNumber = "wa-2", 2
	if err := s.StartWorkflowStepAttempt(ctx, attempt, `{}`); err != nil {
		t.Fatal(err)
	}
	artifact := WorkflowArtifact{ID: "wfa-1", RunID: "wr-1", StepID: "prepare", AttemptID: "wa-2", TaskID: "wf-task", OutputKey: "review", ManagedPath: "/data/review.md", OriginalPath: "review.md", MediaType: "text/markdown", SizeBytes: 12, Digest: "sha256:x"}
	if err := s.InsertWorkflowArtifact(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetWorkflowRun(ctx, "wr-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attempts) != 2 || len(view.Artifacts) != 1 || view.Steps[0].Status != "running" {
		t.Fatalf("view = %+v", view)
	}
}
