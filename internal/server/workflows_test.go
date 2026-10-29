package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type forceWorkflowService struct {
	triggerForce bool
	retryForce   bool
	artifact     *store.WorkflowArtifact
	attempts     []store.WorkflowStepAttempt
	lookupTaskID string
}

func (f *forceWorkflowService) Start(context.Context, *workflowengine.Definition, map[string]string) (*store.WorkflowRunView, error) {
	return nil, nil
}
func (f *forceWorkflowService) ListRuns(context.Context, store.WorkflowRunFilter) ([]store.WorkflowRun, error) {
	return nil, nil
}
func (f *forceWorkflowService) ActiveAgentAttemptsByTask(_ context.Context, taskID string) ([]store.WorkflowStepAttempt, error) {
	f.lookupTaskID = taskID
	return f.attempts, nil
}
func (f *forceWorkflowService) Show(context.Context, string) (*store.WorkflowRunView, error) {
	return nil, nil
}
func (f *forceWorkflowService) Cancel(context.Context, string, string) (*store.WorkflowRunView, error) {
	return nil, nil
}
func (f *forceWorkflowService) Retry(_ context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error) {
	f.retryForce = force
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, CurrentStepID: stepID}}, nil
}
func (f *forceWorkflowService) TriggerStep(_ context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error) {
	f.triggerForce = force
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, CurrentStepID: stepID}}, nil
}
func (f *forceWorkflowService) PromptStep(context.Context, string, string) (*store.WorkflowRunView, error) {
	return nil, nil
}
func (f *forceWorkflowService) Artifact(context.Context, string) (*store.WorkflowArtifact, error) {
	return f.artifact, nil
}

func TestNotifyAgentChangedEmitsTargetedWorkflowEvent(t *testing.T) {
	bus := events.New()
	workflows := &forceWorkflowService{attempts: []store.WorkflowStepAttempt{{RunID: "wr-1", StepID: "agent"}}}
	s := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	s.SetWorkflowService(workflows, "", "")
	changed := bus.Subscribe(events.EventWorkflowChanged)

	s.NotifyAgentChanged("task-1")
	if workflows.lookupTaskID != "task-1" {
		t.Fatalf("lookup task ID = %q", workflows.lookupTaskID)
	}

	select {
	case event := <-changed:
		payload, ok := event.Payload.(events.WorkflowChangedPayload)
		if !ok || payload.RunID != "wr-1" || payload.StepID != "agent" {
			t.Fatalf("payload=%+v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for targeted workflow event")
	}
}

func TestWorkflowStepEndpointsForwardForce(t *testing.T) {
	workflows := &forceWorkflowService{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")
	for _, endpoint := range []string{"trigger", "retry"} {
		req := httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/remove/"+endpoint, strings.NewReader(`{"force":true}`))
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, rec.Code, rec.Body.String())
		}
	}
	if !workflows.triggerForce || !workflows.retryForce {
		t.Fatalf("trigger force=%v retry force=%v", workflows.triggerForce, workflows.retryForce)
	}
}

func TestWorkflowArtifactEndpointServesTextInlineAndBinaryWithSafeFilename(t *testing.T) {
	tests := []struct {
		name, mediaType, originalPath, wantType, wantDisposition string
	}{
		{name: "Markdown remains inline", mediaType: "text/markdown", originalPath: "reports/triage.md", wantType: "text/markdown"},
		{name: "binary uses original basename", mediaType: "application/zip", originalPath: "nested/reports/archive.zip", wantType: "application/octet-stream", wantDisposition: "attachment; filename=archive.zip"},
		{name: "unsafe filename is encoded", mediaType: "application/zip", originalPath: "nested/evil\r\nX-Legato: injected.zip", wantType: "application/octet-stream", wantDisposition: "attachment; filename*=utf-8''evil%0D%0AX-Legato%3A%20injected.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managed := filepath.Join(t.TempDir(), "artifact")
			if err := os.WriteFile(managed, []byte("artifact body"), 0o600); err != nil {
				t.Fatal(err)
			}
			workflows := &forceWorkflowService{artifact: &store.WorkflowArtifact{ID: "wfa-1", ManagedPath: managed, OriginalPath: tt.originalPath, OutputKey: "artifact", MediaType: tt.mediaType}}
			s := New(nil, nil, nil, "")
			s.SetWorkflowService(workflows, "", "")
			rec := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-artifacts/wfa-1", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Content-Disposition"); got != tt.wantDisposition {
				t.Fatalf("Content-Disposition = %q, want %q", got, tt.wantDisposition)
			}
			if got := rec.Header().Get("X-Legato"); got != "" {
				t.Fatalf("injected header = %q", got)
			}
		})
	}
}

func TestWorkflowsEndpointReturnsDefinitionsAndValidationErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "review.yaml"), []byte("schema_version: 1\nname: review\ntitle: Review\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: review}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("schema_version: bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(nil, dir, "", workflowengine.RepositoryPreset{Name: "Legato", Repo: "cpave3/legato", PrimaryDir: "/projects/legato", BaseBranch: "main"})
	req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"review"`) || !strings.Contains(rec.Body.String(), `broken.yaml`) || !strings.Contains(rec.Body.String(), `"repo":"cpave3/legato"`) || !strings.Contains(rec.Body.String(), `"base_branch":"main"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type startRecordingWorkflowService struct {
	forceWorkflowService
	started *workflowengine.Definition
	inputs  map[string]string
}

func (f *startRecordingWorkflowService) Start(_ context.Context, definition *workflowengine.Definition, inputs map[string]string) (*store.WorkflowRunView, error) {
	f.started, f.inputs = definition, inputs
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: "wr-1", CurrentStepID: "checkout"}}, nil
}

func TestBuiltInWorkflowIsListedAndStartable(t *testing.T) {
	workflows := &startRecordingWorkflowService{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, t.TempDir(), "")
	s.SetBuiltInWorkflows(&workflowengine.Definition{SchemaVersion: 1, Name: "legato-job", Title: "Agent job", Source: "builtin:legato-job"})

	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"legato-job"`) {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows/legato-job/runs", strings.NewReader(`{"inputs":{"prompt":"Fix it"}}`)))
	if rec.Code != http.StatusCreated || workflows.started == nil || workflows.started.Source != "builtin:legato-job" || workflows.inputs["prompt"] != "Fix it" {
		t.Fatalf("start: status=%d body=%s started=%+v", rec.Code, rec.Body.String(), workflows.started)
	}
}
