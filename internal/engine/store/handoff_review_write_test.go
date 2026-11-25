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

// A handoff's loop that holds claims (converted from before the loop
// record) closes once its handoff has, so its pull requests are free; the
// loop of a handoff still open keeps them.
func TestTheLoopOfAClosedHandoffIsClosedAndFreesItsClaims(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"h-1", "h-2"} {
		card := "card-" + id
		h := Handoff{MainInstanceID: "main-1", HandoffID: id, TaskID: card, Purpose: "make_change", State: HandoffInProgress,
			CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}
		if err := s.CreateHandoffCard(ctx, Task{ID: card, Title: card, Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}, nil, h); err != nil {
			t.Fatal(err)
		}
		pr := 7
		if id == "h-2" {
			pr = 8
		}
		if _, _, err := s.OpenReviewLoop(ctx, ReviewLoopOpen{Loop: ReviewLoop{OwnerKind: ReviewLoopOwnerHandoff, HandoffID: id, MainInstanceID: "main-1",
			HostTask: card, Repo: "acme/widget", Phase: "waiting_for_author"}, PRs: []ReviewLoopPR{{Number: pr}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CloseHandoffAndArchive(ctx, "main-1", "h-1", "2024-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CloseReviewLoopsOfClosedHandoffs(ctx, "2024-01-02T00:00:01Z")
	if err != nil || closed != 1 {
		t.Fatalf("closed %d, %v; want 1", closed, err)
	}
	if held, _ := s.ActiveReviewLoop(ctx, "acme/widget", 7); held != nil {
		t.Fatalf("#7 is still claimed by %s", held.ID)
	}
	loop, _ := s.CurrentReviewLoop(ctx, "card-h-1")
	if loop.State != ReviewLoopClosedState || loop.Phase != "waiting_for_author" {
		t.Fatalf("loop = %+v; want it closed, its phase kept", loop)
	}
	if held, _ := s.ActiveReviewLoop(ctx, "acme/widget", 8); held == nil {
		t.Fatal("the open handoff's loop lost its claim on #8")
	}
}

// openedRun is a run whose github.pr.review.open step opened a loop and
// succeeded, handing it to the run's later steps.
func openedRun(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "run-task")
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: "wr-1", WorkflowName: "review-loop", WorkflowTitle: "Review", DefinitionSource: "test",
		DefinitionDigest: "1", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "open", TaskID: "run-task"},
		[]WorkflowStep{{RunID: "wr-1", StepID: "open", Title: "Open", ExecutorType: ReviewLoopOpenStepType, DefinitionJSON: `{}`, Status: "pending"},
			{RunID: "wr-1", StepID: "close", Title: "Close", ExecutorType: "github.pr.review.close", DefinitionJSON: `{}`, Status: "pending", StepOrder: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartWorkflowStepAttempt(ctx, WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "open", AttemptNumber: 1,
		ExecutorType: ReviewLoopOpenStepType, TaskID: "run-task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenReviewLoop(ctx, ReviewLoopOpen{Loop: ReviewLoop{OwnerKind: ReviewLoopOwnerRun, RunID: "wr-1", StepPath: "open",
		AttemptID: "wa-1", HostTask: "run-task", Repo: "acme/widget"}, PRs: []ReviewLoopPR{{Number: 7}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteWorkflowStep(ctx, "wr-1", "open", "wa-1", `{}`, "close"); err != nil {
		t.Fatal(err)
	}
	reviewing(t, s, "run-task")
	return s
}

func TestALoopAnOpenStepHandedOnIsOwnedWhileItsRunRuns(t *testing.T) {
	s := openedRun(t)
	ctx := context.Background()
	if !s.ReviewLoopOpenedByStep(ctx, "wa-1") {
		t.Fatal("the loop's attempt is an open step's")
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, runOwner); err != nil {
		t.Fatalf("check = %v; want the write allowed while the run runs", err)
	}
	if err := s.CancelWorkflowRun(ctx, "wr-1", "not needed"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHandoffReviewWrite(ctx, "run-task", 1, runOwner); !errors.Is(err, ErrReviewLoopNotOwned) {
		t.Fatalf("check after the run was cancelled = %v; want not owned", err)
	}
}

func TestAnInlineStepsLoopIsNotOpenedByStep(t *testing.T) {
	s := reviewedRun(t)
	if s.ReviewLoopOpenedByStep(context.Background(), "wa-1") {
		t.Fatal("the inline step owns its loop while it waits")
	}
}
