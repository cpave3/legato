package store

import (
	"context"
	"errors"
	"testing"
)

// reviewedRun is a run task whose review step waits, with its loop
// reviewing pass 1, submitted.
func reviewedRun(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "run-task")
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "wr-1", WorkflowName: "review-pr-passes", WorkflowTitle: "Review", DefinitionSource: "test",
		DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "review", TaskID: "run-task"},
		[]WorkflowStep{{RunID: "wr-1", StepID: "review", Title: "Review", ExecutorType: "github.pr.review_passes", DefinitionJSON: `{}`, Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "review", AttemptNumber: 1,
		ExecutorType: "github.pr.review_passes", TaskID: "run-task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitWorkflowStepOnService(ctx, "wr-1", "review", "wa-1"); err != nil {
		t.Fatal(err)
	}
	// The step's loop, owned by its run attempt.
	if _, _, err := s.OpenReviewLoop(ctx, ReviewLoopOpen{Loop: ReviewLoop{OwnerKind: ReviewLoopOwnerRun, RunID: "wr-1", StepPath: "review",
		AttemptID: "wa-1", HostTask: "run-task", Repo: "acme/widget", MarkerKey: "run-task"}, PRs: []ReviewLoopPR{{Number: 7}}}); err != nil {
		t.Fatal(err)
	}
	reviewing(t, s, "run-task")
	return s
}

// reviewing gives a card's loop pass 1, submitted.
func reviewing(t *testing.T, s *Store, taskID string) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveHandoffReview(ctx, HandoffReview{TaskID: taskID, Settings: "{}", Phase: "reviewing", Pass: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertHandoffReviewPass(ctx, HandoffReviewPass{TaskID: taskID, Pass: 1, State: "submitted"}, nil); err != nil {
		t.Fatal(err)
	}
}

var runOwner = HandoffReviewOwner{AttemptID: "wa-1"}

func TestAGitHubWriteOfAnOpenLoopItsRunAttemptStillOwnsIsAllowed(t *testing.T) {
	s := reviewedRun(t)
	if err := s.CheckHandoffReviewWrite(context.Background(), "run-task", 1, runOwner); err != nil {
		t.Fatalf("check = %v; want the write allowed", err)
	}
}

func TestAGitHubWriteOfACancelledRunsLoopIsNotOwned(t *testing.T) {
	s := reviewedRun(t)
	ctx := context.Background()
	if err := s.CancelWorkflowRun(ctx, "wr-1", "not needed"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, runOwner); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("check = %v; want not owned", err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, HandoffReviewOwner{AttemptID: "wa-2"}); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("check of another attempt = %v; want not owned", err)
	}
}

func TestAGitHubWriteOfAnEndedLoopOrASetAsidePassIsRefused(t *testing.T) {
	s := reviewedRun(t)
	ctx := context.Background()
	if _, err := s.EndHandoffReview(ctx, "run-task", "cancelled", "ended", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, runOwner); !errors.Is(err, ErrReviewLoopClosed) {
		t.Fatalf("check = %v; want closed", err)
	}
	// The loop open, its pass set aside: an answer may post, the pass not.
	if err := s.SaveHandoffReview(ctx, HandoffReview{TaskID: "run-task", Settings: "{}", Phase: "waiting_for_author", Pass: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, runOwner); !errors.Is(err, ErrReviewPassNotSubmitted) {
		t.Fatalf("check of the pass = %v; want it not submitted", err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 0, runOwner); err != nil {
		t.Fatalf("check of an answer = %v; want it allowed", err)
	}
}

func TestAGitHubWriteOfAHandoffsLoopNeedsTheHandoffStillOpenHere(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := Handoff{MainInstanceID: "main-1", HandoffID: "h-1", TaskID: "card", Purpose: "make_change", State: HandoffInProgress,
		CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}
	if err := s.CreateHandoffCard(ctx, Task{ID: "card", Title: "card", Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}, nil, h); err != nil {
		t.Fatal(err)
	}
	reviewing(t, s, "card")
	owner := HandoffReviewOwner{MainInstanceID: "main-1", HandoffID: "h-1"}
	if err := s.CheckHandoffReviewWrite(ctx, "card", 1, owner); err != nil {
		t.Fatalf("check = %v; want the write allowed", err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "card", 1, HandoffReviewOwner{MainInstanceID: "main-1", HandoffID: "h-2"}); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("check of another handoff = %v; want not owned", err)
	}
	if _, err := s.MarkHandoffReturning(ctx, "main-1", "h-1", "2024-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "card", 1, owner); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("check while it returns = %v; want not owned", err)
	}
}
