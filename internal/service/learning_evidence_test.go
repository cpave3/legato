package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestPrepareLearningPacketCollectsAuthoredPlanAndReviewEvidenceIdempotently(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	createServiceTestTask(t, db, "TASK-EVIDENCE")

	plan := store.Plan{ID: "plan-1", TaskID: "TASK-EVIDENCE", Name: "default", Title: "Plan", Status: "draft", LatestRevision: 1}
	revision := store.PlanRevision{ID: "prev-1", PlanID: plan.ID, Revision: 1, Markdown: "generated plan body", ManifestJSON: `{}`}
	question := store.PlanQuestion{ID: "pq-1", PlanID: plan.ID, RevisionID: revision.ID, Key: "choice", Kind: "free_text", Prompt: "generated prompt", OptionsJSON: `[]`, RecommendedJSON: `[]`}
	if err := db.InsertPlanRevision(ctx, plan, revision, []store.PlanQuestion{question}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertPlanComment(ctx, store.PlanComment{ID: "pc-1", PlanID: plan.ID, RevisionID: revision.ID, Body: "Keep the API backwards compatible."}); err != nil {
		t.Fatal(err)
	}
	if err := db.SubmitPlanComments(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertPlanResponse(ctx, store.PlanResponse{ID: "pres-1", PlanID: plan.ID, RevisionID: revision.ID, QuestionID: question.ID, Text: "Use a compatibility shim."}); err != nil {
		t.Fatal(err)
	}

	tour, err := db.EnsureReviewTour(ctx, "TASK-EVIDENCE", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RestartReviewTourWithGuidance(ctx, tour.ID, "Add a regression test."); err != nil {
		t.Fatal(err)
	}
	pass, err := db.GetActiveReviewPass(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	questionMessage, err := db.InsertReviewMessage(ctx, store.ReviewMessage{TaskID: "TASK-EVIDENCE", TourID: tour.ID, PassID: pass.ID, StepID: "step-1", Kind: "question", Author: "reviewer:42", Body: "What protects old clients?"}, true)
	if err != nil {
		t.Fatal(err)
	}
	answerMessage, err := db.InsertReviewMessage(ctx, store.ReviewMessage{TaskID: "TASK-EVIDENCE", TourID: tour.ID, PassID: pass.ID, StepID: "step-1", Kind: "answer", Author: "agent:session-7", Body: "The compatibility test and shim."}, true)
	if err != nil {
		t.Fatal(err)
	}

	svc := NewLearningService(db, nil)
	input := PrepareLearningPacketInput{AttemptID: "collect-1", TaskID: "TASK-EVIDENCE", ProjectKey: "repo:acme/widget", ProjectLabel: "acme/widget", MaxItems: 20, MaxBytes: 128 * 1024}
	packet, err := svc.PrepareLearningPacket(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Packet.Evidence) != 5 {
		t.Fatalf("evidence = %+v", packet.Packet.Evidence)
	}

	want := map[string]struct{ ref, author, content string }{
		"plan_comment":    {"pc-1", "user", "Keep the API backwards compatible."},
		"plan_response":   {"pres-1", "user", "Use a compatibility shim."},
		"review_guidance": {pass.ID, "user", "Add a regression test."},
		"review_question": {stringID(questionMessage.ID), "reviewer:42", "What protects old clients?"},
		"review_answer":   {stringID(answerMessage.ID), "agent:session-7", "The compatibility test and shim."},
	}
	for _, evidence := range packet.Packet.Evidence {
		expected, ok := want[evidence.SourceKind]
		if !ok {
			t.Fatalf("unexpected generated/transcript evidence: %+v", evidence)
		}
		if evidence.SourceRef != expected.ref || evidence.Author != expected.author || evidence.Content != expected.content || evidence.ProjectKey != input.ProjectKey {
			t.Fatalf("evidence provenance = %+v, want %+v", evidence, expected)
		}
		delete(want, evidence.SourceKind)
	}
	if len(want) != 0 {
		t.Fatalf("missing evidence kinds: %v", want)
	}

	if err := svc.CollectTaskEvidence(ctx, input.TaskID, &ProjectIdentity{Key: input.ProjectKey, Label: input.ProjectLabel}); err != nil {
		t.Fatal(err)
	}
	stored, err := db.ListUnlinkedLearningEvidence(ctx, input.TaskID, input.ProjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 5 {
		t.Fatalf("recollection created duplicates: %+v", stored)
	}
}

func TestCollectTaskEvidenceBoundsTextAndSkipsDraftPlanComments(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	createServiceTestTask(t, db, "TASK-BOUND")
	plan := store.Plan{ID: "plan-bound", TaskID: "TASK-BOUND", Name: "default", Status: "draft", LatestRevision: 1}
	revision := store.PlanRevision{ID: "prev-bound", PlanID: plan.ID, Revision: 1, ManifestJSON: `{}`}
	question := store.PlanQuestion{ID: "question-long", PlanID: plan.ID, RevisionID: revision.ID, Key: "long", Kind: "free_text", OptionsJSON: `[]`, RecommendedJSON: `[]`}
	if err := db.InsertPlanRevision(ctx, plan, revision, []store.PlanQuestion{question}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertPlanComment(ctx, store.PlanComment{ID: "draft", PlanID: plan.ID, RevisionID: revision.ID, Body: "not submitted"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertPlanResponse(ctx, store.PlanResponse{ID: "long", PlanID: plan.ID, RevisionID: revision.ID, QuestionID: "question-long", Text: strings.Repeat("é", MaxCollectedEvidenceBytes)}); err != nil {
		t.Fatal(err)
	}

	svc := NewLearningService(db, nil)
	if err := svc.CollectTaskEvidence(ctx, "TASK-BOUND", nil); err != nil {
		t.Fatal(err)
	}
	evidence, err := db.ListUnlinkedLearningEvidence(ctx, "TASK-BOUND", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].SourceRef != "long" || len(evidence[0].Content) > MaxCollectedEvidenceBytes {
		t.Fatalf("bounded evidence = %+v", evidence)
	}
}

func stringID(id int) string {
	return fmt.Sprint(id)
}
