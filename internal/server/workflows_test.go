package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

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
