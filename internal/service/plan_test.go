package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestSubmitPlanBundleSnapshotsFilesAndAppearsInQueue(t *testing.T) {
	s, svc, bundle := newPlanFixture(t)
	view, err := svc.Submit(context.Background(), "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if view.Plan.Status != "proposed" || view.Plan.LatestRevision != 1 {
		t.Fatalf("plan = %+v", view.Plan)
	}
	if view.Revision.Markdown != "# Add search\n\nImplement indexed search.\n" {
		t.Fatalf("markdown = %q", view.Revision.Markdown)
	}
	if len(view.Questions) != 1 || view.Questions[0].Key != "backend" {
		t.Fatalf("questions = %+v", view.Questions)
	}

	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.Plan(context.Background(), view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision.Markdown != "# Add search\n\nImplement indexed search.\n" {
		t.Fatalf("stored markdown changed with source file: %q", stored.Revision.Markdown)
	}

	queue, err := svc.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].PlanID != view.Plan.ID || queue[0].UnansweredRequired != 1 {
		t.Fatalf("queue = %+v", queue)
	}
	_ = s
}

func TestPlanApprovalRequiresAnswersAndPublishesVerdict(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	bus := events.New()
	svc.bus = bus
	changes := bus.Subscribe(events.EventPlanChanged)
	view, err := svc.Submit(context.Background(), "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(context.Background(), view.Plan.ID); !errors.Is(err, ErrPlanQuestionsUnanswered) {
		t.Fatalf("approve error = %v", err)
	}
	if err := svc.Respond(context.Background(), view.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(context.Background(), view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	approved, err := svc.Plan(context.Background(), view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Plan.Status != "approved" || approved.Plan.ApprovedAt == nil {
		t.Fatalf("plan = %+v", approved.Plan)
	}
	select {
	case event := <-changes:
		payload := event.Payload.(events.PlanChangedPayload)
		if payload.PlanID != view.Plan.ID {
			t.Fatalf("payload = %+v", payload)
		}
	default:
		t.Fatal("submit did not publish plan change")
	}
}

func TestRequestChangesSubmitsAnchoredCommentsAndResubmissionCreatesRevision(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	ctx := context.Background()
	view, err := svc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(view.Revision.Markdown, "indexed search")
	comment, err := svc.AddComment(ctx, view.Plan.ID, PlanCommentInput{
		Body: "Use FTS terminology", SelectionStart: &start, SelectionEnd: intPtr(start + len("indexed search")), SelectedText: "indexed search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestChanges(ctx, view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.Plan(ctx, view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Plan.Status != "changes_requested" || len(changed.Comments) != 1 || changed.Comments[0].ID != comment.ID || changed.Comments[0].SubmittedAt == nil {
		t.Fatalf("changed plan = %+v comments=%+v", changed.Plan, changed.Comments)
	}

	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("# Add search\n\nUse SQLite FTS.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	revised, err := svc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Plan.LatestRevision != 2 || revised.Plan.Status != "proposed" || revised.Revision.Revision != 2 {
		t.Fatalf("revised = %+v", revised)
	}
	history, err := svc.Revisions(ctx, view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("history = %+v", history)
	}
}

func TestUpdatePlanCommentPreservesAnchorAndSubmittedState(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	ctx := context.Background()
	view, err := svc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(view.Revision.Markdown, "indexed search")
	comment, err := svc.AddComment(ctx, view.Plan.ID, PlanCommentInput{Body: "Original", SelectionStart: &start, SelectionEnd: intPtr(start + len("indexed search")), SelectedText: "indexed search"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestChanges(ctx, view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateComment(ctx, view.Plan.ID, comment.ID, "Updated body")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != "Updated body" || updated.SelectionStart == nil || *updated.SelectionStart != start || updated.SubmittedAt == nil {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := svc.UpdateComment(ctx, "other-plan", comment.ID, "Cross plan"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-plan error = %v", err)
	}
	if _, err := svc.UpdateComment(ctx, view.Plan.ID, comment.ID, "  "); err == nil {
		t.Fatal("empty update succeeded")
	}
}

func TestCompleteApprovedPlanKeepsOrCleansSourceBundle(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup=%t", cleanup), func(t *testing.T) {
			_, svc, bundle := newPlanFixture(t)
			ctx := context.Background()
			view, err := svc.Submit(ctx, "task-1", "search", bundle)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Respond(ctx, view.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
				t.Fatal(err)
			}
			if err := svc.Approve(ctx, view.Plan.ID, cleanup); err != nil {
				t.Fatal(err)
			}
			result, err := svc.Complete(ctx, view.Plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "completed" || result.CleanedUp != cleanup {
				t.Fatalf("result = %+v", result)
			}
			_, statErr := os.Stat(bundle)
			if cleanup && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("bundle still exists: %v", statErr)
			}
			if !cleanup && statErr != nil {
				t.Fatalf("bundle removed: %v", statErr)
			}
			stored, err := svc.Plan(ctx, view.Plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Plan.Status != "completed" || stored.Plan.CompletedAt == nil || stored.Plan.CleanupAfterImplementation != cleanup {
				t.Fatalf("stored = %+v", stored.Plan)
			}
			again, err := svc.Complete(ctx, view.Plan.ID)
			if err != nil || !again.AlreadyCompleted {
				t.Fatalf("repeat complete = %+v, %v", again, err)
			}
		})
	}
}

func TestCompleteRefusesUnsafeOrInvalidBundle(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	ctx := context.Background()
	view, err := svc.Submit(ctx, "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Respond(ctx, view.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, view.Plan.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bundle, "plan.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(ctx, view.Plan.ID); err == nil || !strings.Contains(err.Error(), "plan.json") {
		t.Fatalf("complete error = %v", err)
	}
	stored, err := svc.Plan(ctx, view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Plan.Status != "approved" || stored.Plan.CompletedAt != nil {
		t.Fatalf("invalid bundle marked completed: %+v", stored.Plan)
	}
}

func TestPlanQuestionAndAgentAnswerRemainInTranscriptWhenOffline(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	view, err := svc.Submit(context.Background(), "task-1", "search", bundle)
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := svc.AskQuestion(context.Background(), view.Plan.ID, "Could this avoid a new dependency?")
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("ask error = %v", err)
	}
	if err := svc.Answer(context.Background(), view.Plan.ID, threadID, "Yes, by using SQLite FTS."); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Plan(context.Background(), view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Messages) != 2 || updated.Messages[0].DeliveredAt != nil || updated.Messages[1].Author != "agent" {
		t.Fatalf("messages = %+v", updated.Messages)
	}
}

func TestSubmitWithOriginLinksPlanToReviewFindings(t *testing.T) {
	s, svc, bundle := newPlanFixture(t)
	ctx := context.Background()
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	reviewSvc := NewReviewService(s, nil, nil)
	finding, err := reviewSvc.CreateFinding(ctx, tour.ID, ReviewFindingInput{Body: "Fix validation"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.SubmitWithOrigin(ctx, "task-1", "search", bundle, PlanOriginInput{
		ReviewPassID: finding.PassID, FindingIDs: []string{finding.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Origins) != 1 || view.Origins[0].Finding.ID != finding.ID || view.Origins[0].ReviewPassID != finding.PassID {
		t.Fatalf("origins = %+v", view.Origins)
	}
}

func TestCompleteLinkedPlanResolvesFindings(t *testing.T) {
	s, svc, bundle := newPlanFixture(t)
	ctx := context.Background()
	repo := initTestRepo(t)
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	reviewSvc := NewReviewService(s, nil, nil)
	finding, err := reviewSvc.CreateFinding(ctx, tour.ID, ReviewFindingInput{Body: "Fix validation"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.SubmitWithOrigin(ctx, "task-1", "search", bundle, PlanOriginInput{ReviewPassID: finding.PassID, FindingIDs: []string{finding.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Respond(ctx, view.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(ctx, view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	review, err := reviewSvc.Tour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved := review.Passes[0].Findings[0]
	if resolved.Status != "resolved" || resolved.ResolvedAt == nil {
		t.Fatalf("finding after completion = %+v", resolved)
	}
}

func newPlanFixture(t *testing.T) (*store.Store, *PlanService, string) {
	t.Helper()
	s := newReviewTestStore(t)
	createTask(t, s, "task-1")
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("# Add search\n\nImplement indexed search.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema_version": 1,
		"title": "Add search",
		"summary": "Introduce indexed search",
		"questions": [{
			"id": "backend",
			"kind": "single_choice",
			"prompt": "Which backend?",
			"required": true,
			"options": [{"id":"sqlite","label":"SQLite"},{"id":"bleve","label":"Bleve"}],
			"recommended_options": ["sqlite"]
		}]
	}`
	if err := os.WriteFile(filepath.Join(bundle, "plan.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return s, NewPlanService(s, nil, nil), bundle
}

func intPtr(value int) *int { return &value }
