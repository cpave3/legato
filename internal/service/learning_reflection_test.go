package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestLearningServiceCreateAndSubmitReflection(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	sessionID := 17
	req, err := svc.CreateReflectionRequest(context.Background(), CreateReflectionRequestInput{WorkflowRunID: "run-1", StepID: "reflect", AttemptID: "attempt-1", TaskID: "TASK-1", AgentSessionID: &sessionID, Prompt: "What should be retained?"})
	if err != nil {
		t.Fatal(err)
	}
	if req.SchemaVersion != ReflectionSchemaVersion || req.Status != "pending" {
		t.Fatalf("request = %+v", req)
	}

	doc := ReflectionDocument{SchemaVersion: ReflectionSchemaVersion, Observations: []ReflectionObservation{{Kind: "technical_fact", Content: "The callback must include the active request ID.", Rationale: "Stale callbacks must not advance work."}}}
	result, err := svc.SubmitReflection(context.Background(), req.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || len(result.Evidence) != 1 {
		t.Fatalf("result = %+v", result)
	}
	e := result.Evidence[0]
	if e.Author != "agent_session:17" || e.TaskID != "TASK-1" || e.WorkflowRunID != "run-1" || e.SourceRef != "reflection:"+req.ID+":0" {
		t.Fatalf("evidence attribution = %+v", e)
	}

	replay, err := svc.SubmitReflection(context.Background(), req.ID, doc)
	if err != nil || !replay.Replayed || replay.Evidence[0].ID != e.ID {
		t.Fatalf("replay = %+v, err = %v", replay, err)
	}
	changed := doc
	changed.Observations = []ReflectionObservation{{Kind: "technical_fact", Content: "different"}}
	if _, err := svc.SubmitReflection(context.Background(), req.ID, changed); !errors.Is(err, ErrReflectionStale) {
		t.Fatalf("changed replay error = %v, want stale", err)
	}
}

func TestLearningServiceSubmitEmptyReflectionCompletesWithoutEvidence(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	req, err := svc.CreateReflectionRequest(ctx, CreateReflectionRequestInput{
		WorkflowRunID: "run-empty", StepID: "reflect", AttemptID: "attempt-empty", TaskID: "task-empty",
	})
	if err != nil {
		t.Fatal(err)
	}

	doc := ReflectionDocument{SchemaVersion: ReflectionSchemaVersion, Observations: []ReflectionObservation{}}
	result, err := svc.SubmitReflection(ctx, req.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Status != "submitted" || len(result.Evidence) != 0 || result.Replayed {
		t.Fatalf("empty reflection result = %+v", result)
	}

	replay, err := svc.SubmitReflection(ctx, req.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || len(replay.Evidence) != 0 {
		t.Fatalf("empty reflection replay = %+v", replay)
	}
}

func TestLearningServiceDefaultsReflectionToFutureUsefulGuidance(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	req, err := svc.CreateReflectionRequest(context.Background(), CreateReflectionRequestInput{
		WorkflowRunID: "run", StepID: "reflect", AttemptID: "attempt-default", TaskID: "task",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, guidance := range []string{
		"future task",
		"module-specific",
		"Do not submit a task recap",
		"learned or discovered",
		"merely requested or implemented",
		"zero observations",
		"future trigger",
	} {
		if !strings.Contains(req.Prompt, guidance) {
			t.Errorf("default reflection prompt missing %q: %q", guidance, req.Prompt)
		}
	}
}

func TestLearningServiceReflectionValidationRejectsMalformedAndBoundedInput(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	newRequest := func(attempt string) string {
		req, err := svc.CreateReflectionRequest(ctx, CreateReflectionRequestInput{WorkflowRunID: "run", StepID: "reflect", AttemptID: attempt, TaskID: "task", Prompt: "prompt"})
		if err != nil {
			t.Fatal(err)
		}
		return req.ID
	}

	tests := []struct {
		name string
		doc  ReflectionDocument
	}{
		{"wrong version", ReflectionDocument{SchemaVersion: 2, Observations: []ReflectionObservation{{Kind: "fact", Content: "x"}}}},
		{"too many", ReflectionDocument{SchemaVersion: 1, Observations: make([]ReflectionObservation, MaxReflectionObservations+1)}},
		{"empty kind", ReflectionDocument{SchemaVersion: 1, Observations: []ReflectionObservation{{Content: "x"}}}},
		{"empty content", ReflectionDocument{SchemaVersion: 1, Observations: []ReflectionObservation{{Kind: "fact"}}}},
		{"oversize content", ReflectionDocument{SchemaVersion: 1, Observations: []ReflectionObservation{{Kind: "fact", Content: strings.Repeat("x", MaxReflectionObservationBytes+1)}}}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := newRequest(tt.name + string(rune('a'+i)))
			if _, err := svc.SubmitReflection(ctx, id, tt.doc); !errors.Is(err, ErrInvalidReflection) {
				t.Fatalf("error = %v, want invalid reflection", err)
			}
			request, err := db.GetLearningReflectionRequest(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if request.Status != "pending" {
				t.Fatalf("request status = %q after invalid submission", request.Status)
			}
		})
	}
}

func TestLearningServiceSubmitReflectionJSONRequiresExplicitObservationArray(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	for i, data := range [][]byte{
		[]byte(`{"schema_version":1}`),
		[]byte(`{"schema_version":1,"observations":null}`),
	} {
		req, err := svc.CreateReflectionRequest(ctx, CreateReflectionRequestInput{WorkflowRunID: "run", StepID: "reflect", AttemptID: fmt.Sprintf("explicit-array-%d", i), TaskID: "task"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SubmitReflectionJSON(ctx, req.ID, data); !errors.Is(err, ErrInvalidReflection) {
			t.Fatalf("document %s error = %v, want invalid reflection", data, err)
		}
	}
}

func TestLearningServiceSubmitReflectionJSONRejectsUnknownFieldsAndDeepInput(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	req, err := svc.CreateReflectionRequest(ctx, CreateReflectionRequestInput{WorkflowRunID: "run", StepID: "reflect", AttemptID: "attempt-json", TaskID: "task", Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitReflectionJSON(ctx, req.ID, []byte(`{"schema_version":1,"observations":[{"kind":"fact","content":"x","unknown":true}]}`)); !errors.Is(err, ErrInvalidReflection) {
		t.Fatalf("unknown field error = %v", err)
	}
	deep := `{"schema_version":1,"observations":[{"kind":"fact","content":"x","metadata":{"a":{"b":{"c":{"d":{"e":1}}}}}}]}`
	if _, err := svc.SubmitReflectionJSON(ctx, req.ID, []byte(deep)); !errors.Is(err, ErrInvalidReflection) {
		t.Fatalf("deep document error = %v", err)
	}
}
