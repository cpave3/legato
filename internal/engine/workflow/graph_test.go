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
	if investigate.Title != "Investigate the task" || investigate.Type != "agent.run" || investigate.Start != "automatic" || investigate.Path != "investigate" {
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
		{"message an agent", `{id: s, title: S, type: agent.message, with: {task: "${{ workflow.inputs.task }}", prompt: "Push it\nnow"}}`, "prompt", "Push it"},
		{"compact agent", `{id: s, title: S, type: agent.compact, with: {task: "${{ workflow.task }}", instructions: "Keep the plan\nand the tests"}}`, "instructions", "Keep the plan"},
		{"move card", `{id: s, title: S, type: task.move, with: {task: "${{ workflow.inputs.task }}", column: Ready}}`, "column", "Ready"},
		{"review passes", `{id: s, title: S, type: github.pr.review_passes, with: {repo: acme/widget, pr_number: 12}}`, "pr", "acme/widget#12"},
		{"repository", `{id: s, title: S, type: repo.ensure, with: {repo: acme/widget}}`, "repo", "acme/widget"},
		{"remove a file", `{id: s, title: S, type: file.remove, with: {working_dir: /w, path: notes.md}}`, "path", "notes.md"},
		{"note body", `{id: s, title: S, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: "Hello\nthere"}}`, "body", "Hello"},
		{"note from a file", `{id: s, title: S, type: task.note, with: {task: "${{ workflow.inputs.task }}", body_path: out.md}}`, "body_path", "out.md"},
		{"metadata", `{id: s, title: S, type: task.metadata.set, with: {task: "${{ workflow.inputs.task }}", values: {triaged: true, area: web}}}`, "values", "area, triaged"},
		{"gate", `{id: s, title: S, type: gate, with: {title: Ship it, options: [Ship, Stop]}}`, "title", "Ship it"},
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
			// Each step but the first is reached in order, or through a
			// branch into its if: (ticket-to-merged-pr's outcomes).
			into := len(edgesOfKind(graph, EdgeOrder))
			for _, edge := range graph.Edges {
				if edge.Kind == EdgeBranch && !edge.Skip {
					into++
				}
			}
			if into != len(definition.Steps)-1 {
				t.Fatalf("%d order and branch edges for %d steps", into, len(definition.Steps))
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

func TestGraphDrawsTheReviewLoopAsAGroup(t *testing.T) {
	example := OldReviewPassesCopy()
	definition, err := Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	graph := BuildGraph(definition)
	group := nodeByID(graph, "step:review")
	if group == nil || group.Kind != NodeStep || group.Step.Group != GroupReviewLoop {
		t.Fatalf("group = %+v", group)
	}
	if group.Step.Key.Value != "${{ workflow.inputs.repo }}#${{ workflow.inputs.pr_number }}" {
		t.Fatalf("key = %+v", group.Step.Key)
	}
	pass, fixes := nodeByID(graph, "step:review/pass"), nodeByID(graph, "step:review/fixes")
	if pass == nil || pass.Kind != NodeInner || pass.Parent != "step:review" || pass.Inner.Title != "Review pass" {
		t.Fatalf("pass = %+v", pass)
	}
	if fixes == nil || fixes.Kind != NodeInner || fixes.Parent != "step:review" || fixes.Inner.Title != "Author fixes" {
		t.Fatalf("fixes = %+v", fixes)
	}
	if got, want := edgesOfKind(graph, EdgeLoop), [][2]string{{"step:review/pass", "step:review/fixes"}, {"step:review/fixes", "step:review/pass"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loop edges = %v", got)
	}
	if got := edgesOfKind(graph, EdgeOrder); !reflect.DeepEqual(got, [][2]string{{"step:review", "step:note"}}) {
		t.Fatalf("order edges attach to the group: %v", got)
	}
	if _, ok := dataEdges(graph)[[2]string{"step:review", "step:note"}]; !ok {
		t.Fatal("data edges attach to the group")
	}
}

const chatTriggers = `
schema_version: 1
name: ops
title: Ops
on:
  chat.message:
    bot: slack
    channel: C1
  chat.mention:
    bot: discord
    enabled: false
    fallback: true
steps:
  - {id: say, title: Say, type: chat.message.send, with: {bot: slack, channel: C1, text: "On it"}}
  - {id: seen, title: Seen, type: chat.reaction.add, with: {bot: slack, channel: C1, message_id: "1.5", emoji: eyes}}
  - {id: unseen, title: Unseen, type: chat.reaction.remove, with: {bot: slack, channel: C1, message_id: "1.5", emoji: eyes}}
  - {id: raise, title: Raise, type: ticket.create, with: {provider: jira, project: AG, summary: Login fails}}
  - {id: seen-again, title: Seen again, type: ticket.comment, with: {key: AG-12, body: Seen again}}
  - {id: take, title: Take it, type: ticket.update, with: {key: AG-12, assignee: me, status: In Progress}}
`

func TestGraphLabelsChatTriggersAndSends(t *testing.T) {
	graph := BuildGraph(mustParse(t, chatTriggers))
	for kind, summary := range map[string]string{TriggerChatMessage: "Message in C1 on slack", TriggerChatMention: "Mention of discord (fallback)"} {
		node := nodeByID(graph, "trigger:"+kind)
		if node == nil || node.Trigger == nil || node.Trigger.Summary != summary || node.Trigger.Definition == nil {
			t.Fatalf("%s node = %+v", kind, node)
		}
	}
	if node := nodeByID(graph, "trigger:"+TriggerChatMention); node.Trigger.Enabled {
		t.Fatal("a paused mention trigger is enabled in the graph")
	}
	if node := nodeByID(graph, "step:say"); node == nil || node.Step == nil || node.Step.Key != (GraphKey{Field: "text", Value: "On it"}) {
		t.Fatalf("say node = %+v", node)
	}
	for id, key := range map[string]GraphKey{"step:raise": {Field: "summary", Value: "Login fails"}, "step:seen-again": {Field: "key", Value: "AG-12"}, "step:take": {Field: "key", Value: "AG-12"}} {
		if node := nodeByID(graph, id); node == nil || node.Step == nil || node.Step.Key != key {
			t.Fatalf("%s node = %+v", id, node)
		}
	}
	for _, id := range []string{"step:seen", "step:unseen"} {
		if node := nodeByID(graph, id); node == nil || node.Step == nil || node.Step.Key != (GraphKey{Field: "emoji", Value: "eyes"}) {
			t.Fatalf("%s node = %+v", id, node)
		}
	}
}

// An if: reads earlier steps' outputs as a with value does: the graph draws
// those data edges too.
func TestAnIfDrawsDataEdges(t *testing.T) {
	definition := mustParse(t, "schema_version: 2\nname: k\ntitle: K\nsteps:\n  - {id: pass, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n  - {id: note, if: {field: steps.pass.outputs.content, empty: false}, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n")
	for _, edge := range BuildGraph(definition).Edges {
		if edge.Source == "step:pass" && edge.Target == "step:note" && edge.Kind == "data" {
			return
		}
	}
	t.Fatalf("no data edge from pass to note: %+v", BuildGraph(definition).Edges)
}

// A chat button's if: reads earlier steps' outputs as a step's if: does.
func TestAChatButtonsIfDrawsDataEdges(t *testing.T) {
	definition := mustParse(t, "schema_version: 2\nname: k\ntitle: K\nsteps:\n  - {id: pass, type: task.context, with: {task: \"${{ workflow.task }}\"}}\n  - {id: say, type: chat.message.send, with: {bot: slack, channel: C1, text: hi, buttons: [{label: Fix, value: fix, if: {field: steps.pass.outputs.content, empty: false}}]}}\n")
	for _, edge := range BuildGraph(definition).Edges {
		if edge.Source == "step:pass" && edge.Target == "step:say" && edge.Kind == "data" {
			return
		}
	}
	t.Fatalf("no data edge from pass to say: %+v", BuildGraph(definition).Edges)
}
