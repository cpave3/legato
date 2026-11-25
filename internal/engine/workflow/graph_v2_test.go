package workflow

import (
	"reflect"
	"testing"
)

// Graph version 2 (LEG-484): a loop is a node whose body's steps are nodes
// with the loop as parent, joined to it by loop edges; an if: makes the
// edges into its step branch edges, with one past it for when it is
// skipped.

const overLoopGraph = `schema_version: 2
name: lg
title: Loop graph
inputs:
  items: {type: json, required: true}
steps:
  - id: find
    title: Find
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: each
    title: Each item
    loop:
      over: "${{ workflow.inputs.items }}"
      max: 5
    outputs:
      note: {type: string, value: "${{ steps.write.outputs.note_id }}"}
    steps:
      - id: read
        title: Read
        type: task.context
        with: {task: "${{ workflow.task }}"}
      - id: write
        title: Write
        type: task.note
        if: {field: steps.read.outputs.content, exists: true}
        with: {task: "${{ workflow.task }}", body: "${{ steps.find.outputs.content }}"}
  - id: done
    title: Done
    type: task.move
    with: {task: "${{ workflow.task }}", column: Done}
`

func edgeBetween(graph *Graph, kind, source, target string) *GraphEdge {
	for i, edge := range graph.Edges {
		if edge.Kind == kind && edge.Source == source && edge.Target == target {
			return &graph.Edges[i]
		}
	}
	return nil
}

func TestGraphVersion2DrawsALoopAndItsBody(t *testing.T) {
	graph := BuildGraph(mustParse(t, overLoopGraph))
	if graph.Version != 2 {
		t.Fatalf("version = %d", graph.Version)
	}
	loop := nodeByID(graph, "step:each")
	if loop == nil || loop.Kind != NodeStep || loop.Step.Type != LoopStepType || loop.Step.Group != GroupLoop || loop.Parent != "" {
		t.Fatalf("loop node = %#v", loop)
	}
	if loop.Step.Loop == nil || loop.Step.Loop.Form != "over" || loop.Step.Loop.Max != 5 {
		t.Fatalf("loop = %#v", loop.Step.Loop)
	}
	for _, id := range []string{"read", "write"} {
		node := nodeByID(graph, "step:each/"+id)
		if node == nil || node.Kind != NodeStep || node.Parent != "step:each" || node.Step.ID != id || node.Step.Path != "each/"+id {
			t.Fatalf("body step %s = %#v", id, node)
		}
	}
	if got, want := edgesOfKind(graph, EdgeLoop), [][2]string{{"step:each", "step:each/read"}, {"step:each/write", "step:each"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loop edges = %v", got)
	}
	if got, want := edgesOfKind(graph, EdgeOrder), [][2]string{{"step:find", "step:each"}, {"step:each", "step:done"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order edges = %v", got)
	}
	want := map[[2]string][]string{
		{"step:find", "step:each/write"}:      {"content"},
		{"step:each/read", "step:each/write"}: {"content"},
		{"step:each/write", "step:each"}:      {"note_id"},
	}
	if got := dataEdges(graph); !reflect.DeepEqual(got, want) {
		t.Fatalf("data edges = %v", got)
	}
}

func TestGraphVersion2DrawsAnIfAsBranches(t *testing.T) {
	graph := BuildGraph(mustParse(t, overLoopGraph))
	into := edgeBetween(graph, EdgeBranch, "step:each/read", "step:each/write")
	if into == nil || into.Condition != "steps.read.outputs.content exists" || into.Skip {
		t.Fatalf("branch into write = %#v", into)
	}
	// write is its pass's last step: skipped, the pass goes back to the loop.
	past := edgeBetween(graph, EdgeBranch, "step:each/read", "step:each")
	if past == nil || !past.Skip {
		t.Fatalf("branch past write = %#v", past)
	}
	if edgeBetween(graph, EdgeOrder, "step:each/read", "step:each/write") != nil {
		t.Fatal("an order edge leads into a step with if:")
	}

	top := mustParse(t, `schema_version: 2
name: b
title: B
steps:
  - {id: a, type: gate, with: {title: Go}}
  - {id: b, if: {any: [{field: steps.a.outputs.choice, equals: Continue}, {not: {field: steps.a.outputs.choice, in: [Stop, Later]}}]}, type: task.context, with: {task: "${{ workflow.task }}"}}
  - {id: c, type: task.context, with: {task: "${{ workflow.task }}"}}
`)
	graph = BuildGraph(top)
	into = edgeBetween(graph, EdgeBranch, "step:a", "step:b")
	if into == nil || into.Condition != "steps.a.outputs.choice equals Continue or not (steps.a.outputs.choice in Stop, Later)" {
		t.Fatalf("branch into b = %#v", into)
	}
	if past := edgeBetween(graph, EdgeBranch, "step:a", "step:c"); past == nil || !past.Skip {
		t.Fatalf("branch past b = %#v", past)
	}
	if edgeBetween(graph, EdgeOrder, "step:b", "step:c") == nil {
		t.Fatal("no order edge after b")
	}
}

func TestGraphVersion2DrawsANextLoopsQueryInItsPass(t *testing.T) {
	graph := BuildGraph(mustParse(t, `schema_version: 2
name: n
title: N
steps:
  - id: tickets
    loop:
      next:
        type: task.context
        with: {task: "${{ workflow.task }}"}
      key: "${{ loop.item.key }}"
    steps:
      - id: card
        type: task.context
        with: {task: "${{ workflow.task }}"}
`))
	query := nodeByID(graph, "step:tickets/next")
	if query == nil || query.Parent != "step:tickets" || query.Step.Type != "task.context" || query.Step.Path != "tickets/next" || query.Step.ID != NextStepID {
		t.Fatalf("query node = %#v", query)
	}
	if nodeByID(graph, "step:tickets").Step.Loop.Form != "next" {
		t.Fatal("the loop is not a next: loop")
	}
	if edgeBetween(graph, EdgeLoop, "step:tickets", "step:tickets/next") == nil || edgeBetween(graph, EdgeOrder, "step:tickets/next", "step:tickets/card") == nil {
		t.Fatalf("the pass does not start at its query: %v", graph.Edges)
	}
}

func TestAGraphOfAVersion1FileKeepsAgentEnded(t *testing.T) {
	graph := BuildGraph(mustParse(t, feeding))
	after := nodeByID(graph, "step:after").Step
	if after.Start != StartAgentEnded || after.Gate != GateAgentEnded {
		t.Fatalf("after = %#v", after)
	}
}
