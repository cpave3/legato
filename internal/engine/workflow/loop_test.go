package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// The acceptance sketch of spec LEG-559 (review-loop), with its review
// steps, which do not exist yet, named by task.context: two repeat loops
// whose bodies hold if:-guarded steps, pass outputs that pick the step
// that ran with ??, reads of a step outside the loop, and a step after the
// loop reading the last pass.
const ReviewLoopSketch = `schema_version: 2
name: review-loop-sketch
title: Review loop sketch
inputs:
  ready: {type: boolean, default: false}
  steward: {type: boolean, default: false}
steps:
  - id: open
    type: task.context
    with: {task: "${{ workflow.task }}"}
    outputs: {loop_id: {type: string}}
  - id: review
    loop: {repeat: true, max: 100, until: {field: loop.last.state, in: [approved, closed, stuck]}}
    outputs:
      state: {type: string, value: "${{ steps.confirm.outputs.state ?? steps.author.outputs.state ?? steps.pass.outputs.state }}"}
      verdict: {type: string, value: "${{ steps.pass.outputs.verdict }}"}
    steps:
      - {id: pass, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}, outputs: {verdict: {type: string}, state: {type: string}}}
      - {id: confirm, if: {field: steps.pass.outputs.verdict, equals: clean}, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}, outputs: {state: {type: string}}}
      - {id: author, if: {field: steps.pass.outputs.verdict, equals: changes}, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}", mode: author}, outputs: {state: {type: string}}}
  - id: ready
    if: {all: [{field: workflow.inputs.ready, equals: true}, {field: steps.review.outputs.last.state, equals: approved}]}
    type: task.context
    with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}
  - id: steward
    if: {all: [{field: workflow.inputs.steward, equals: true}, {field: steps.review.outputs.last.state, equals: approved}]}
    loop: {repeat: true, max: 100, until: {field: loop.last.woke, in: [merged, closed, idle, ended]}}
    outputs:
      woke: {type: string, value: "${{ steps.watch.outputs.woke }}"}
    steps:
      - {id: watch, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}", mode: watch}, outputs: {woke: {type: string}}}
      - {id: pass, if: {field: steps.watch.outputs.woke, in: [pushed, disputed]}, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}, outputs: {verdict: {type: string}}}
      - {id: confirm, if: {field: steps.pass.outputs.verdict, equals: clean}, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}, outputs: {state: {type: string}}}
      - {id: ready, if: {all: [{field: workflow.inputs.ready, equals: true}, {field: steps.confirm.outputs.state, equals: approved}]}, type: task.context, with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}"}}
  - id: close
    type: task.context
    with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}", outcome: "${{ steps.review.outputs.last.state }}"}
`

// LEG-480's example B, with the ticket steps LEG-488 builds named by
// task.context (tickets.list as the next: query, ticket.import as card).
const ExampleB = `schema_version: 2
name: build-milestone-by-ticket
title: Build a milestone, one agent per ticket
inputs:
  milestone: {type: string, required: true}
  repo:      {type: repository, required: true}
  base:      {type: string}
outputs:
  top_branch: {type: string, value: "${{ steps.push.outputs.top_branch }}"}
  prs:        {type: json,   value: "${{ steps.push.outputs.prs }}"}
steps:
  - id: tickets
    loop:
      next:
        type: task.context
        with: {task: "${{ workflow.task }}", milestone: "${{ workflow.inputs.milestone }}", kind: work, frontier: true}
      key: "${{ loop.item.key }}"
      max: 30
      on_failure: pause
    outputs:
      branch:   {type: string,  value: "${{ steps.build.outputs.branch }}"}
      clone_id: {type: integer, value: "${{ steps.build.outputs.clone_id }}"}
    steps:
      - id: card
        type: task.context
        with: {task: "${{ workflow.task }}", key: "${{ loop.item.key }}"}
        outputs: {task: {type: string}}
      - id: build
        type: agent.run
        with:
          on: "${{ steps.card.outputs.task }}"
          repo: "${{ workflow.inputs.repo.repo }}"
          ref: "${{ workflow.inputs.base ?? workflow.inputs.repo.base_branch }}"
          clone: "${{ loop.previous.clone_id ?? null }}"
          branch: "${{ loop.item.key }}"
          stop: true
          prompt: |
            Build ${{ loop.item.key }} on this branch. Report finished when it is committed.
        wait_for: [finished]
        timeout: 4h
  - id: push
    type: agent.run
    with:
      repo: "${{ workflow.inputs.repo.repo }}"
      clone: "${{ steps.tickets.outputs.results[-1].clone_id }}"
      prompt: Push every branch of this clone's stack and open its pull requests, bottom first.
    wait_for: [finished]
`

// LEG-480's example C, as written: a next: loop over milestones whose
// pass calls a workflow, each stacking on the last.
const ExampleC = `schema_version: 2
name: proposal-project
title: Build every ready milestone of a project
inputs:
  project: {type: string, required: true}
  repo:    {type: repository, required: true}
  base:    {type: string}
  builder: {type: string, default: build-milestone, choices: [build-milestone, build-milestone-by-ticket]}
outputs:
  milestones: {type: json, value: "${{ steps.milestones.outputs.results }}"}
steps:
  - id: milestones
    loop:
      next:
        type: task.context
        with: {task: "${{ workflow.task }}", project: "${{ workflow.inputs.project }}"}
      key: "${{ loop.item.key }}"
      max: 10
      until: {field: loop.last.review, equals: stuck}
      on_failure: pause
    outputs:
      milestone:  {type: string, value: "${{ loop.item.key }}"}
      top_branch: {type: string, value: "${{ steps.build.outputs.top_branch }}"}
      prs:        {type: json,   value: "${{ steps.build.outputs.prs }}"}
      review:     {type: string, value: "${{ steps.build.outputs.review }}"}
    steps:
      - id: build
        type: workflow.run
        with:
          workflow: "${{ workflow.inputs.builder }}"
          inputs:
            milestone: "${{ loop.item.key }}"
            repo: "${{ workflow.inputs.repo }}"
            base: "${{ loop.previous.top_branch ?? workflow.inputs.base }}"
  - id: tell
    type: task.note
    with:
      task: "${{ workflow.task }}"
      note: "${{ steps.milestones.outputs.count }} milestones built for ${{ workflow.inputs.project }}"
`

// LEG-480's example D, with tasks.list named by task.context and deliver by
// task.note: an over: loop of board cards, each pass continued on failure.
const ExampleD = `schema_version: 2
name: triage-backlog
title: Triage the cards waiting in Triage
steps:
  - id: find
    type: task.context
    with: {task: "${{ workflow.task }}", column: Triage, limit: 15}
    outputs: {items: {type: json}, count: {type: integer}}
  - id: each
    if: {field: steps.find.outputs.count, gt: 0}
    loop: {over: "${{ steps.find.outputs.items }}", max: 15, on_failure: continue}
    steps:
      - id: triage
        type: workflow.run
        with: {workflow: triage, inputs: {task: "${{ loop.item.id }}"}}
  - id: tell
    type: task.note
    if: {field: steps.each.outputs.count, gt: 0}
    with: {task: "${{ workflow.task }}", note: "Triaged ${{ steps.each.outputs.succeeded }} cards, ${{ steps.each.outputs.failed }} failed"}
`

func TestTheLoopExamplesValidate(t *testing.T) {
	for name, text := range map[string]string{"review-loop": ReviewLoopSketch, "B": ExampleB, "C": ExampleC, "D": ExampleD} {
		if _, err := workflow.Parse(name+".yaml", []byte(text)); err != nil {
			t.Errorf("example %s: %v", name, err)
		}
	}
}

func TestALoopStepHasItsLoopAndBody(t *testing.T) {
	definition, err := workflow.Parse("b.yaml", []byte(ExampleB))
	if err != nil {
		t.Fatal(err)
	}
	loop := definition.Steps[0]
	if loop.Loop == nil || loop.Loop.Next == nil || loop.Loop.Next.Type != "task.context" || loop.Loop.Key != "${{ loop.item.key }}" || loop.Loop.Limit() != 30 || loop.Loop.Failure() != workflow.OnFailurePause {
		t.Fatalf("loop = %+v", loop.Loop)
	}
	if len(loop.Steps) != 2 || loop.Steps[1].ID != "build" {
		t.Fatalf("body = %+v", loop.Steps)
	}
	if loop.Outputs["clone_id"].Value != "${{ steps.build.outputs.clone_id }}" {
		t.Fatalf("pass outputs = %+v", loop.Outputs)
	}
	repeat := workflow.Loop{Repeat: true}
	if repeat.Limit() != workflow.DefaultLoopMax || repeat.Failure() != workflow.OnFailurePause {
		t.Fatalf("defaults: max %d, on_failure %s", repeat.Limit(), repeat.Failure())
	}
}

// loopFile is a workflow with one step before a loop, the loop, and one
// step after it; body and after are spliced in.
func loopFile(loop, outputs, body, after string) string {
	text := `schema_version: 2
name: loops
title: Loops
inputs:
  items: {type: json}
  label: {type: string}
steps:
  - id: before
    type: task.context
    with: {task: "${{ workflow.task }}"}
    outputs: {list: {type: json}, name: {type: string}}
  - id: each
    loop: ` + loop + `
`
	if outputs != "" {
		text += "    outputs:\n" + outputs
	}
	text += "    steps:\n" + body
	if after != "" {
		text += after
	}
	return text
}

const plainBody = `      - {id: work, type: task.context, with: {task: "${{ workflow.task }}"}, outputs: {result: {type: string}}}
`

func TestLoopsAreChecked(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"no form", loopFile("{max: 3}", "", plainBody, ""), "one of over, next or repeat"},
		{"two forms", loopFile(`{repeat: true, over: "${{ workflow.inputs.items }}"}`, "", plainBody, ""), "one of over, next or repeat"},
		{"next without key", loopFile("{next: {type: task.context, with: {task: \"${{ workflow.task }}\"}}}", "", plainBody, ""), "key is required with next"},
		{"key on repeat", loopFile(`{repeat: true, key: "${{ loop.index }}"}`, "", plainBody, ""), "key"},
		{"max too big", loopFile("{repeat: true, max: 101}", "", plainBody, ""), "max"},
		{"max negative", loopFile("{repeat: true, max: -1}", "", plainBody, ""), "max"},
		{"bad on_failure", loopFile("{repeat: true, on_failure: retry}", "", plainBody, ""), "on_failure"},
		{"no body", loopFile("{repeat: true}", "", "      []\n", ""), "steps"},
		{"over not json", loopFile(`{over: "${{ workflow.inputs.label }}"}`, "", plainBody, ""), "list"},
		{"unknown next type", loopFile(`{next: {type: tickets.nope}, key: "${{ loop.item.key }}"}`, "", plainBody, ""), "tickets.nope"},
		{"reserved pass output", loopFile("{repeat: true}", "      status: {type: string, value: \"${{ steps.work.outputs.result }}\"}\n", plainBody, ""), "reserved"},
		{"pass output without value", loopFile("{repeat: true}", "      result: {type: string}\n", plainBody, ""), "value"},
		{"pass output of no step", loopFile("{repeat: true}", "      result: {type: string, value: \"${{ steps.nope.outputs.result }}\"}\n", plainBody, ""), "nope"},
		{"pass output type", loopFile("{repeat: true}", "      result: {type: file, value: \"${{ steps.work.outputs.result }}\"}\n", plainBody, ""), "type"},
		{"loop.last in a body", loopFile("{repeat: true}", "", `      - {id: work, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ loop.last.state }}"}}
`, ""), "loop.last"},
		{"loop.previous undeclared", loopFile("{repeat: true}", "", `      - {id: work, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ loop.previous.nope }}"}}
`, ""), "nope"},
		{"until reads an undeclared result", loopFile("{repeat: true, until: {field: loop.last.nope, equals: x}}", "", plainBody, ""), "nope"},
		{"a later body step", loopFile("{repeat: true}", "", `      - {id: first, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ steps.second.outputs.content }}"}}
      - {id: second, type: task.context, with: {task: "${{ workflow.task }}"}}
`, ""), "second"},
		{"a body step from after the loop", loopFile("{repeat: true}", "", plainBody, `  - {id: after, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ steps.work.outputs.result }}"}}
`), "work"},
		{"loop outside a loop", loopFile("{repeat: true}", "", plainBody, `  - {id: after, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ loop.index }}"}}
`), "loop"},
		{"a loop with a type", strings.Replace(loopFile("{repeat: true}", "", plainBody, ""), "    loop: {repeat: true}", "    type: task.context\n    loop: {repeat: true}", 1), "type"},
		{"steps without a loop", strings.Replace(loopFile("{repeat: true}", "", plainBody, ""), "    loop: {repeat: true}\n", "", 1), "loop"},
		{"a loop with a timeout", strings.Replace(loopFile("{repeat: true}", "", plainBody, ""), "    loop: {repeat: true}", "    timeout: 1h\n    loop: {repeat: true}", 1), "timeout"},
		{"a body step called next", loopFile(`{next: {type: task.context, with: {task: "${{ workflow.task }}"}}, key: "${{ loop.item.key }}"}`, "", `      - {id: next, type: task.context, with: {task: "${{ workflow.task }}"}}
`, ""), "next"},
		{"a slash in an ID", loopFile("{repeat: true}", "", `      - {id: a/b, type: task.context, with: {task: "${{ workflow.task }}"}}
`, ""), "/"},
		{"duplicate body IDs", loopFile("{repeat: true}", "", plainBody+plainBody, ""), "unique"},
		{"a manual loop", strings.Replace(loopFile("{repeat: true}", "", plainBody, ""), "    loop: {repeat: true}", "    start: manual\n    loop: {repeat: true}", 1), "gate"},
	}
	for _, tc := range cases {
		_, err := workflow.Parse(tc.name+".yaml", []byte(tc.text))
		if err == nil {
			t.Errorf("%s: Parse accepted it", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

func TestALoopIsAVersion2Key(t *testing.T) {
	text := strings.Replace(loopFile("{repeat: true}", "", plainBody, ""), "schema_version: 2", "schema_version: 1", 1)
	text = strings.Replace(text, "  items: {type: json}\n  label: {type: string}\n", "  items: {type: json}\n", 1)
	if _, err := workflow.Parse("v1.yaml", []byte(text)); err == nil || !strings.Contains(err.Error(), "schema_version 2") {
		t.Fatalf("Parse = %v, want a schema_version 2 refusal", err)
	}
}

func TestLoopReadsThatAreAllowed(t *testing.T) {
	text := loopFile(`{over: "${{ steps.before.outputs.list }}", key: "${{ loop.item.id }}", until: {any: [{field: loop.last.result, equals: done}, {field: loop.last.status, equals: failed}, {field: loop.index, gt: 3}, {field: steps.before.outputs.name, equals: x}]}}`,
		"      result: {type: string, value: \"${{ steps.work.outputs.result ?? 'none' }}\"}\n      item: {type: json, value: \"${{ loop.item }}\"}\n",
		`      - id: work
        type: task.context
        with:
          task: "${{ workflow.task }}"
          from_before: "${{ steps.before.outputs.name }}"
          index: "${{ loop.index }}"
          item: "${{ loop.item.id }}"
          previous: "${{ loop.previous.result ?? 'first' }}"
          previous_status: "${{ loop.previous.status }}"
          results: "${{ loop.results[-1].result ?? 'none' }}"
        outputs: {result: {type: string}}
      - id: again
        if: {field: steps.work.outputs.result, equals: x}
        type: task.context
        with: {task: "${{ workflow.task }}", got: "${{ steps.work.outputs.result }}"}
`,
		`  - id: after
    if: {field: steps.each.outputs.last.result, equals: done}
    type: task.context
    with:
      task: "${{ workflow.task }}"
      count: "${{ steps.each.outputs.count }}"
      ended: "${{ steps.each.outputs.ended_by }}"
      last: "${{ steps.each.outputs.last.result }}"
      first: "${{ steps.each.outputs.results[0].result }}"
`)
	if _, err := workflow.Parse("ok.yaml", []byte(text)); err != nil {
		t.Fatal(err)
	}
}

// A body step's ID may be one an enclosing scope has: the body's own is
// the nearest.
func TestABodyStepMayShadowAnEnclosingID(t *testing.T) {
	text := loopFile("{repeat: true}", "", `      - {id: before, type: task.context, with: {task: "${{ workflow.task }}"}, outputs: {other: {type: string}}}
      - {id: use, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ steps.before.outputs.other }}"}}
`, "")
	if _, err := workflow.Parse("shadow.yaml", []byte(text)); err != nil {
		t.Fatal(err)
	}
	refused := loopFile("{repeat: true}", "", `      - {id: before, type: task.context, with: {task: "${{ workflow.task }}"}, outputs: {other: {type: string}}}
      - {id: use, type: task.context, with: {task: "${{ workflow.task }}", x: "${{ steps.before.outputs.name }}"}}
`, "")
	if _, err := workflow.Parse("shadow.yaml", []byte(refused)); err == nil {
		t.Fatal("the body's before has no output name: the enclosing one is hidden")
	}
}

// Version 1 allowed a slash in a step ID: its paths are its IDs, read
// whole, not split into a loop's pass.
func TestAVersion1StepIDWithASlashIsItsOwnPath(t *testing.T) {
	definition, err := workflow.Parse("v1.yaml", []byte(`schema_version: 1
name: slashes
title: Slashes
steps:
  - {id: build/test, type: task.context, with: {task: "${{ workflow.task }}"}, outputs: {done: {type: string}}}
  - {id: after, type: task.context, with: {task: "${{ workflow.task }}", done: "${{ steps.build/test.outputs.done }}"}}
`))
	if err != nil {
		t.Fatal(err)
	}
	step, err := definition.StepAt("build/test")
	if err != nil || step.ID != "build/test" {
		t.Fatalf("StepAt = %+v, %v", step, err)
	}
	if next, err := definition.NextPath("build/test"); err != nil || next != "after" {
		t.Fatalf("NextPath = %q, %v", next, err)
	}
	if path, _, err := definition.Nearest("after", "build/test"); err != nil || path != "build/test" {
		t.Fatalf("Nearest = %q, %v", path, err)
	}
}
