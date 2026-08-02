package store

import (
	"context"
	"errors"
	"testing"
)

func TestReflectionRequestSubmissionIsAtomicAndReplaySafe(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessionID := 42
	req := LearningReflectionRequest{ID: "lrq-1", WorkflowRunID: "run-1", StepID: "reflect", AttemptID: "attempt-1", TaskID: "TASK-1", AgentSessionID: &sessionID, SchemaVersion: 1, Prompt: "reflect", Status: ReflectionRequestPending, ObservationsJSON: "[]"}
	got, err := s.CreateLearningReflectionRequest(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != req.ID || got.Status != ReflectionRequestPending {
		t.Fatalf("request = %+v", got)
	}

	evidence := []LearningEvidence{{ID: "lev-1", SourceKind: "agent_reflection", SourceRef: "reflection:lrq-1:0", Content: "The API requires an idempotency key.", ContentDigest: "sha256:x", Author: "agent_session:42", TaskID: "TASK-1", AgentSessionID: &sessionID, WorkflowRunID: "run-1", MetadataJSON: `{}`}}
	replayed, err := s.SubmitLearningReflection(ctx, req.ID, `[{"kind":"technical_fact","content":"The API requires an idempotency key."}]`, evidence)
	if err != nil || replayed {
		t.Fatalf("first submission replayed=%v err=%v", replayed, err)
	}
	replayed, err = s.SubmitLearningReflection(ctx, req.ID, `[{"kind":"technical_fact","content":"The API requires an idempotency key."}]`, evidence)
	if err != nil || !replayed {
		t.Fatalf("replay replayed=%v err=%v", replayed, err)
	}
	if _, err := s.SubmitLearningReflection(ctx, req.ID, `[{"kind":"other","content":"different"}]`, evidence); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error = %v, want conflict", err)
	}

	stored, err := s.GetLearningReflectionRequest(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReflectionRequestSubmitted || stored.SubmittedAt == nil {
		t.Fatalf("stored request = %+v", stored)
	}
	var count int
	if err := s.DB().Get(&count, `SELECT count(*) FROM learning_evidence WHERE source_kind = 'agent_reflection' AND source_ref = 'reflection:lrq-1:0'`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("evidence count = %d, want 1", count)
	}
}

func TestMarkReflectionUnavailableClosesPendingRequest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := LearningReflectionRequest{ID: "lrq-unavailable", WorkflowRunID: "run", StepID: "step", AttemptID: "attempt-unavailable", TaskID: "task", SchemaVersion: 1, Prompt: "prompt", Status: ReflectionRequestPending, ObservationsJSON: "[]"}
	if _, err := s.CreateLearningReflectionRequest(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReflectionUnavailable(ctx, &req, "delivery failed"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetLearningReflectionRequest(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReflectionRequestUnavailable || stored.Error != "delivery failed" {
		t.Fatalf("request = %+v", stored)
	}
	if _, err := s.SubmitLearningReflection(ctx, req.ID, `{}`, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("submission error = %v, want conflict", err)
	}
	if err := s.MarkReflectionUnavailable(ctx, &req, "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second mark error = %v, want conflict", err)
	}
}

func TestCreateReflectionRequestIsIdempotentByAttempt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := LearningReflectionRequest{ID: "lrq-1", WorkflowRunID: "run", StepID: "step", AttemptID: "attempt", TaskID: "task", SchemaVersion: 1, Prompt: "prompt", Status: ReflectionRequestPending, ObservationsJSON: "[]"}
	if _, err := s.CreateLearningReflectionRequest(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "lrq-2"
	got, err := s.CreateLearningReflectionRequest(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID {
		t.Fatalf("idempotent create ID = %q, want %q", got.ID, first.ID)
	}
	second.Prompt = "changed"
	if _, err := s.CreateLearningReflectionRequest(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed request error = %v, want conflict", err)
	}
}
