package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

type workflowMapBody struct {
	Nodes []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		BuiltIn  bool   `json:"built_in"`
		Broken   bool   `json:"broken"`
		Missing  bool   `json:"missing"`
		Triggers []struct {
			Trigger string `json:"trigger"`
			Enabled bool   `json:"enabled"`
		} `json:"triggers"`
	} `json:"nodes"`
	Edges []struct {
		Source  string   `json:"source"`
		Target  string   `json:"target"`
		Calls   int      `json:"calls"`
		InLoop  bool     `json:"in_loop"`
		Dynamic bool     `json:"dynamic"`
		Steps   []string `json:"steps"`
	} `json:"edges"`
}

func TestTheWorkflowMapRouteGivesEveryWorkflowAndTheCallsBetweenThem(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := t.TempDir()
	files := map[string]string{
		// Calls review-loop twice, once inside a loop, and a dynamic callee.
		"ship.workflow.yaml": `schema_version: 2
name: ship
title: Ship
tags: [delivery]
inputs:
  next: {type: string, default: alone, choices: [alone, gone]}
on:
  task.entered_column:
    column: Doing
    enabled: false
    inputs: {}
steps:
  - id: first
    type: workflow.run
    with: {workflow: review-loop, inputs: {repo: owner/repo, pr_number: 1}}
  - id: passes
    loop: {repeat: true, max: 3, until: {field: loop.last.status, equals: succeeded}}
    steps:
      - id: again
        type: workflow.run
        with: {workflow: review-loop, inputs: {repo: owner/repo, pr_number: 1}}
  - id: then
    type: workflow.run
    with: {workflow: "${{ workflow.inputs.next }}"}
`,
		"alone.workflow.yaml": `schema_version: 2
name: alone
title: Alone
steps:
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: hi}
`,
		"standalone.workflow.yaml": `schema_version: 2
name: standalone
title: Standalone
steps:
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: hi}
`,
		"broken.workflow.yaml": "schema_version: 2\nname: broken\nsteps: [oops",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := New(nil, nil, nil, ":0")
	s.workflowGlobalDir = dir
	s.SetBuiltInWorkflows(workflowengine.BuiltInReviewLoop())
	workflows := service.NewWorkflowService(db, nil)
	s.SetWorkflowTriggerCoordinator(service.NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) }))
	if err := db.SetWorkflowTriggerSwitch(t.Context(), "ship", "task.entered_column", true); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows/graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body workflowMapBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]int{}
	for i, node := range body.Nodes {
		nodes[node.ID] = i
	}
	for _, id := range []string{"ship", "alone", "standalone", "broken", "review-loop", "gone"} {
		if _, ok := nodes[id]; !ok {
			t.Fatalf("no node %q in %+v", id, body.Nodes)
		}
	}
	if !body.Nodes[nodes["review-loop"]].BuiltIn || !body.Nodes[nodes["broken"]].Broken || !body.Nodes[nodes["gone"]].Missing {
		t.Fatalf("nodes = %+v", body.Nodes)
	}
	// The trigger is as this Legato has it: switched on here, though its
	// file says off.
	ship := body.Nodes[nodes["ship"]]
	if len(ship.Triggers) != 1 || ship.Triggers[0].Trigger != "task.entered_column" || !ship.Triggers[0].Enabled {
		t.Fatalf("ship's triggers = %+v", ship.Triggers)
	}
	edges := map[string]int{}
	for i, edge := range body.Edges {
		edges[edge.Source+">"+edge.Target] = i
	}
	if len(body.Edges) != 3 {
		t.Fatalf("edges = %+v", body.Edges)
	}
	loop := body.Edges[edges["ship>review-loop"]]
	if loop.Calls != 2 || !loop.InLoop || loop.Dynamic || len(loop.Steps) != 2 || loop.Steps[1] != "passes.again" {
		t.Fatalf("ship → review-loop = %+v", loop)
	}
	if edge := body.Edges[edges["ship>alone"]]; !edge.Dynamic || edge.Calls != 1 {
		t.Fatalf("ship → alone = %+v", edge)
	}
	for _, edge := range body.Edges {
		if edge.Source == "standalone" || edge.Target == "standalone" {
			t.Fatalf("standalone has an edge: %+v", edge)
		}
	}
}

func TestTheWorkflowMapRouteIsGetOnly(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows/graph", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
