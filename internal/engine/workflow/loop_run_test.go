package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
	"gopkg.in/yaml.v3"
)

func parse(t *testing.T, text string) *workflow.Definition {
	t.Helper()
	definition, err := workflow.Parse("loop.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestStepPathsWalkIntoLoopPasses(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	b := parse(t, ExampleB)
	for _, tc := range []struct {
		definition *workflow.Definition
		path, id   string
		typ        string
		next       string
	}{
		{review, "open", "open", "task.context", "review"},
		{review, "review", "review", "", "ready"},
		{review, "review/0/pass", "pass", "task.context", "review/0/confirm"},
		{review, "review/3/author", "author", "task.context", "review"},
		{review, "steward/1/ready", "ready", "task.context", "steward"},
		{review, "close", "close", "task.context", ""},
		{b, "tickets/2/next", "next", "task.context", "tickets"},
		{b, "tickets/2/card", "card", "task.context", "tickets/2/build"},
	} {
		step, err := tc.definition.StepAt(tc.path)
		if err != nil {
			t.Errorf("StepAt(%s): %v", tc.path, err)
			continue
		}
		if step.ID != tc.id || step.Type != tc.typ {
			t.Errorf("StepAt(%s) = %s (%s)", tc.path, step.ID, step.Type)
		}
		next, err := tc.definition.NextPath(tc.path)
		if err != nil || next != tc.next {
			t.Errorf("NextPath(%s) = %q, %v; want %q", tc.path, next, err, tc.next)
		}
	}
	for _, bad := range []string{"nope", "review/pass", "review/x/pass", "open/0/pass", "review/0/nope", "review/0/next", "tickets/0/next/more"} {
		if _, err := review.StepAt(bad); err == nil {
			if _, err := b.StepAt(bad); err == nil {
				t.Errorf("StepAt(%s) found a step", bad)
			}
		}
	}
}

func TestAStepsScopesAreItsPassesOutermostFirst(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	scopes, err := review.Scopes("review/2/author")
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0].Path != "" || scopes[0].Before != 1 || scopes[1].Path != "review/2" || scopes[1].Before != 2 || scopes[1].Loop != "review" || scopes[1].Pass != 2 {
		t.Fatalf("scopes = %+v", scopes)
	}
	pass, err := review.PassScopes("review", 2)
	if err != nil {
		t.Fatal(err)
	}
	if last := pass[len(pass)-1]; last.Path != "review/2" || last.Before != 3 {
		t.Fatalf("pass scopes = %+v", pass)
	}
}

// A body step reads the step outside the loop and the loop context.
func TestABodyStepResolvesInItsScope(t *testing.T) {
	b := parse(t, ExampleB)
	state := workflow.LoopState{Passes: []workflow.LoopPass{
		{Index: 0, Item: map[string]any{"key": "LEG-1"}, Result: map[string]any{"status": "succeeded", "error": "", "clone_id": 7, "branch": "legato/a"}},
		{Index: 1, Item: map[string]any{"key": "LEG-2"}, Result: map[string]any{"status": "failed", "error": "boom"}},
		{Index: 2, Item: map[string]any{"key": "LEG-3"}, Body: true, Open: true},
	}}
	scope := workflow.Scope{
		Inputs: map[string]any{"repo": map[string]any{"repo": "o/r", "base_branch": "main"}},
		Steps:  map[string]map[string]any{"card": {"task": "T3"}},
		Loop:   workflow.LoopContext(state, 2),
	}
	resolved, err := b.ResolveStepAt("tickets/2/build", scope)
	if err != nil {
		t.Fatal(err)
	}
	// loop.previous is the newest pass that succeeded: pass 0, not 1.
	if resolved.With["clone"] != 7 || resolved.With["branch"] != "LEG-3" || resolved.With["on"] != "T3" || resolved.With["ref"] != "main" {
		t.Fatalf("with = %v", resolved.With)
	}
	first := workflow.LoopContext(workflow.LoopState{Passes: []workflow.LoopPass{{Index: 0, Item: map[string]any{"key": "LEG-1"}, Body: true, Open: true}}}, 0)
	scope.Loop = first
	resolved, err = b.ResolveStepAt("tickets/0/build", scope)
	if err != nil {
		t.Fatal(err)
	}
	if clone, given := resolved.With["clone"]; !given || clone != nil {
		t.Fatalf("the first pass's clone = %v (%v), want null: a fresh clone", clone, given)
	}
}

// The pass result rule (LEG-559 X1): a body's outputs are value
// expressions over its steps, ?? picking the step that ran; every result
// has status and error.
func TestAPassResultPicksTheStepThatRan(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	clean := workflow.Scope{Steps: map[string]map[string]any{"pass": {"verdict": "clean", "state": "reviewed"}, "confirm": {"state": "approved"}}}
	if got := review.PassResult("review", clean, workflow.PassSucceeded, ""); got["state"] != "approved" || got["verdict"] != "clean" || got["status"] != "succeeded" || got["error"] != "" {
		t.Fatalf("clean pass = %v", got)
	}
	changes := workflow.Scope{Steps: map[string]map[string]any{"pass": {"verdict": "changes", "state": "reviewed"}, "author": {"state": "pushed"}}}
	if got := review.PassResult("review", changes, workflow.PassSucceeded, ""); got["state"] != "pushed" {
		t.Fatalf("changes pass = %v", got)
	}
	failed := review.PassResult("review", clean, workflow.PassFailed, "the pass broke")
	if len(failed) != 2 || failed["status"] != "failed" || failed["error"] != "the pass broke" {
		t.Fatalf("a failed pass's result = %v, want only status and error", failed)
	}
	huge := workflow.Scope{Steps: map[string]map[string]any{"pass": {"verdict": strings.Repeat("x", workflow.PassResultLimit), "state": "reviewed"}}}
	if got := review.PassResult("review", huge, workflow.PassSucceeded, ""); got["status"] != "failed" || !strings.Contains(got["error"].(string), "16 KiB") || len(got) != 2 {
		t.Fatalf("an oversized result = %v", got)
	}
}

func TestUntilReadsTheLastPass(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	state := workflow.LoopState{Passes: []workflow.LoopPass{{Index: 0, Result: map[string]any{"status": "succeeded", "error": "", "state": "pushed"}}}}
	if done, err := review.LoopUntil("review", workflow.Scope{Loop: workflow.UntilContext(state)}); err != nil || done {
		t.Fatalf("until after pushed = %v, %v", done, err)
	}
	state.Passes = append(state.Passes, workflow.LoopPass{Index: 1, Result: map[string]any{"status": "succeeded", "error": "", "state": "approved"}})
	if done, err := review.LoopUntil("review", workflow.Scope{Loop: workflow.UntilContext(state)}); err != nil || !done {
		t.Fatalf("until after approved = %v, %v", done, err)
	}
	// A failed pass has no state: until is false and the loop goes on.
	state.Passes = append(state.Passes, workflow.LoopPass{Index: 2, Result: map[string]any{"status": "failed", "error": "x"}})
	if done, err := review.LoopUntil("review", workflow.Scope{Loop: workflow.UntilContext(state)}); err != nil || done {
		t.Fatalf("until after a failed pass = %v, %v", done, err)
	}
}

func TestALoopsOutputsCountItsPasses(t *testing.T) {
	state := workflow.LoopState{EndedBy: workflow.LoopEndedUntil, Passes: []workflow.LoopPass{
		{Index: 0, Result: map[string]any{"status": "succeeded", "error": "", "state": "pushed"}},
		{Index: 1, Result: map[string]any{"status": "failed", "error": "x"}},
		{Index: 2, Result: map[string]any{"status": "succeeded", "error": "", "state": "approved"}},
	}}
	outputs := state.Outputs()
	if outputs["count"] != 3 || outputs["succeeded"] != 2 || outputs["failed"] != 1 || outputs["ended_by"] != "until" || outputs["last"].(map[string]any)["state"] != "approved" || len(outputs["results"].([]any)) != 3 {
		t.Fatalf("outputs = %v", outputs)
	}
	empty := workflow.LoopState{EndedBy: workflow.LoopEndedItems}.Outputs()
	if _, has := empty["last"]; has || empty["count"] != 0 {
		t.Fatalf("a loop with no pass = %v", empty)
	}
	encoded := state.Encode()
	decoded, err := workflow.ParseLoopState(encoded)
	if err != nil || len(decoded.Passes) != 3 || decoded.EndedBy != "until" {
		t.Fatalf("ParseLoopState(%s) = %+v, %v", encoded, decoded, err)
	}
}

func TestOverItemsAndKeys(t *testing.T) {
	d := parse(t, ExampleD)
	items, err := d.LoopItems("each", workflow.Scope{Steps: map[string]map[string]any{"find": {"items": []any{map[string]any{"id": "A"}, map[string]any{"id": "B"}}}}})
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %v, %v", items, err)
	}
	if items, err := d.LoopItems("each", workflow.Scope{Steps: map[string]map[string]any{}}); err != nil || len(items) != 0 {
		t.Fatalf("a missing list = %v, %v; want no items", items, err)
	}
	if _, err := d.LoopItems("each", workflow.Scope{Steps: map[string]map[string]any{"find": {"items": "nope"}}}); err == nil {
		t.Fatal("a list that is not one was taken")
	}
	b := parse(t, ExampleB)
	key, err := b.LoopKey("tickets", workflow.Scope{Loop: map[string]any{"item": map[string]any{"key": "LEG-9"}}})
	if err != nil || key != "LEG-9" {
		t.Fatalf("key = %q, %v", key, err)
	}
	if _, err := b.LoopKey("tickets", workflow.Scope{Loop: map[string]any{"item": map[string]any{"title": "no key"}}}); err == nil {
		t.Fatal("an item with no key was given one")
	}
	// An over: loop with no key tells items apart by their JSON.
	key, err = d.LoopKey("each", workflow.Scope{Loop: map[string]any{"item": map[string]any{"id": "A"}}})
	if err != nil || key != `{"id":"A"}` {
		t.Fatalf("key = %q, %v", key, err)
	}
}

func TestALoopWritesBackAsYAML(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	data, err := yaml.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "start: automatic") {
		t.Fatalf("a body step says start: automatic:\n%s", data)
	}
	again, err := workflow.Parse("again.yaml", data)
	if err != nil {
		t.Fatalf("the written file does not load: %v\n%s", err, data)
	}
	if again.Steps[1].Loop == nil || len(again.Steps[1].Steps) != 3 || again.Steps[1].Outputs["state"].Value == nil {
		t.Fatalf("the loop did not come back: %+v", again.Steps[1])
	}
}

// Graph version 2 (LEG-484) draws a loop's body: the data a body step reads
// from outside comes into that step's node, inside the loop's.
func TestTheGraphDrawsALoopsBody(t *testing.T) {
	graph := workflow.BuildGraph(parse(t, ReviewLoopSketch))
	var loop *workflow.GraphStep
	nodes := map[string]bool{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = true
		if node.ID == "step:review" {
			loop = node.Step
		}
	}
	if loop == nil || loop.Type != workflow.LoopStepType {
		t.Fatalf("loop node = %+v", loop)
	}
	edges := map[string][]string{}
	for _, edge := range graph.Edges {
		if !nodes[edge.Source] || !nodes[edge.Target] {
			t.Errorf("edge %s joins a node the graph does not have", edge.ID)
		}
		edges[edge.ID] = edge.Outputs
	}
	if got := edges["data:step:open>step:review/pass"]; strings.Join(got, ",") != "loop_id" {
		t.Errorf("open → review/pass data = %v", got)
	}
	if got := edges["data:step:review/pass>step:review"]; strings.Join(got, ",") != "state,verdict" {
		t.Errorf("review/pass → review data = %v", got)
	}
	if got := edges["data:step:review>step:close"]; strings.Join(got, ",") != "last" {
		t.Errorf("review → close data = %v", got)
	}
}

// A workflow.run in a loop's pass is a call: the call graph and the
// checks of its inputs and outputs see it.
func TestACallInALoopIsChecked(t *testing.T) {
	c := parse(t, ExampleC)
	if !c.Calls() {
		t.Fatal("Calls() missed the call in the loop")
	}
	child := parse(t, `schema_version: 2
name: build-milestone
title: Build
inputs:
  milestone: {type: string, required: true}
  repo: {type: repository, required: true}
  base: {type: string}
outputs:
  top_branch: {type: string, value: "${{ steps.a.outputs.content }}"}
steps:
  - {id: a, type: task.context, with: {task: "${{ workflow.task }}"}}
`)
	diagnostics := workflow.CallDiagnostics([]byte(ExampleC), c, []*workflow.Definition{child})
	var outputs []string
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "does not declare") {
			outputs = append(outputs, diagnostic.Message)
		}
	}
	// The pass reads prs and review, which build-milestone does not
	// declare; the other choice is not found.
	if len(outputs) != 2 || !strings.Contains(strings.Join(outputs, "\n"), `"review"`) {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	self := parse(t, strings.Replace(ExampleC, "choices: [build-milestone, build-milestone-by-ticket]", "choices: [proposal-project]", 1))
	found := false
	for _, diagnostic := range workflow.CallDiagnostics([]byte(ExampleC), self, nil) {
		found = found || diagnostic.Severity == workflow.SeverityError
	}
	if !found {
		t.Fatal("a loop's call of its own workflow was not an error")
	}
}

func TestNearestFindsTheStepOfAnIDInTheClosestScope(t *testing.T) {
	review := parse(t, ReviewLoopSketch)
	for _, tc := range []struct{ at, id, want string }{
		{"review/2/author", "pass", "review/2/pass"},
		{"review/2/author", "open", "open"},
		{"steward/0/confirm", "pass", "steward/0/pass"},
		{"close", "review", "review"},
	} {
		got, step, err := review.Nearest(tc.at, tc.id)
		if err != nil || got != tc.want || step.ID != tc.id {
			t.Errorf("Nearest(%s, %s) = %s, %v", tc.at, tc.id, got, err)
		}
	}
	for _, tc := range []struct{ at, id string }{{"review/0/pass", "confirm"}, {"close", "pass"}, {"open", "close"}} {
		if got, _, err := review.Nearest(tc.at, tc.id); err == nil {
			t.Errorf("Nearest(%s, %s) = %s: not before it", tc.at, tc.id, got)
		}
	}
}
