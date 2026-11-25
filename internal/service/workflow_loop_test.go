package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// funcExecutor runs each start through run, which sees the step and how
// many times a step of that ID has started before.
type funcExecutor struct {
	typeName string
	run      func(step ResolvedWorkflowStep, count int) (WorkflowExecutionResult, error)
	starts   []ResolvedWorkflowStep
	counts   map[string]int
}

func (e *funcExecutor) Type() string { return e.typeName }
func (e *funcExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if e.counts == nil {
		e.counts = map[string]int{}
	}
	_, id := workflowengine.SplitStepPath(step.StepID)
	count := e.counts[id]
	e.counts[id]++
	e.starts = append(e.starts, step)
	return e.run(step, count)
}
func (e *funcExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func succeeded(outputs map[string]any) (WorkflowExecutionResult, error) {
	return WorkflowExecutionResult{Status: "succeeded", Outputs: outputs}, nil
}

// startsOf are the paths of the starts of steps of that ID, in order.
func (e *funcExecutor) startsOf(id string) []ResolvedWorkflowStep {
	var out []ResolvedWorkflowStep
	for _, step := range e.starts {
		if _, stepID := workflowengine.SplitStepPath(step.StepID); stepID == id {
			out = append(out, step)
		}
	}
	return out
}

func loopDefinition(t *testing.T, text string) *workflowengine.Definition {
	t.Helper()
	definition, err := workflowengine.Parse("loop.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func loopOutputs(t *testing.T, view *store.WorkflowRunView, path string) map[string]any {
	t.Helper()
	var outputs map[string]any
	decoder := json.NewDecoder(strings.NewReader(stepOf(t, view, path).OutputsJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&outputs); err != nil {
		t.Fatal(err)
	}
	return outputs
}

// The acceptance sketch of LEG-559 (workflowengine's ReviewLoopSketch, the
// review steps named by task.context), run against fake steps: the review
// repeat ends on loop.last.state, the steward's on loop.last.woke, every
// body step reads the loop ID of the open step outside the loop, and the
// close step after the loops reads steps.review.outputs.last.state.
const reviewLoopSketch = `schema_version: 2
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
  - id: close
    type: task.context
    with: {task: "${{ workflow.task }}", loop: "${{ steps.open.outputs.loop_id }}", outcome: "${{ steps.review.outputs.last.state }}"}
`

func TestTheReviewLoopSketchRunsAgainstFakeSteps(t *testing.T) {
	// Review: pass 0 asks for changes and the author pushes; pass 1 is
	// clean and confirmed. Steward: wakes idle-free on a push (re-review,
	// clean, confirmed), then the PR merges.
	reviewVerdicts := []string{"changes", "clean"}
	wokes := []string{"pushed", "merged"}
	steps := &funcExecutor{typeName: "task.context", run: func(step ResolvedWorkflowStep, count int) (WorkflowExecutionResult, error) {
		scope, id := workflowengine.SplitStepPath(step.StepID)
		steward := strings.HasPrefix(scope, "steward/")
		switch {
		case id == "open":
			return succeeded(map[string]any{"loop_id": "rl-1"})
		case id == "pass" && !steward:
			return succeeded(map[string]any{"verdict": reviewVerdicts[count], "state": "reviewed"})
		case id == "pass":
			return succeeded(map[string]any{"verdict": "clean"})
		case id == "author":
			return succeeded(map[string]any{"state": "pushed"})
		case id == "confirm":
			return succeeded(map[string]any{"state": "approved"})
		case id == "watch":
			return succeeded(map[string]any{"woke": wokes[count]})
		}
		return succeeded(map[string]any{})
	}}
	svc, _ := controlService(t, steps)
	view, err := svc.Start(context.Background(), loopDefinition(t, reviewLoopSketch), map[string]string{"steward": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %s at %s: %s", view.Run.Status, view.Run.CurrentStepID, view.Run.Error)
	}
	for _, step := range steps.starts {
		if _, id := workflowengine.SplitStepPath(step.StepID); id != "open" && step.With["loop"] != "rl-1" {
			t.Errorf("%s did not read the open step's loop_id: %v", step.StepID, step.With)
		}
	}
	review := loopOutputs(t, view, "review")
	if review["count"] != json.Number("2") || review["ended_by"] != "until" || review["last"].(map[string]any)["state"] != "approved" {
		t.Fatalf("review = %v", review)
	}
	results := review["results"].([]any)
	if first := results[0].(map[string]any); first["state"] != "pushed" || first["status"] != "succeeded" || first["error"] != "" {
		t.Fatalf("the first pass's result = %v: the author's state, picked with ??", first)
	}
	for path, want := range map[string]string{"review/0/confirm": "skipped", "review/0/author": "succeeded", "review/1/confirm": "succeeded", "review/1/author": "skipped", "ready": "skipped"} {
		if got := stepOf(t, view, path).Status; got != want {
			t.Errorf("%s = %s, want %s", path, got, want)
		}
	}
	steward := loopOutputs(t, view, "steward")
	if steward["count"] != json.Number("2") || steward["last"].(map[string]any)["woke"] != "merged" {
		t.Fatalf("steward = %v", steward)
	}
	if got := stepOf(t, view, "steward/1/pass").Status; got != "skipped" {
		t.Fatalf("the merged pass's review = %s, want skipped", got)
	}
	closing := steps.startsOf("close")
	if len(closing) != 1 || closing[0].With["outcome"] != "approved" {
		t.Fatalf("close = %+v", closing)
	}
	// The steward's own pass and confirm are its body's: the review's are
	// not read there.
	if got := steps.startsOf("confirm"); len(got) != 2 || got[1].StepID != "steward/0/confirm" {
		t.Fatalf("confirm starts = %v", got)
	}
}

// max stops a repeat that never meets its until.
func TestMaxStopsARunawayRepeat(t *testing.T) {
	steps := &funcExecutor{typeName: "task.context", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{"state": "pushed"})
	}}
	svc, _ := controlService(t, steps)
	view, err := svc.Start(context.Background(), loopDefinition(t, `schema_version: 2
name: runaway
title: Runaway
steps:
  - id: spin
    loop: {repeat: true, max: 3, until: {field: loop.last.state, equals: approved}}
    outputs: {state: {type: string, value: "${{ steps.work.outputs.state }}"}}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}", index: "${{ loop.index }}"}, outputs: {state: {type: string}}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs := loopOutputs(t, view, "spin")
	if view.Run.Status != "succeeded" || outputs["ended_by"] != "max" || outputs["count"] != json.Number("3") || len(steps.starts) != 3 {
		t.Fatalf("run %s, outputs %v, starts %d", view.Run.Status, outputs, len(steps.starts))
	}
	if steps.starts[2].With["index"] != 2 {
		t.Fatalf("the third pass's loop.index = %v", steps.starts[2].With["index"])
	}
}

// LEG-480's example B, its ticket steps named by task.context and its
// agent by a step that waits on its service: the next: query is asked
// again before each pass (closing a ticket moves the frontier), each pass
// stacks on loop.previous, and a pass whose step fails pauses (the
// default) and goes on after Retry.
const exampleB = `schema_version: 2
name: build-milestone-by-ticket
title: Build a milestone, one agent per ticket
inputs:
  milestone: {type: string, required: true}
outputs:
  branches: {type: json, value: "${{ steps.tickets.outputs.results }}"}
steps:
  - id: tickets
    loop:
      next:
        type: task.note
        with: {task: "${{ workflow.task }}", note: "frontier of ${{ workflow.inputs.milestone }}"}
      key: "${{ loop.item.key }}"
      max: 30
    outputs:
      branch:   {type: string,  value: "${{ steps.build.outputs.branch }}"}
      clone_id: {type: integer, value: "${{ steps.build.outputs.clone_id }}"}
    steps:
      - id: card
        type: task.context
        with: {task: "${{ workflow.task }}", key: "${{ loop.item.key }}"}
        outputs: {task: {type: string}}
      - id: build
        type: github.pr.wait
        with:
          repo: o/r
          pr_number: 1
          on: "${{ steps.card.outputs.task }}"
          clone: "${{ loop.previous.clone_id ?? null }}"
          branch: "${{ loop.item.key }}"
        outputs: {branch: {type: string}, clone_id: {type: integer}}
  - id: push
    type: task.context
    with: {task: "${{ workflow.task }}", clone: "${{ steps.tickets.outputs.results[-1].clone_id }}"}
`

func TestExampleBTakesTheNextTicketAfterEachPassAndStacks(t *testing.T) {
	open := []string{"LEG-1", "LEG-2", "LEG-3"}
	frontier := func() []any {
		items := []any{}
		for _, key := range open {
			items = append(items, map[string]any{"key": key})
		}
		return items
	}
	query := &funcExecutor{typeName: "task.note", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{"items": frontier()})
	}}
	cards := &funcExecutor{typeName: "task.context", run: func(step ResolvedWorkflowStep, _ int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{"task": "card-" + fmt.Sprint(step.With["key"])})
	}}
	builds := &funcExecutor{typeName: "github.pr.wait", run: func(step ResolvedWorkflowStep, count int) (WorkflowExecutionResult, error) {
		if count == 1 {
			return WorkflowExecutionResult{}, errors.New("the agent could not start")
		}
		return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
	}}
	svc, _ := controlService(t, query, cards, builds)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, exampleB), map[string]string{"milestone": "LEG-0"})
	if err != nil {
		t.Fatal(err)
	}
	finish := func(pass int, clone int) {
		t.Helper()
		path := fmt.Sprintf("tickets/%d/build", pass)
		var attempt string
		for _, a := range view.Attempts {
			if a.Path == path && a.Status == WorkflowStepWaiting {
				attempt = a.ID
			}
		}
		if attempt == "" {
			t.Fatalf("no waiting attempt at %s (run at %s)", path, view.Run.CurrentStepID)
		}
		key := open[0]
		open = open[1:] // the agent closes its ticket
		var err error
		view, err = svc.CompleteWaiting(ctx, view.Run.ID, path, attempt, map[string]any{"branch": "legato/" + key, "clone_id": clone})
		if err != nil {
			t.Fatal(err)
		}
	}
	finish(0, 41)
	// Pass 1's build fails: the pass pauses there.
	paused := stepOf(t, view, "tickets/1/build")
	if view.Run.Status != "waiting" || paused.Status != WorkflowStepPaused {
		t.Fatalf("run %s, pass 1's build %s", view.Run.Status, paused.Status)
	}
	view, err = svc.StepAction(ctx, view.Run.ID, "tickets/1/build", PausedRetry, "")
	if err != nil {
		t.Fatal(err)
	}
	finish(1, 41)
	finish(2, 41)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %s at %s: %s", view.Run.Status, view.Run.CurrentStepID, view.Run.Error)
	}
	tickets := loopOutputs(t, view, "tickets")
	if tickets["count"] != json.Number("3") || tickets["ended_by"] != "next_empty" {
		t.Fatalf("tickets = %v", tickets)
	}
	starts := builds.starts
	if len(starts) != 4 {
		t.Fatalf("build starts = %d", len(starts))
	}
	if clone, given := starts[0].With["clone"]; !given || clone != nil {
		t.Fatalf("the first pass's clone = %v, want null (fresh)", starts[0].With["clone"])
	}
	for _, start := range starts[1:] {
		if cloneNumber(start.With["clone"]) != 41 {
			t.Fatalf("%s stacked on %v, want the previous pass's clone 41", start.StepID, start.With["clone"])
		}
	}
	if starts[3].With["branch"] != "LEG-3" || starts[3].With["on"] != "card-LEG-3" {
		t.Fatalf("pass 2's build = %v", starts[3].With)
	}
	// The query ran before each pass, and once more to find none left.
	if len(query.starts) != 4 || query.starts[0].StepID != "tickets/0/next" {
		t.Fatalf("query starts = %d (%s)", len(query.starts), query.starts[0].StepID)
	}
	push := cards.startsOf("push")
	if len(push) != 1 || cloneNumber(push[0].With["clone"]) != 41 {
		t.Fatalf("push = %+v", push)
	}
	if !strings.Contains(view.Run.OutputsJSON, "legato/LEG-3") {
		t.Fatalf("run outputs = %s", view.Run.OutputsJSON)
	}
}

// A restart mid-loop: the host that started the run is gone; a new one on
// the same database takes the pass's step completion and goes on with the
// loop, from the tables alone.
func TestALoopGoesOnAfterARestart(t *testing.T) {
	waits := &funcExecutor{typeName: "github.pr.wait", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
	}}
	svc, s := controlService(t, waits)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: restart
title: Restart
steps:
  - id: each
    loop: {over: [a, b], key: "${{ loop.item }}"}
    outputs: {item: {type: string, value: "${{ loop.item }}"}}
    steps:
      - {id: wait, type: github.pr.wait, with: {repo: o/r, pr_number: 1, item: "${{ loop.item }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewWorkflowService(s, []WorkflowExecutor{waits})
	for pass := range 2 {
		path := fmt.Sprintf("each/%d/wait", pass)
		var attempt string
		for _, a := range view.Attempts {
			if a.Path == path && a.Status == WorkflowStepWaiting {
				attempt = a.ID
			}
		}
		if attempt == "" {
			t.Fatalf("no waiting attempt at %s; run at %s", path, view.Run.CurrentStepID)
		}
		if view, err = restarted.CompleteWaiting(ctx, view.Run.ID, path, attempt, nil); err != nil {
			t.Fatal(err)
		}
	}
	outputs := loopOutputs(t, view, "each")
	if view.Run.Status != "succeeded" || outputs["ended_by"] != "items" || outputs["last"].(map[string]any)["item"] != "b" {
		t.Fatalf("run %s, outputs %v", view.Run.Status, outputs)
	}
}

// An agent's completion in a pass is recorded from its pane; a host
// started after advances the run, and the loop, from the tables.
func TestARecordedCompletionInAPassIsAdvancedByAHost(t *testing.T) {
	// An agent's step, without an agent: the step waits as one does.
	agents := &funcExecutor{typeName: "task.context", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return WorkflowExecutionResult{Status: "waiting_agent"}, nil
	}}
	svc, s := controlService(t, agents)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: recorded
title: Recorded
steps:
  - id: each
    loop: {repeat: true, max: 2}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}", prompt: "pass ${{ loop.index }}"}, outputs: {done: {type: string}}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	pane := NewWorkflowService(s, []WorkflowExecutor{agents})
	pane.SetRecordOnly()
	host := NewWorkflowService(s, []WorkflowExecutor{agents})
	for pass := range 2 {
		path := fmt.Sprintf("each/%d/work", pass)
		var attempt string
		for _, a := range view.Attempts {
			if a.Path == path && a.Status == "waiting_agent" {
				attempt = a.ID
			}
		}
		if attempt == "" {
			t.Fatalf("no waiting attempt at %s; run at %s", path, view.Run.CurrentStepID)
		}
		if _, err := pane.Complete(ctx, view.Run.ID, path, attempt, map[string]string{"done": "yes"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := host.AdvanceDue(ctx); err != nil {
			t.Fatal(err)
		}
		if view, err = s.GetWorkflowRun(ctx, view.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if view.Run.Status != "succeeded" || loopOutputs(t, view, "each")["ended_by"] != "max" {
		t.Fatalf("run %s at %s", view.Run.Status, view.Run.CurrentStepID)
	}
	if agents.starts[1].With["prompt"] != "pass 1" {
		t.Fatalf("the second pass's prompt = %v", agents.starts[1].With["prompt"])
	}
}

// LEG-480's example D, tasks.list named by task.context and deliver by
// task.note: an over: loop whose passes call the triage workflow, on
// failure going on to the next card.
const exampleD = `schema_version: 2
name: triage-backlog
title: Triage the cards waiting in Triage
steps:
  - id: find
    type: task.context
    with: {task: "${{ workflow.task }}"}
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

const triageChild = `schema_version: 2
name: triage
title: Triage a card
inputs:
  task: {type: task, required: true}
steps:
  - id: look
    type: task.context
    with: {task: "${{ workflow.inputs.task }}"}
`

func TestExampleDTriagesEachCardAndCountsFailures(t *testing.T) {
	ctx := context.Background()
	var ids []any
	contexts := &funcExecutor{typeName: "task.context", run: func(step ResolvedWorkflowStep, _ int) (WorkflowExecutionResult, error) {
		if step.StepID == "find" {
			return succeeded(map[string]any{"items": ids, "count": len(ids)})
		}
		task, _ := step.With["task"].(map[string]any)
		if strings.Contains(fmt.Sprint(task["title"]), "bad") {
			return WorkflowExecutionResult{}, errors.New("cannot triage")
		}
		return succeeded(map[string]any{"content": "ok"})
	}}
	notes := &funcExecutor{typeName: "task.note", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{"note_id": "n"})
	}}
	svc, s := controlService(t, contexts, notes)
	child := loopDefinition(t, triageChild)
	svc.SetDefinitions(func() workflowengine.DiscoveryResult {
		return workflowengine.DiscoveryResult{Definitions: []*workflowengine.Definition{child}}
	})
	for _, title := range []string{"one", "bad one", "two"} {
		id := store.GenerateTaskID()
		if err := s.CreateTask(ctx, store.Task{ID: id, Title: title, Status: "Backlog"}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, map[string]any{"id": id})
	}
	view, err := svc.Start(ctx, loopDefinition(t, exampleD), nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %s at %s: %s", view.Run.Status, view.Run.CurrentStepID, view.Run.Error)
	}
	each := loopOutputs(t, view, "each")
	if each["count"] != json.Number("3") || each["succeeded"] != json.Number("2") || each["failed"] != json.Number("1") || each["ended_by"] != "items" {
		t.Fatalf("each = %v", each)
	}
	failed := each["results"].([]any)[1].(map[string]any)
	if failed["status"] != "failed" || !strings.Contains(fmt.Sprint(failed["error"]), "cannot triage") {
		t.Fatalf("the failed pass = %v", failed)
	}
	if got := notes.starts; len(got) != 1 || got[0].With["note"] != "Triaged 2 cards, 1 failed" {
		t.Fatalf("tell = %+v", got)
	}
	// Each call is a child run linked to its pass's step.
	children, err := s.ListChildWorkflowRuns(ctx, view.Run.ID)
	if err != nil || len(children) != 3 || children[1].ParentStepPath != "each/1/triage" {
		t.Fatalf("children = %+v, %v", children, err)
	}
	// With no cards, the loop is skipped and so is the delivery.
	ids = nil
	view, err = svc.Start(ctx, loopDefinition(t, exampleD), nil)
	if err != nil || view.Run.Status != "succeeded" || stepOf(t, view, "each").Status != "skipped" || stepOf(t, view, "tell").Status != "skipped" {
		t.Fatalf("an empty backlog: %v, %+v", err, view.Steps)
	}
}

// LEG-480's example C, as written: each milestone's call stacks on the
// last's top branch, until a review is stuck.
const exampleC = `schema_version: 2
name: proposal-project
title: Build every ready milestone of a project
inputs:
  project: {type: string, required: true}
  base:    {type: string}
  builder: {type: string, default: build-milestone, choices: [build-milestone, build-milestone-by-ticket]}
outputs:
  milestones: {type: json, value: "${{ steps.milestones.outputs.results }}"}
steps:
  - id: milestones
    loop:
      next:
        type: task.note
        with: {task: "${{ workflow.task }}", note: "ready milestones of ${{ workflow.inputs.project }}"}
      key: "${{ loop.item.key }}"
      max: 10
      until: {field: loop.last.review, equals: stuck}
      on_failure: pause
    outputs:
      milestone:  {type: string, value: "${{ loop.item.key }}"}
      top_branch: {type: string, value: "${{ steps.build.outputs.top_branch }}"}
      review:     {type: string, value: "${{ steps.build.outputs.review }}"}
    steps:
      - id: build
        type: workflow.run
        with:
          workflow: "${{ workflow.inputs.builder }}"
          inputs:
            milestone: "${{ loop.item.key }}"
            base: "${{ loop.previous.top_branch ?? workflow.inputs.base }}"
  - id: tell
    type: task.context
    with:
      task: "${{ workflow.task }}"
      note: "${{ steps.milestones.outputs.count }} milestones built for ${{ workflow.inputs.project }}"
`

const buildMilestoneChild = `schema_version: 2
name: build-milestone
title: Build a milestone
inputs:
  milestone: {type: string, required: true}
  base: {type: string}
outputs:
  top_branch: {type: string, value: "legato/${{ workflow.inputs.milestone }}"}
  review: {type: string, value: "${{ steps.build.outputs.content }}"}
steps:
  - id: build
    type: task.context
    with: {task: "${{ workflow.task }}", milestone: "${{ workflow.inputs.milestone }}", base: "${{ workflow.inputs.base ?? 'main' }}"}
`

func TestExampleCStacksEachMilestoneOnTheLast(t *testing.T) {
	ctx := context.Background()
	milestones := []any{map[string]any{"key": "M1"}, map[string]any{"key": "M2"}, map[string]any{"key": "M3"}, map[string]any{"key": "M4"}}
	query := &funcExecutor{typeName: "task.note", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{"items": milestones})
	}}
	builds := &funcExecutor{typeName: "task.context", run: func(step ResolvedWorkflowStep, _ int) (WorkflowExecutionResult, error) {
		review := "approved"
		if step.With["milestone"] == "M3" {
			review = "stuck"
		}
		return succeeded(map[string]any{"content": review})
	}}
	svc, _ := controlService(t, query, builds)
	child := loopDefinition(t, buildMilestoneChild)
	svc.SetDefinitions(func() workflowengine.DiscoveryResult {
		return workflowengine.DiscoveryResult{Definitions: []*workflowengine.Definition{child}}
	})
	view, err := svc.Start(ctx, loopDefinition(t, exampleC), map[string]string{"project": "PROP", "base": "main"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %s at %s: %s", view.Run.Status, view.Run.CurrentStepID, view.Run.Error)
	}
	var bases []string
	for _, start := range builds.startsOf("build") {
		if start.StepID == "build" {
			bases = append(bases, fmt.Sprint(start.With["base"]))
		}
	}
	if strings.Join(bases, ",") != "main,legato/M1,legato/M2" {
		t.Fatalf("bases = %v", bases)
	}
	outputs := loopOutputs(t, view, "milestones")
	if outputs["ended_by"] != "until" || outputs["count"] != json.Number("3") || outputs["last"].(map[string]any)["milestone"] != "M3" {
		t.Fatalf("milestones = %v", outputs)
	}
	if tell := builds.startsOf("tell"); len(tell) != 1 || tell[0].With["note"] != "3 milestones built for PROP" {
		t.Fatalf("tell = %+v", tell)
	}
}

// A pass under on_failure: fail ends the loop failed: the loop step's own
// on_failure then decides, here continue, which keeps its outputs.
func TestAFailedPassUnderFailFailsTheLoop(t *testing.T) {
	steps := &funcExecutor{typeName: "task.context", run: func(step ResolvedWorkflowStep, count int) (WorkflowExecutionResult, error) {
		if count == 1 {
			return WorkflowExecutionResult{}, errors.New("broke")
		}
		return succeeded(map[string]any{})
	}}
	svc, _ := controlService(t, steps)
	view, err := svc.Start(context.Background(), loopDefinition(t, `schema_version: 2
name: failing
title: Failing
steps:
  - id: each
    on_failure: continue
    loop: {repeat: true, max: 5, on_failure: fail}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}"}}
  - id: after
    type: task.context
    with: {task: "${{ workflow.task }}", count: "${{ steps.each.outputs.count }}", ended: "${{ steps.each.outputs.ended_by }}", error: "${{ steps.each.outputs.error }}"}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	after := steps.startsOf("after")
	if view.Run.Status != "succeeded" || stepOf(t, view, "each").Status != "failed" || len(after) != 1 {
		t.Fatalf("run %s, each %s, after %d", view.Run.Status, stepOf(t, view, "each").Status, len(after))
	}
	if fmt.Sprint(after[0].With["count"]) != "2" || after[0].With["ended"] != "failure" || !strings.Contains(fmt.Sprint(after[0].With["error"]), "broke") {
		t.Fatalf("after = %v", after[0].With)
	}
}

// A host that died after a pass's last step was written, before its loop
// moved on, leaves the run at the loop: the reconcile moves it on.
func TestTheReconcileMovesOnARunLeftAtItsLoop(t *testing.T) {
	waits := &funcExecutor{typeName: "github.pr.wait", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
	}}
	svc, s := controlService(t, waits)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: crash
title: Crash
steps:
  - id: each
    loop: {repeat: true, max: 2}
    steps:
      - {id: wait, type: github.pr.wait, with: {repo: o/r, pr_number: 1}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[len(view.Attempts)-1]
	if attempt.Path != "each/0/wait" {
		t.Fatalf("attempt at %s", attempt.Path)
	}
	// The step is written as done and the run is at its loop; nothing
	// moved it on.
	if err := s.CompleteWorkflowStep(ctx, view.Run.ID, "each/0/wait", attempt.ID, "{}", "each"); err != nil {
		t.Fatal(err)
	}
	svc.ReconcileLoops(ctx)
	view, err = s.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.CurrentStepID != "each/1/wait" || stepOf(t, view, "each/1/wait").Status != WorkflowStepWaiting {
		t.Fatalf("run at %s", view.Run.CurrentStepID)
	}
}

// A run whose loop failed needs the operator at the loop, which Retry goes
// on from; its pass's failed step is not a second item.
func TestAFailedLoopIsOneAttentionItem(t *testing.T) {
	steps := &funcExecutor{typeName: "task.context", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return WorkflowExecutionResult{}, errors.New("broke")
	}}
	svc, _ := controlService(t, steps)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: failing
title: Failing
steps:
  - id: each
    loop: {repeat: true, on_failure: fail}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || view.Run.CurrentStepID != "each" {
		t.Fatalf("run %s at %s", view.Run.Status, view.Run.CurrentStepID)
	}
	items, err := NewWorkflowAttentionAdapter(svc).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SourceKey != view.Run.ID+":each" {
		t.Fatalf("items = %+v", items)
	}
	// Retry on the pass's step is refused: the loop is what is retried.
	if _, err := svc.Retry(ctx, view.Run.ID, "each/0/work", false); !errors.Is(err, store.ErrWorkflowStateConflict) {
		t.Fatalf("Retry of the pass's step = %v", err)
	}
}

// An item the loop cannot key fails the loop, rather than leave the run
// at it.
func TestAnItemWithNoKeyFailsTheLoop(t *testing.T) {
	steps := &funcExecutor{typeName: "task.context", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return succeeded(map[string]any{})
	}}
	svc, _ := controlService(t, steps)
	view, err := svc.Start(context.Background(), loopDefinition(t, `schema_version: 2
name: keyless
title: Keyless
steps:
  - id: each
    loop: {over: [{key: a}, {title: no key}], key: "${{ loop.item.key }}"}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || stepOf(t, view, "each").Status != "failed" || !strings.Contains(stepOf(t, view, "each").Error, "key") {
		t.Fatalf("run %s, each %s: %s", view.Run.Status, stepOf(t, view, "each").Status, stepOf(t, view, "each").Error)
	}
}

// A host that died after starting a loop's step, before writing its
// state, leaves the loop running with no state: the reconcile starts it
// (an over: loop reads its list then), rather than end it with no passes.
func TestTheReconcileStartsALoopLeftWithNoState(t *testing.T) {
	waits := &funcExecutor{typeName: "github.pr.wait", run: func(ResolvedWorkflowStep, int) (WorkflowExecutionResult, error) {
		return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
	}}
	svc, s := controlService(t, waits)
	ctx := context.Background()
	view, err := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: crash
title: Crash
steps:
  - id: first
    type: github.pr.wait
    with: {repo: o/r, pr_number: 1}
  - id: each
    loop: {over: [a, b], key: "${{ loop.item }}"}
    steps:
      - {id: wait, type: github.pr.wait, with: {repo: o/r, pr_number: 1, item: "${{ loop.item }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteWorkflowStep(ctx, view.Run.ID, "first", view.Attempts[0].ID, "{}", "each"); err != nil {
		t.Fatal(err)
	}
	// The loop's step is started, and nothing more.
	attempt := store.WorkflowStepAttempt{ID: "wa-loop", RunID: view.Run.ID, Path: "each", StepID: "each", AttemptNumber: 1, ExecutorType: workflowengine.LoopStepType, TaskID: view.Run.TaskID, Status: "running"}
	if err := s.StartWorkflowStepAttempt(ctx, attempt, "{}"); err != nil {
		t.Fatal(err)
	}
	NewWorkflowService(s, []WorkflowExecutor{waits}).ReconcileLoops(ctx)
	if view, err = s.GetWorkflowRun(ctx, view.Run.ID); err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" || view.Run.CurrentStepID != "each/0/wait" {
		t.Fatalf("run %s at %s; each = %s", view.Run.Status, view.Run.CurrentStepID, stepOf(t, view, "each").OutputsJSON)
	}
	if starts := waits.startsOf("wait"); len(starts) != 1 || starts[0].With["item"] != "a" {
		t.Fatalf("pass starts = %+v", starts)
	}
}

// A loop's failure writes its state with the step's failure, or neither:
// a host that dies between them leaves a loop the reconcile fails, not a
// loop whose failed pass is closed and that goes on to the next.
func TestALoopsFailureAndItsStateAreWrittenTogether(t *testing.T) {
	steps := &funcExecutor{typeName: "task.context", run: func(_ ResolvedWorkflowStep, count int) (WorkflowExecutionResult, error) {
		if count == 0 {
			return WorkflowExecutionResult{}, errors.New("broke")
		}
		return succeeded(map[string]any{})
	}}
	svc, s := controlService(t, steps)
	ctx := context.Background()
	crash := `CREATE TRIGGER crash_loop_fail BEFORE UPDATE OF status ON workflow_step_attempts
		WHEN NEW.executor_type = 'loop' AND NEW.status = 'failed' BEGIN SELECT RAISE(ABORT, 'crash between writes'); END`
	if _, err := s.DB().Exec(crash); err != nil {
		t.Fatal(err)
	}
	view, _ := svc.Start(ctx, loopDefinition(t, `schema_version: 2
name: failing
title: Failing
steps:
  - id: each
    loop: {repeat: true, max: 3, on_failure: fail}
    steps:
      - {id: work, type: task.context, with: {task: "${{ workflow.task }}"}}
`), nil)
	if view == nil {
		runs, err := s.ListWorkflowRuns(ctx, store.WorkflowRunFilter{})
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs = %+v, %v", runs, err)
		}
		view = &store.WorkflowRunView{Run: runs[0]}
	}
	if _, err := s.DB().Exec(`DROP TRIGGER crash_loop_fail`); err != nil {
		t.Fatal(err)
	}
	NewWorkflowService(s, []WorkflowExecutor{steps}).ReconcileLoops(ctx)
	view, err := s.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if each := stepOf(t, view, "each"); view.Run.Status != "failed" || each.Status != "failed" || len(steps.startsOf("work")) != 1 {
		t.Fatalf("run %s at %s, each %s, work started %d times", view.Run.Status, view.Run.CurrentStepID, each.Status, len(steps.startsOf("work")))
	}
}
