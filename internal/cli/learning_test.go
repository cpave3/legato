package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeLifecycleService struct {
	id, claim, scope, project, destination, target, action, kind, result string
	revision                                                             int
	references                                                           []string
	passed                                                               bool
}

func (f *fakeLifecycleService) Accept(_ context.Context, id string, revision int, claim, scope, project string) (*service.LearningView, error) {
	f.id, f.revision, f.claim, f.scope, f.project = id, revision, claim, scope, project
	return &service.LearningView{Learning: store.Learning{ID: id}}, nil
}
func (f *fakeLifecycleService) Reject(_ context.Context, id string, revision int) (*service.LearningView, error) {
	f.id, f.revision = id, revision
	return &service.LearningView{Learning: store.Learning{ID: id}}, nil
}
func (f *fakeLifecycleService) ProposeIncorporation(_ context.Context, id string, revision int, destination, target, action string) (*store.LearningIncorporation, error) {
	f.id, f.revision, f.destination, f.target, f.action = id, revision, destination, target, action
	return &store.LearningIncorporation{ID: "inc-1"}, nil
}
func (f *fakeLifecycleService) SubmitIncorporation(_ context.Context, id string, revision int, references []string, kind, result string) (*store.LearningIncorporation, error) {
	f.id, f.revision, f.references, f.kind, f.result = id, revision, references, kind, result
	return &store.LearningIncorporation{ID: id}, nil
}

func TestLearningLifecycleUsesRevisionAwareServiceOperations(t *testing.T) {
	fake := &fakeLifecycleService{}
	out := &strings.Builder{}
	if err := LearningAccept(context.Background(), fake, "learn-1", 3, "new", "project", "repo:key", out); err != nil {
		t.Fatal(err)
	}
	if fake.revision != 3 || fake.claim != "new" || fake.project != "repo:key" {
		t.Fatalf("accept args: %#v", fake)
	}
	if err := LearningIncorporate(context.Background(), fake, "learn-1", 4, "documentation", "docs/a.md", "update", out); err != nil {
		t.Fatal(err)
	}
	if fake.destination != "documentation" || fake.target != "docs/a.md" {
		t.Fatalf("incorporate args: %#v", fake)
	}
}

func TestLearningIncorporationSubmitParsesReferencesAndVerification(t *testing.T) {
	fake := &fakeLifecycleService{}
	doc := []byte(`{"references":["docs/a.md:4"],"verification":{"kind":"source_comparison","result":"matches"}}`)
	if err := LearningIncorporationSubmit(context.Background(), fake, "inc-1", 1, doc, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.references) != 1 || fake.kind != "source_comparison" || fake.result != "matches" {
		t.Fatalf("submit args: %#v", fake)
	}
}

func TestLearningLifecycleRequiresPositiveRevision(t *testing.T) {
	err := LearningReject(context.Background(), &fakeLifecycleService{}, "learn-1", 0, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("error = %v", err)
	}
}

type fakeReflectionService struct {
	submission *service.ReflectionSubmission
	err        error
	requestID  string
	document   string
}

func (f *fakeReflectionService) SubmitReflectionJSON(_ context.Context, requestID string, data []byte) (*service.ReflectionSubmission, error) {
	f.requestID, f.document = requestID, string(data)
	return f.submission, f.err
}

type fakeWorkflowCompletion struct {
	runID, stepID, attemptID string
	values                   map[string]string
	called                   bool
	err                      error
}

func (f *fakeWorkflowCompletion) Complete(_ context.Context, runID, stepID, attemptID string, values, _ map[string]string) (*store.WorkflowRunView, error) {
	f.called = true
	f.runID, f.stepID, f.attemptID, f.values = runID, stepID, attemptID, values
	return nil, f.err
}

func TestLearningReflectionSubmitPersistsThenCompletesRecordedAttempt(t *testing.T) {
	learn := &fakeReflectionService{submission: &service.ReflectionSubmission{
		Request:  &store.LearningReflectionRequest{ID: "lrq-1", WorkflowRunID: "run-1", StepID: "reflect", AttemptID: "try-1"},
		Evidence: []store.LearningEvidence{{ID: "one"}, {ID: "two"}},
	}}
	workflow := &fakeWorkflowCompletion{}
	var out strings.Builder
	if err := LearningReflectionSubmit(context.Background(), learn, workflow, "lrq-1", []byte(`{"schema_version":1}`), &out); err != nil {
		t.Fatal(err)
	}
	if learn.requestID != "lrq-1" || !workflow.called {
		t.Fatalf("submission request=%q, workflow called=%v", learn.requestID, workflow.called)
	}
	if workflow.runID != "run-1" || workflow.stepID != "reflect" || workflow.attemptID != "try-1" {
		t.Fatalf("completion identity = %q/%q/%q", workflow.runID, workflow.stepID, workflow.attemptID)
	}
	if workflow.values["available"] != "true" || workflow.values["request_id"] != "lrq-1" {
		t.Fatalf("completion values = %#v", workflow.values)
	}
	if _, ok := workflow.values["evidence_count"]; ok {
		t.Fatalf("undeclared completion output present: %#v", workflow.values)
	}
}

func TestLearningReflectionSubmitDoesNotCompleteAfterSubmissionError(t *testing.T) {
	learn := &fakeReflectionService{err: errors.New("invalid reflection")}
	workflow := &fakeWorkflowCompletion{}
	err := LearningReflectionSubmit(context.Background(), learn, workflow, "lrq-1", []byte(`{}`), &strings.Builder{})
	if err == nil || workflow.called {
		t.Fatalf("error=%v workflow called=%v", err, workflow.called)
	}
}

func TestLearningReflectionSubmitReportsWorkflowCompletionError(t *testing.T) {
	learn := &fakeReflectionService{submission: &service.ReflectionSubmission{Request: &store.LearningReflectionRequest{ID: "lrq-1", WorkflowRunID: "run", StepID: "step", AttemptID: "attempt"}}}
	workflow := &fakeWorkflowCompletion{err: errors.New("stale attempt")}
	err := LearningReflectionSubmit(context.Background(), learn, workflow, "lrq-1", []byte(`{}`), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "complete reflection workflow") {
		t.Fatalf("error = %v", err)
	}
}
