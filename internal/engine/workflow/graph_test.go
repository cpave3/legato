package workflow

import (
	"encoding/json"
	"reflect"
	"testing"
)

func mustParse(t *testing.T, yaml string) *Definition {
	t.Helper()
	definition, err := Parse("test.workflow.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func nodeByID(graph *Graph, id string) *GraphNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == id {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func edgesOfKind(graph *Graph, kind string) [][2]string {
	var out [][2]string
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			out = append(out, [2]string{edge.Source, edge.Target})
		}
	}
	return out
}

const threeSteps = `schema_version: 1
name: three
title: Three steps
inputs:
  task: {type: task, required: true}
steps:
  - id: investigate
    title: Investigate the task
    type: agent.run
    with:
      prompt: |

        Look into ${{ workflow.inputs.task.title }}.
        Then write it up.
  - id: note
    title: Note it
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: Done
  - id: ready
    title: Move to Ready
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
      column: Ready
`

func TestGraphHasAManualStartAndAStepNodePerStep(t *testing.T) {
	graph := BuildGraph(mustParse(t, threeSteps))
	if graph.Version != GraphVersion {
		t.Fatalf("version = %d", graph.Version)
	}
	manual := nodeByID(graph, "trigger:manual")
	if manual == nil || manual.Kind != NodeTrigger || manual.Trigger == nil || manual.Trigger.Kind != TriggerManual {
		t.Fatalf("manual start = %#v", manual)
	}
	if !manual.Trigger.Enabled {
		t.Fatal("running by hand is always on")
	}
	for _, id := range []string{"investigate", "note", "ready"} {
		node := nodeByID(graph, "step:"+id)
		if node == nil || node.Kind != NodeStep || node.Step == nil || node.Step.ID != id {
			t.Fatalf("step %s = %#v", id, node)
		}
	}
	investigate := nodeByID(graph, "step:investigate").Step
	if investigate.Title != "Investigate the task" || investigate.Type != "agent.run" || investigate.Trigger != "automatic" {
		t.Fatalf("investigate = %#v", investigate)
	}
	if investigate.Definition.ID != "investigate" || investigate.Definition.With["prompt"] == nil {
		t.Fatalf("the step's own definition = %#v", investigate.Definition)
	}
}

func TestGraphStartAndOrderEdges(t *testing.T) {
	graph := BuildGraph(mustParse(t, threeSteps))
	if got, want := edgesOfKind(graph, EdgeStart), [][2]string{{"trigger:manual", "step:investigate"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start edges = %v", got)
	}
	if got, want := edgesOfKind(graph, EdgeOrder), [][2]string{{"step:investigate", "step:note"}, {"step:note", "step:ready"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order edges = %v", got)
	}
	seen := map[string]bool{}
	for _, edge := range graph.Edges {
		if edge.ID == "" || seen[edge.ID] {
			t.Fatalf("edge ID %q is empty or repeated", edge.ID)
		}
		seen[edge.ID] = true
	}
}

func TestGraphKeyValuePerExecutorType(t *testing.T) {
	cases := []struct {
		name  string
		step  string
		field string
		value string
	}{
		{"agent prompt, first line", `{id: s, title: S, type: agent.run, with: {prompt: "\n  Look into it.\nMore."}}`, "prompt", "Look into it."},
		{"agent macro", `{id: s, title: S, type: agent.run, with: {macro: tidy}}`, "macro", "tidy"},
		{"send to agent", `{id: s, title: S, type: agent.dispatch, with: {task: "${{ workflow.inputs.task }}", macro: review}}`, "macro", "review"},
		{"move card", `{id: s, title: S, type: task.move, with: {task: "${{ workflow.inputs.task }}", column: Ready}}`, "column", "Ready"},
		{"review passes", `{id: s, title: S, type: github.pr.review_passes, with: {repo: acme/widget, pr_number: 12}}`, "pr", "acme/widget#12"},
		{"repository", `{id: s, title: S, type: repo.ensure, with: {repo: acme/widget}}`, "repo", "acme/widget"},
		{"remove a file", `{id: s, title: S, type: file.remove, with: {working_dir: /w, path: notes.md}}`, "path", "notes.md"},
		{"note body", `{id: s, title: S, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: "Hello\nthere"}}`, "body", "Hello"},
		{"note from a file", `{id: s, title: S, type: task.note, with: {task: "${{ workflow.inputs.task }}", body_path: out.md}}`, "body_path", "out.md"},
		{"metadata", `{id: s, title: S, type: task.metadata.set, with: {task: "${{ workflow.inputs.task }}", values: {triaged: true, area: web}}}`, "values", "area, triaged"},
		{"nothing to show", `{id: s, title: S, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}`, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition := mustParse(t, "schema_version: 1\nname: k\ntitle: K\ninputs:\n  task: {type: task, required: true}\nsteps:\n  - "+tc.step+"\n")
			step := nodeByID(BuildGraph(definition), "step:s").Step
			if step.Key.Field != tc.field || step.Key.Value != tc.value {
				t.Fatalf("key = %#v, want %s=%q", step.Key, tc.field, tc.value)
			}
		})
	}
}

func TestGraphIDsAreStable(t *testing.T) {
	first, _ := json.Marshal(BuildGraph(mustParse(t, threeSteps)))
	for i := 0; i < 20; i++ {
		again, _ := json.Marshal(BuildGraph(mustParse(t, threeSteps)))
		if string(again) != string(first) {
			t.Fatalf("graph changed between builds:\n%s\n%s", first, again)
		}
	}
}

func TestGraphOfEveryBundledTemplate(t *testing.T) {
	for _, example := range Examples() {
		t.Run(example.Name, func(t *testing.T) {
			definition, err := Parse(example.Name+".workflow.yaml", example.YAML)
			if err != nil {
				t.Fatal(err)
			}
			graph := BuildGraph(definition)
			if nodeByID(graph, "trigger:manual") == nil {
				t.Fatal("no manual start")
			}
			for _, step := range definition.Steps {
				if nodeByID(graph, "step:"+step.ID) == nil {
					t.Fatalf("no node for step %s", step.ID)
				}
			}
			if got := len(edgesOfKind(graph, EdgeOrder)); got != len(definition.Steps)-1 {
				t.Fatalf("%d order edges for %d steps", got, len(definition.Steps))
			}
			ids := map[string]bool{}
			for _, node := range graph.Nodes {
				if ids[node.ID] {
					t.Fatalf("node ID %s repeated", node.ID)
				}
				ids[node.ID] = true
			}
			for _, edge := range graph.Edges {
				if !ids[edge.Source] || !ids[edge.Target] {
					t.Fatalf("edge %s names a node that is not there", edge.ID)
				}
			}
		})
	}
}

const everyTrigger = `schema_version: 1
name: all
title: All triggers
inputs:
  task: {type: task}
  repo: {type: string}
on:
  task.entered_column:
    column: Triage
    confirm: true
    inputs: {task: "${{ event.task }}"}
  schedule:
    every: weekdays
    at: "09:00"
    enabled: false
    max_runs_per_day: 1
  pr.changes_requested:
    max_runs_per_day: 5
steps:
  - {id: first, title: First, type: agent.run, with: {prompt: go}}
  - {id: second, title: Second, type: task.note, with: {body: done}}
`

func TestGraphHasATriggerNodePerDeclaredKind(t *testing.T) {
	graph := BuildGraph(mustParse(t, everyTrigger))
	cases := []struct {
		kind    string
		summary string
		enabled bool
		confirm bool
		max     int
	}{
		{TriggerManual, "Run by hand", true, false, 0},
		{TriggerTaskEnteredColumn, "Card enters Triage", true, true, DefaultMaxRunsPerDay},
		{TriggerSchedule, "Every weekday at 09:00", false, false, 1},
		{TriggerPRChangesRequested, "Changes requested on a PR", true, false, 5},
	}
	for _, tc := range cases {
		node := nodeByID(graph, "trigger:"+tc.kind)
		if node == nil || node.Trigger == nil {
			t.Fatalf("no node for %s", tc.kind)
		}
		got := node.Trigger
		if got.Kind != tc.kind || got.Summary != tc.summary || got.Enabled != tc.enabled || got.Confirm != tc.confirm || got.MaxRunsPerDay != tc.max {
			t.Fatalf("%s = %+v", tc.kind, got)
		}
		if tc.kind != TriggerManual && got.Definition == nil {
			t.Fatalf("%s has no definition", tc.kind)
		}
	}
	want := [][2]string{{"trigger:manual", "step:first"}, {"trigger:task.entered_column", "step:first"}, {"trigger:schedule", "step:first"}, {"trigger:pr.changes_requested", "step:first"}}
	if got := edgesOfKind(graph, EdgeStart); !reflect.DeepEqual(got, want) {
		t.Fatalf("start edges = %v", got)
	}
}

func TestGraphHasNoTriggerNodeForAKindNotDeclared(t *testing.T) {
	graph := BuildGraph(mustParse(t, threeSteps))
	for _, node := range graph.Nodes {
		if node.Kind == NodeTrigger && node.Trigger.Kind != TriggerManual {
			t.Fatalf("unexpected trigger node %s", node.ID)
		}
	}
}

const feeding = `schema_version: 1
name: feed
title: Feeding
inputs:
  task: {type: task, required: true}
steps:
  - id: write
    title: Write
    type: agent.run
    with: {prompt: write}
    outputs:
      report: {type: file, path: report.md}
      summary: {type: string}
  - id: wait
    title: Wait for me
    type: task.note
    trigger: manual
    with: {task: "${{ workflow.inputs.task }}", body: "${{ steps.write.outputs.summary }}"}
  - id: after
    title: After the agent
    type: task.note
    trigger: agent_ended
    with:
      task: "${{ workflow.inputs.task }}"
      body_path: "${{ steps.write.outputs.report.path }}"
      extra: ["${{ steps.write.outputs.summary }}", {nested: "${{ steps.write.outputs.report }}"}]
    outputs:
      copy: {type: file, path: copy.md}
`

func TestGraphGates(t *testing.T) {
	definition := mustParse(t, feeding)
	graph := BuildGraph(definition)
	for id, gate := range map[string]string{"write": "", "wait": GateManual, "after": GateAgentEnded} {
		if got := nodeByID(graph, "step:"+id).Step.Gate; got != gate {
			t.Fatalf("%s gate = %q, want %q", id, got, gate)
		}
	}
}

const outputPathRef = `schema_version: 1
name: paths
title: Paths
steps:
  - id: name
    title: Name
    type: agent.run
    with: {prompt: name it}
    outputs:
      slug: {type: string}
  - id: middle
    title: Middle
    type: agent.run
    with: {prompt: nothing}
  - id: write
    title: Write
    type: agent.run
    with: {prompt: write}
    outputs:
      out: {type: file, path: "${{ steps.name.outputs.slug }}.md"}
`

func dataEdges(graph *Graph) map[[2]string][]string {
	out := map[[2]string][]string{}
	for _, edge := range graph.Edges {
		if edge.Kind == EdgeData {
			out[[2]string{edge.Source, edge.Target}] = edge.Outputs
		}
	}
	return out
}

func TestGraphDataEdges(t *testing.T) {
	graph := BuildGraph(mustParse(t, feeding))
	want := map[[2]string][]string{
		{"step:write", "step:wait"}:  {"summary"},
		{"step:write", "step:after"}: {"report", "summary"},
	}
	if got := dataEdges(graph); !reflect.DeepEqual(got, want) {
		t.Fatalf("data edges = %v", got)
	}
}

func TestGraphDataEdgesFromOutputPathsSkipSteps(t *testing.T) {
	graph := BuildGraph(mustParse(t, outputPathRef))
	want := map[[2]string][]string{{"step:name", "step:write"}: {"slug"}}
	if got := dataEdges(graph); !reflect.DeepEqual(got, want) {
		t.Fatalf("data edges = %v", got)
	}
}
