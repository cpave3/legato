package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type forceWorkflowService struct {
	triggerForce bool
	retryForce   bool
}

func (f *forceWorkflowService) Start(context.Context, *workflowengine.Definition, map[string]string) (*store.WorkflowRunView, error) {
	return nil, nil
}
func (f *forceWorkflowService) ListRuns(context.Context, string, int) ([]store.WorkflowRun, error) {
	return nil, nil
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
	return nil, nil
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
