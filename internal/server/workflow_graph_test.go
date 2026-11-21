package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestEachWorkflowDefinitionComesWithItsGraph(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "review.yaml"), []byte("schema_version: 1\nname: review\ntitle: Review\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: review}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("schema_version: bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(nil, dir, "")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Definitions []struct {
			Name  string                `json:"name"`
			Steps []any                 `json:"steps"`
			Graph *workflowengine.Graph `json:"graph"`
		} `json:"definitions"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Definitions) != 1 || body.Definitions[0].Name != "review" || len(body.Definitions[0].Steps) != 1 {
		t.Fatalf("definitions = %+v", body.Definitions)
	}
	graph := body.Definitions[0].Graph
	if graph == nil || graph.Version != workflowengine.GraphVersion || len(graph.Nodes) != 2 || graph.Nodes[1].ID != "step:run" {
		t.Fatalf("graph = %+v", graph)
	}
	if len(body.Errors) != 1 {
		t.Fatalf("errors = %+v", body.Errors)
	}
	if _, has := body.Errors[0]["graph"]; has {
		t.Fatal("a definition that failed to parse has a graph")
	}
}
