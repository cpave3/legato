package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
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

type showWorkflowService struct {
	forceWorkflowService
	view *store.WorkflowRunView
}

func (f *showWorkflowService) Show(context.Context, string) (*store.WorkflowRunView, error) {
	return f.view, nil
}

func runWithCopy(t *testing.T, yaml string) *store.WorkflowRunView {
	t.Helper()
	definition, err := workflowengine.Parse("/w/review.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := json.Marshal(definition)
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: "wr-1", WorkflowName: definition.Name, DefinitionJSON: string(copy), DefinitionDigest: workflowengine.Digest(definition), Status: "running"}, TriggerKind: "schedule"}
}

func getRun(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-runs/wr-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

const reviewOneStep = "schema_version: 1\nname: review\ntitle: Review\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: review}\n"
const reviewTwoSteps = reviewOneStep + "  - id: note\n    title: Note\n    type: task.note\n    with: {body: done}\n"

func serverWithRun(t *testing.T, fileYAML string, view *store.WorkflowRunView) *Server {
	t.Helper()
	dir := t.TempDir()
	if fileYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, "review.yaml"), []byte(fileYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&showWorkflowService{view: view}, dir, "")
	return s
}

func graphNodeIDs(body map[string]any) []string {
	graph, _ := body["graph"].(map[string]any)
	nodes, _ := graph["nodes"].([]any)
	var ids []string
	for _, node := range nodes {
		ids = append(ids, node.(map[string]any)["id"].(string))
	}
	return ids
}

func TestARunsGraphComesFromItsOwnCopyOfTheDefinition(t *testing.T) {
	body := getRun(t, serverWithRun(t, reviewTwoSteps, runWithCopy(t, reviewOneStep)))
	if ids := graphNodeIDs(body); len(ids) != 2 || ids[1] != "step:run" {
		t.Fatalf("graph nodes = %v, want the run's one step", ids)
	}
	if body["definition_changed"] != true || body["definition_missing"] == true {
		t.Fatalf("changed=%v missing=%v", body["definition_changed"], body["definition_missing"])
	}
	if body["trigger_kind"] != "schedule" {
		t.Fatalf("trigger_kind = %v", body["trigger_kind"])
	}
	if run, _ := body["run"].(map[string]any); run["id"] != "wr-1" {
		t.Fatalf("the run view is still there: %v", body["run"])
	}
}

func TestARunOfTheCurrentDefinitionHasNotChanged(t *testing.T) {
	view := runWithCopy(t, reviewOneStep)
	view.TriggerKind = ""
	body := getRun(t, serverWithRun(t, reviewOneStep, view))
	if body["definition_changed"] == true || body["definition_missing"] == true {
		t.Fatalf("changed=%v missing=%v", body["definition_changed"], body["definition_missing"])
	}
	if kind, _ := body["trigger_kind"].(string); kind != "" {
		t.Fatalf("a manual run has trigger_kind %q", kind)
	}
}

func TestARunWhoseWorkflowFileIsGone(t *testing.T) {
	body := getRun(t, serverWithRun(t, "", runWithCopy(t, reviewOneStep)))
	if body["definition_missing"] != true || body["definition_changed"] == true {
		t.Fatalf("changed=%v missing=%v", body["definition_changed"], body["definition_missing"])
	}
	if len(graphNodeIDs(body)) != 2 {
		t.Fatal("a run whose file is gone still has its graph")
	}
}
