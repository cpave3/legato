package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// writeWorkflows writes each file into a fresh folder and discovers it,
// with the built-in review-loop added.
func writeWorkflows(t *testing.T, files map[string]string) workflow.DiscoveryResult {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return workflow.WithBuiltIns(workflow.DiscoverPaths(dir), workflow.BuiltInReviewLoop())
}

func nodeByID(graph workflow.CallGraph, id string) (workflow.CallGraphNode, bool) {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return workflow.CallGraphNode{}, false
}

func edgeBetween(graph workflow.CallGraph, source, target string) (workflow.CallGraphEdge, bool) {
	for _, edge := range graph.Edges {
		if edge.Source == source && edge.Target == target {
			return edge, true
		}
	}
	return workflow.CallGraphEdge{}, false
}

const callGraphNote = `
  - id: note
    type: task.note
    with: {task: "${{ workflow.task }}", note: hi}
`

func TestTheCallGraphHasANodeForEveryWorkflowAndAnEdgeForEachCall(t *testing.T) {
	result := writeWorkflows(t, map[string]string{
		"deliver.workflow.yaml": `schema_version: 2
name: deliver
title: Deliver
tags: [delivery]
on:
  schedule: {cron: "0 9 * * 1", enabled: false}
steps:
  - id: build
    type: workflow.run
    with: {workflow: build}
  - id: again
    type: workflow.run
    with: {workflow: build}
  - id: review
    type: workflow.run
    with: {workflow: review-loop, inputs: {repo: owner/repo, pr_number: 1}}
`,
		"build.workflow.yaml": `schema_version: 2
name: build
title: Build
inputs:
  next: {type: string, default: polish, choices: [polish, ship]}
steps:
  - id: each
    loop: {repeat: true, max: 3, until: {field: loop.last.status, equals: succeeded}}
    steps:
      - id: inner
        loop: {repeat: true, max: 2, until: {field: loop.last.status, equals: succeeded}}
        steps:
          - id: review
            type: workflow.run
            with: {workflow: review-loop, inputs: {repo: owner/repo, pr_number: 2}}
  - id: then
    type: workflow.run
    with: {workflow: "${{ workflow.inputs.next }}"}
`,
		"polish.workflow.yaml": `schema_version: 2
name: polish
title: Polish
steps:` + callGraphNote,
		"alone.workflow.yaml": `schema_version: 2
name: alone
title: Alone
steps:` + callGraphNote,
		"broken.workflow.yaml": `schema_version: 2
name: broken
title: Broken
steps:
  - id: x
    type: no.such.step
`,
	})
	graph := workflow.BuildCallGraph(result)

	deliver, ok := nodeByID(graph, "deliver")
	if !ok || deliver.Title != "Deliver" || len(deliver.Tags) != 1 || deliver.Broken || deliver.BuiltIn {
		t.Fatalf("deliver = %+v", deliver)
	}
	if len(deliver.Triggers) != 1 || deliver.Triggers[0].Kind != "schedule" || deliver.Triggers[0].EnabledByDefault {
		t.Fatalf("deliver's triggers = %+v", deliver.Triggers)
	}
	if loop, ok := nodeByID(graph, "review-loop"); !ok || !loop.BuiltIn {
		t.Fatalf("review-loop = %+v, %v", loop, ok)
	}

	// Two steps call build: one edge that counts them.
	if edge, ok := edgeBetween(graph, "deliver", "build"); !ok || edge.Calls != 2 || edge.InLoop || edge.Dynamic {
		t.Fatalf("deliver → build = %+v, %v", edge, ok)
	}
	if edge, ok := edgeBetween(graph, "deliver", "review-loop"); !ok || edge.Calls != 1 {
		t.Fatalf("deliver → review-loop = %+v, %v", edge, ok)
	}
	// A call inside nested loops is found, and marked as in a loop.
	if edge, ok := edgeBetween(graph, "build", "review-loop"); !ok || !edge.InLoop || edge.Steps[0] != "each.inner.review" {
		t.Fatalf("build → review-loop = %+v, %v", edge, ok)
	}
	// An input with choices: an edge to each choice, marked dynamic; a
	// choice no file defines is a missing node.
	if edge, ok := edgeBetween(graph, "build", "polish"); !ok || !edge.Dynamic {
		t.Fatalf("build → polish = %+v, %v", edge, ok)
	}
	if edge, ok := edgeBetween(graph, "build", "ship"); !ok || !edge.Dynamic {
		t.Fatalf("build → ship = %+v, %v", edge, ok)
	}
	if ship, ok := nodeByID(graph, "ship"); !ok || !ship.Missing {
		t.Fatalf("ship = %+v, %v", ship, ok)
	}

	// A file that does not parse is a node marked broken, named from its
	// file, with the error.
	broken, ok := nodeByID(graph, "broken")
	if !ok || !broken.Broken || broken.Error == "" || broken.Source == "" {
		t.Fatalf("broken = %+v, %v", broken, ok)
	}

	// A workflow with no calls either way has no edges.
	for _, edge := range graph.Edges {
		if edge.Source == "alone" || edge.Target == "alone" {
			t.Fatalf("alone has edge %+v", edge)
		}
	}
	if _, ok := nodeByID(graph, "alone"); !ok {
		t.Fatal("alone has no node")
	}
}

func TestABrokenFileThatSharesAWorkflowsNameIsANodeOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	good := `schema_version: 2
name: same
title: Same
steps:` + callGraphNote
	if err := os.WriteFile(filepath.Join(dir, "same.workflow.yaml"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "same.workflow.yaml"), []byte("name: same\nsteps: [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	graph := workflow.BuildCallGraph(workflow.DiscoverPaths(dir, project))
	if len(graph.Nodes) != 2 {
		t.Fatalf("nodes = %+v", graph.Nodes)
	}
	if node, ok := nodeByID(graph, "same"); !ok || node.Broken {
		t.Fatalf("same = %+v", node)
	}
	broken := graph.Nodes[1]
	if broken.ID == "same" || !broken.Broken || broken.Name != "same" {
		t.Fatalf("broken = %+v", broken)
	}
}
