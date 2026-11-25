package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// LEG-480's example A, build-milestone, as far as the steps it needs are
// built: ticket.import (LEG-488) stands in as task.context, and the
// outputs Legato fills at the agent's signal (LEG-485) as the outputs its
// steps declare. A caller reads its declared top_branch.
const buildMilestone = `schema_version: 2
name: build-milestone
title: Build a milestone as one stack
run_title: Build ${{ workflow.inputs.milestone }}
inputs:
  milestone: {type: string, required: true, description: A Burnlite milestone key}
  repo:      {type: repository, required: true}
  base:      {type: string, description: The branch the stack starts on}
outputs:
  top_branch: {type: string, value: "${{ steps.push.outputs.top_branch }}"}
  prs:        {type: json,   value: "${{ steps.push.outputs.prs }}"}
  review:     {type: string, value: "${{ steps.review.outputs.outcome ?? 'not reviewed' }}"}
steps:
  - id: card
    title: Import the milestone
    type: task.context
    with: {task: "${{ workflow.task }}"}
  - id: build
    title: Build it with proposal-stack
    type: agent.run
    with:
      prompt: "Build ${{ workflow.inputs.milestone }} on ${{ workflow.inputs.base ?? workflow.inputs.repo.base_branch }} in ${{ workflow.inputs.repo.repo }}"
    outputs: {branch: {type: string}}
  - id: push
    title: Push the stack and open its PRs
    type: agent.run
    with: {prompt: Push every branch of the stack and open its pull requests, bottom first.}
    outputs: {top_branch: {type: string}, prs: {type: json}}
  - id: review
    title: Review this milestone's PRs
    type: github.pr.review_passes
    if: {field: steps.push.outputs.prs, empty: false}
    with: {repo: "${{ workflow.inputs.repo.repo }}", pr_number: 1, agent_author: true}
  - id: stuck
    title: Ask the operator about a stuck review
    type: gate
    if: {field: steps.review.outputs.outcome, equals: stuck}
    with: {title: "The review of ${{ workflow.inputs.milestone }} is stuck", options: [Continue, Stop]}
`

const shipMilestone = `schema_version: 2
name: ship-milestone
title: Ship a milestone
inputs:
  milestone: {type: string, required: true}
  repo: {type: repository, required: true}
outputs:
  top: {type: string, value: "${{ steps.build.outputs.top_branch }}"}
steps:
  - id: build
    type: workflow.run
    with:
      workflow: build-milestone
      inputs: {milestone: "${{ workflow.inputs.milestone }}", repo: "${{ workflow.inputs.repo }}"}
  - id: tell
    type: task.note
    with: {task: "${{ workflow.task }}", note: "Built ${{ steps.build.outputs.top_branch }} (${{ steps.build.outputs.status }}, ${{ steps.build.outputs.review }})"}
`

// byStepExecutor succeeds with the outputs given for each step's ID, and
// fails a step's first starts with the errors given for it.
type byStepExecutor struct {
	typeName string
	outputs  map[string]map[string]any
	failures map[string][]error
	starts   []ResolvedWorkflowStep
}

func (e *byStepExecutor) Type() string { return e.typeName }
func (e *byStepExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts = append(e.starts, step)
	_, id := workflowengine.SplitStepPath(step.StepID)
	if failures := e.failures[id]; len(failures) > 0 {
		e.failures[id] = failures[1:]
		return WorkflowExecutionResult{}, failures[0]
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: e.outputs[id]}, nil
}
func (e *byStepExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

type callFixture struct {
	svc    *WorkflowService
	store  *store.Store
	agents *byStepExecutor
	notes  *byStepExecutor
}

func newCallFixture(t *testing.T, files ...string) callFixture {
	t.Helper()
	agents := &byStepExecutor{typeName: "agent.run", failures: map[string][]error{}, outputs: map[string]map[string]any{
		"build": {"branch": "legato/m-1"},
		"push":  {"top_branch": "legato/m-1-top", "prs": []any{}},
	}}
	notes := &byStepExecutor{typeName: "task.note", failures: map[string][]error{}, outputs: map[string]map[string]any{}}
	context := &byStepExecutor{typeName: "task.context", failures: map[string][]error{}, outputs: map[string]map[string]any{"card": {"content": "M-1"}}}
	review := &byStepExecutor{typeName: "github.pr.review_passes", failures: map[string][]error{}, outputs: map[string]map[string]any{}}
	svc, s := controlService(t, agents, notes, context, review)
	definitions := workflowengine.DiscoveryResult{}
	for _, file := range append([]string{buildMilestone}, files...) {
		definitions.Definitions = append(definitions.Definitions, parseControlWorkflow(t, file))
	}
	svc.SetDefinitions(func() workflowengine.DiscoveryResult { return definitions })
	return callFixture{svc: svc, store: s, agents: agents, notes: notes}
}

func (f callFixture) definition(t *testing.T, name string) *workflowengine.Definition {
	t.Helper()
	for _, definition := range f.svc.definitions().Definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("no workflow %s", name)
	return nil
}

func (f callFixture) children(t *testing.T, runID string) []store.WorkflowRun {
	t.Helper()
	children, err := f.store.ListChildWorkflowRuns(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return children
}

func (f callFixture) run(t *testing.T, runID string) *store.WorkflowRunView {
	t.Helper()
	view, err := f.svc.Show(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

const customRepo = `{"repo":"o/custom","primary_dir":"/src/custom","base_branch":"trunk"}`

// A parent calls example A and reads its declared top_branch.
func TestAParentCallsBuildMilestoneAndReadsItsTopBranch(t *testing.T) {
	f := newCallFixture(t, shipMilestone)
	ctx := context.Background()

	view, err := f.svc.Start(ctx, f.definition(t, "ship-milestone"), map[string]string{"milestone": "M-1", "repo": customRepo})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("parent = %s (%s), steps %+v", view.Run.Status, view.Run.Error, view.Steps)
	}
	children := f.children(t, view.Run.ID)
	if len(children) != 1 {
		t.Fatalf("children = %+v", children)
	}
	child := children[0]
	if child.WorkflowName != "build-milestone" || child.Status != "succeeded" || child.ParentStepPath != "build" || child.ParentAttempt != 1 || child.WorkflowTitle != "Build M-1" {
		t.Fatalf("child = %+v", child)
	}
	// The repository went through as the object it resolved to: a custom
	// repository survives the call.
	if prompt := f.agents.starts[0].With["prompt"]; prompt != "Build M-1 on trunk in o/custom" {
		t.Fatalf("build prompt = %q", prompt)
	}
	if got := decodeObject(t, child.OutputsJSON); got["top_branch"] != "legato/m-1-top" || got["review"] != "not reviewed" {
		t.Fatalf("child outputs = %v", got)
	}
	build := stepOf(t, view, "build")
	if build.ChildRunID != child.ID {
		t.Fatalf("build step's child = %q", build.ChildRunID)
	}
	if got := outputsOf(t, build); got["top_branch"] != "legato/m-1-top" || got["status"] != "succeeded" || got["run_id"] != child.ID {
		t.Fatalf("build outputs = %v", got)
	}
	if note := f.notes.starts[0].With["note"]; note != "Built legato/m-1-top (succeeded, not reviewed)" {
		t.Fatalf("note = %q", note)
	}
	if got := decodeObject(t, view.Run.OutputsJSON); got["top"] != "legato/m-1-top" {
		t.Fatalf("parent outputs = %v", got)
	}
}

func decodeObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return out
}

// Outputs that can't be resolved are left out and named as warnings.
func TestAnUnresolvedOutputIsAWarningOnTheRun(t *testing.T) {
	f := newCallFixture(t)
	f.agents.outputs["push"] = map[string]any{"prs": []any{}}
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.definition(t, "build-milestone"), map[string]string{"milestone": "M-1", "repo": customRepo})
	if err != nil {
		t.Fatal(err)
	}
	shown := f.run(t, view.Run.ID)
	if shown.Run.Status != "succeeded" || len(shown.OutputWarnings) != 1 || !strings.Contains(shown.OutputWarnings[0], `"top_branch"`) {
		t.Fatalf("run %s, warnings %q", shown.Run.Status, shown.OutputWarnings)
	}
	if got := decodeObject(t, shown.Run.OutputsJSON); got["review"] != "not reviewed" || got["top_branch"] != nil {
		t.Fatalf("outputs = %v", got)
	}
}

const continueCaller = `schema_version: 2
name: caller
title: Caller
steps:
  - id: build
    type: workflow.run
    on_failure: continue
    with: {workflow: build-milestone, inputs: {milestone: M-1, repo: "` + `o-custom` + `"}}
  - id: busy
    if: {field: steps.build.outputs.error_code, equals: review_loop_active}
    type: task.note
    with: {task: "${{ workflow.task }}", note: "${{ steps.build.outputs.status }}: ${{ steps.build.outputs.error }} in ${{ steps.build.outputs.run_id }}"}
`

// A child's failure follows the parent's on_failure, and its error_code
// reaches the parent step's outputs.
func TestAFailedChildsErrorCodeReachesTheParentStep(t *testing.T) {
	f := newCallFixture(t, continueCaller)
	f.svc.SetRepositoryPresets([]workflowengine.RepositoryPreset{{Name: "o-custom", Repo: "o/custom", PrimaryDir: "/src", BaseBranch: "trunk"}})
	f.agents.failures["push"] = []error{&WorkflowStepError{Code: "review_loop_active", Link: "/workflows/runs/wr-other", Message: "another loop holds the pull request"}}
	ctx := context.Background()

	view, err := f.svc.Start(ctx, f.definition(t, "caller"), nil)
	if err != nil {
		t.Fatal(err)
	}
	child := f.children(t, view.Run.ID)[0]
	if child.Status != "failed" {
		t.Fatalf("child = %s", child.Status)
	}
	build := stepOf(t, view, "build")
	got := outputsOf(t, build)
	if build.Status != "failed" || got["status"] != "failed" || got["error_code"] != "review_loop_active" || got["run_id"] != child.ID || got["error_link"] != "/workflows/runs/wr-other" {
		t.Fatalf("build = %s, outputs %v", build.Status, got)
	}
	if view.Run.Status != "succeeded" || len(f.notes.starts) != 1 {
		t.Fatalf("parent = %s, notes %+v", view.Run.Status, f.notes.starts)
	}
	if note, _ := f.notes.starts[0].With["note"].(string); !strings.HasPrefix(note, "failed: ") || !strings.Contains(note, "another loop holds the pull request") || !strings.HasSuffix(note, child.ID) {
		t.Fatalf("note = %q", note)
	}
}

// With on_failure: fail the parent fails; Retry resumes the same child,
// whose own failed step runs again.
func TestRetryOfAFailedCallResumesTheSameChild(t *testing.T) {
	f := newCallFixture(t, shipMilestone)
	f.agents.failures["push"] = []error{&WorkflowStepError{Code: "push_refused", Message: "the push was refused"}}
	ctx := context.Background()

	view, err := f.svc.Start(ctx, f.definition(t, "ship-milestone"), map[string]string{"milestone": "M-1", "repo": customRepo})
	if err != nil {
		t.Fatal(err)
	}
	build := stepOf(t, view, "build")
	if view.Run.Status != "failed" || build.Status != "failed" || build.ErrorCode != "push_refused" {
		t.Fatalf("parent = %s, build %s %q", view.Run.Status, build.Status, build.ErrorCode)
	}
	child := f.children(t, view.Run.ID)[0]
	builds := len(f.agents.starts)

	view, err = f.svc.Retry(ctx, view.Run.ID, "build", false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("parent after Retry = %s (%s)", view.Run.Status, view.Run.Error)
	}
	children := f.children(t, view.Run.ID)
	if len(children) != 1 || children[0].ID != child.ID || children[0].Status != "succeeded" {
		t.Fatalf("children after Retry = %+v", children)
	}
	// Only the failed push ran again, not the child's earlier steps.
	if again := f.agents.starts[builds:]; len(again) != 1 || !strings.HasSuffix(again[0].StepID, "push") {
		t.Fatalf("starts after Retry = %+v", again)
	}
}

const gatedChild = `schema_version: 2
name: gated
title: Gated
outputs:
  choice: {type: string, value: "${{ steps.ask.outputs.choice }}"}
steps:
  - id: ask
    type: gate
    with: {title: "Go on?"}
`

const gatedCaller = `schema_version: 2
name: gated-caller
title: Gated caller
steps:
  - id: call
    type: workflow.run
    on_failure: pause
    with: {workflow: gated}
`

// A gate in the child keeps the parent waiting, until the operator
// chooses.
func TestAGateInTheChildKeepsTheParentWaiting(t *testing.T) {
	f := newCallFixture(t, gatedChild, gatedCaller)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" || stepOf(t, view, "call").Status != WorkflowStepWaiting {
		t.Fatalf("parent = %s, call %s", view.Run.Status, stepOf(t, view, "call").Status)
	}
	child := f.children(t, view.Run.ID)[0]
	if _, err := f.svc.StepAction(ctx, child.ID, "ask", GateChoiceAction("Continue"), ""); err != nil {
		t.Fatal(err)
	}
	view = f.run(t, view.Run.ID)
	if view.Run.Status != "succeeded" || outputsOf(t, stepOf(t, view, "call"))["choice"] != "Continue" {
		t.Fatalf("parent = %s, call outputs %v", view.Run.Status, outputsOf(t, stepOf(t, view, "call")))
	}
}

// Cancelling a parent cancels its child, and never runs the parent's own
// on_failure: the child's cancellation is not the child failing.
func TestCancellingAParentCancelsItsChildWithoutItsOnFailure(t *testing.T) {
	// Under continue, a child's failure would run the next step.
	caller := strings.Replace(gatedCaller, "on_failure: pause", "on_failure: continue", 1) + `  - id: after
    type: task.note
    with: {task: "${{ workflow.task }}", note: "after ${{ steps.call.outputs.status }}"}
`
	f := newCallFixture(t, gatedChild, caller)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
	if err != nil {
		t.Fatal(err)
	}
	child := f.children(t, view.Run.ID)[0]
	view, err = f.svc.Cancel(ctx, view.Run.ID, "not now")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "cancelled" || stepOf(t, view, "call").Status != "cancelled" {
		t.Fatalf("parent = %s, call %s", view.Run.Status, stepOf(t, view, "call").Status)
	}
	if got := f.run(t, child.ID); got.Run.Status != "cancelled" {
		t.Fatalf("child = %s", got.Run.Status)
	}
	if len(f.notes.starts) != 0 || stepOf(t, view, "after").Status != "cancelled" {
		t.Fatalf("the parent's on_failure ran: notes %+v, after %s", f.notes.starts, stepOf(t, view, "after").Status)
	}
	f.svc.SweepEndedRuns(ctx)
	f.svc.ReconcileChildRuns(ctx, 0)
	if view = f.run(t, view.Run.ID); view.Run.Status != "cancelled" || stepOf(t, view, "call").Status != "cancelled" {
		t.Fatalf("after the sweep, parent = %s, call %s", view.Run.Status, stepOf(t, view, "call").Status)
	}
}

// A workflow.run step its timeout ends fails with error_code timeout, and
// its child is cancelled after that failure is written, so the child's end
// settles nothing: the step's on_failure has already decided.
func TestATimedOutCallCancelsItsChild(t *testing.T) {
	caller := strings.Replace(gatedCaller, "on_failure: pause", "on_failure: continue\n    timeout: 1h", 1)
	f := newCallFixture(t, gatedChild, caller)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
	if err != nil {
		t.Fatal(err)
	}
	child := f.children(t, view.Run.ID)[0]
	if n, err := f.svc.SweepTimeouts(ctx, time.Now().Add(2*time.Hour)); err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	if got := f.run(t, child.ID); got.Run.Status != "cancelled" {
		t.Fatalf("child = %s", got.Run.Status)
	}
	view = f.run(t, view.Run.ID)
	call := stepOf(t, view, "call")
	if call.Status != "failed" || outputsOf(t, call)["error_code"] != WorkflowErrorTimeout {
		t.Fatalf("call = %s, outputs %v", call.Status, outputsOf(t, call))
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("parent = %s; on_failure: continue should have gone on", view.Run.Status)
	}
}

// A child cancelled on its own fails the step, as one that fails does; a
// child stopped at its gate does too, with status stopped.
func TestAChildCancelledOrStoppedOnItsOwnFailsTheStep(t *testing.T) {
	for _, end := range []string{"cancelled", "stopped"} {
		t.Run(end, func(t *testing.T) {
			f := newCallFixture(t, gatedChild, strings.Replace(gatedCaller, "on_failure: pause", "on_failure: continue", 1))
			ctx := context.Background()
			view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
			if err != nil {
				t.Fatal(err)
			}
			child := f.children(t, view.Run.ID)[0]
			if end == "cancelled" {
				_, err = f.svc.Cancel(ctx, child.ID, "by hand")
			} else {
				_, err = f.svc.StepAction(ctx, child.ID, "ask", GateChoiceAction("Stop"), "")
			}
			if err != nil {
				t.Fatal(err)
			}
			view = f.run(t, view.Run.ID)
			call := stepOf(t, view, "call")
			if got := outputsOf(t, call); call.Status != "failed" || got["status"] != end || got["run_id"] != child.ID {
				t.Fatalf("call = %s, outputs %v", call.Status, got)
			}
			if view.Run.Status != "succeeded" {
				t.Fatalf("parent = %s", view.Run.Status)
			}
		})
	}
}

// Recursion is also checked at run time: a workflow that calls itself
// fails the step, as do calls nested deeper than five, and the code
// travels up through every caller.
func TestRecursionAndDepthAreCheckedAtRunTime(t *testing.T) {
	self := "schema_version: 2\nname: self\ntitle: Self\nsteps:\n  - id: again\n    type: workflow.run\n    with: {workflow: self}\n"
	f := newCallFixture(t, self)
	view, err := f.svc.Start(context.Background(), f.definition(t, "self"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepOf(t, view, "again"); view.Run.Status != "failed" || got.ErrorCode != "workflow_recursion" {
		t.Fatalf("self = %s, step %+v", view.Run.Status, got)
	}

	var files []string
	for i := 0; i < 6; i++ {
		files = append(files, "schema_version: 2\nname: w"+string(rune('0'+i))+"\ntitle: W\nsteps:\n  - id: call\n    type: workflow.run\n    with: {workflow: w"+string(rune('1'+i))+"}\n")
	}
	files = append(files, "schema_version: 2\nname: w6\ntitle: W\nsteps:\n  - id: note\n    type: task.note\n    with: {task: \"${{ workflow.task }}\", note: deep}\n")
	f = newCallFixture(t, files...)
	view, err = f.svc.Start(context.Background(), f.definition(t, "w0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepOf(t, view, "call"); view.Run.Status != "failed" || got.ErrorCode != "workflow_too_deep" {
		t.Fatalf("w0 = %s, step %+v", view.Run.Status, got)
	}
	if len(f.notes.starts) != 0 {
		t.Fatal("w6 ran")
	}
	depth, id := 0, view.Run.ID
	for {
		children := f.children(t, id)
		if len(children) == 0 {
			break
		}
		depth, id = depth+1, children[0].ID
	}
	if depth != 5 {
		t.Fatalf("calls nested %d deep, want 5", depth)
	}
}

// A crash between creating the child and marking the parent's step
// waiting doesn't start the child twice: the reconcile finds it, marks the
// step waiting and starts it once.
func TestACrashBetweenCreatingTheChildAndWaitingStartsItOnce(t *testing.T) {
	f := newCallFixture(t, shipMilestone)
	ctx := context.Background()
	definition := f.definition(t, "ship-milestone")

	// A run that reached its workflow.run step, whose attempt created the
	// child, then the process died.
	taskID := store.GenerateTaskID()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := f.store.CreateTask(ctx, store.Task{ID: taskID, Title: "Ship", Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	definitionJSON, _ := json.Marshal(definition)
	inputs := map[string]any{"milestone": "M-1", "repo": decodeObject(t, customRepo)}
	inputsJSON, _ := json.Marshal(inputs)
	run := store.WorkflowRun{ID: "wr-crash", WorkflowName: definition.Name, WorkflowTitle: "Ship", DefinitionJSON: string(definitionJSON), InputsJSON: string(inputsJSON), Status: "running", CurrentStepID: "build", TaskID: taskID, SchemaVersion: 2}
	if err := f.store.CreateWorkflowRun(ctx, run, stepInstances(run.ID, "", nil, definition.Steps)); err != nil {
		t.Fatal(err)
	}
	with := map[string]any{"workflow": "build-milestone", "inputs": inputs}
	withJSON, _ := json.Marshal(with)
	attempt := store.WorkflowStepAttempt{ID: "wa-crash", RunID: run.ID, Path: "build", StepID: "build", AttemptNumber: 1, ExecutorType: "workflow.run", TaskID: taskID, Status: "running"}
	if err := f.store.StartWorkflowStepAttempt(ctx, attempt, string(withJSON)); err != nil {
		t.Fatal(err)
	}
	step := ResolvedWorkflowStep{RunID: run.ID, AttemptID: attempt.ID, TaskID: taskID, StepID: "build", Type: "workflow.run", With: with}
	if _, err := f.svc.Executor("workflow.run").Start(ctx, step); err != nil {
		t.Fatal(err)
	}

	f.svc.ReconcileChildRuns(ctx, 0)
	f.svc.ReconcileChildRuns(ctx, 0)
	f.svc.SweepEndedRuns(ctx)

	if children := f.children(t, run.ID); len(children) != 1 || children[0].Status != "succeeded" {
		t.Fatalf("children = %+v", children)
	}
	if len(f.agents.starts) != 2 {
		t.Fatalf("the child's agent steps started %d times, want 2 (build, push)", len(f.agents.starts))
	}
	if view := f.run(t, run.ID); view.Run.Status != "succeeded" || outputsOf(t, stepOf(t, view, "build"))["top_branch"] != "legato/m-1-top" {
		t.Fatalf("parent = %s", view.Run.Status)
	}
}

// A child that ended while no host ran is settled by the sweep: its
// parent's step is one the run-ended hook holds.
func TestAChildThatEndedWhileLegatoWasDownIsSettledByTheSweep(t *testing.T) {
	f := newCallFixture(t, gatedChild, gatedCaller)
	ctx := context.Background()
	view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
	if err != nil {
		t.Fatal(err)
	}
	child := f.children(t, view.Run.ID)[0]
	// Another process, with no listeners, completes the child's gate.
	other := NewWorkflowService(f.store, nil)
	other.SetRecordOnly()
	shown, err := other.Show(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.CompleteWaiting(ctx, child.ID, "ask", shown.Attempts[0].ID, map[string]any{"choice": "Continue"}); err != nil {
		t.Fatal(err)
	}
	if view = f.run(t, view.Run.ID); view.Run.Status != "running" {
		t.Fatalf("parent = %s before the sweep", view.Run.Status)
	}
	f.svc.SweepEndedRuns(ctx)
	if view = f.run(t, view.Run.ID); view.Run.Status != "succeeded" {
		t.Fatalf("parent = %s after the sweep", view.Run.Status)
	}
}

const signalChild = `schema_version: 2
name: signal-child
title: Signal child
inputs:
  task: {type: task, required: true}
outputs:
  kind:   {type: string, value: done}
  signal: {type: string, value: "${{ steps.work.outputs.signal }}"}
steps:
  - id: work
    type: agent.run
    with: {on: "${{ workflow.inputs.task }}", prompt: Do it}
    outputs: {note: {type: string}}
`

const signalCaller = `schema_version: 2
name: signal-caller
title: Signal caller
inputs:
  task: {type: task, required: true}
steps:
  - id: call
    type: workflow.run
    with: {workflow: signal-child, inputs: {task: "${{ workflow.inputs.task }}"}}
`

// A run that ends at an agent's completion keeps the outputs Legato fills
// at the signal, and its parent's step gets them: neither reads the step
// before the host has filled it.
func TestARunEndedByAnAgentsCompletionKeepsTheOutputsLegatoFills(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	child, caller := parseControlWorkflow(t, signalChild), parseControlWorkflow(t, signalCaller)
	f.workflows.SetDefinitions(func() workflowengine.DiscoveryResult {
		return workflowengine.DiscoveryResult{Definitions: []*workflowengine.Definition{child, caller}}
	})
	parent, err := f.workflows.Start(ctx, caller, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	children, err := f.store.ListChildWorkflowRuns(ctx, parent.Run.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("children = %+v, %v", children, err)
	}
	view, err := f.store.GetWorkflowRun(ctx, children[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.workflows.Complete(ctx, view.Run.ID, "work", view.Attempts[0].ID, map[string]string{"note": "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	ended, err := f.store.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeObject(t, ended.Run.OutputsJSON); ended.Run.Status != "succeeded" || got["kind"] != "done" || got["signal"] != "complete" {
		t.Fatalf("child = %s, kept outputs %v", ended.Run.Status, got)
	}
	settled, err := f.store.GetWorkflowRun(ctx, parent.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := outputsOf(t, stepOf(t, settled, "call")); settled.Run.Status != "succeeded" || got["kind"] != "done" || got["signal"] != "complete" {
		t.Fatalf("parent = %s, call outputs %v", settled.Run.Status, got)
	}
}

// A parent whose end was written but whose children were never cancelled
// (the follow-up failed, or Legato stopped between the two) has them
// cancelled by the reconcile, and its own on_failure still never runs.
func TestTheReconcileCancelsTheChildrenOfAnEndedParent(t *testing.T) {
	caller := strings.Replace(gatedCaller, "on_failure: pause", "on_failure: continue", 1) + `  - id: after
    type: task.note
    with: {task: "${{ workflow.task }}", note: "after ${{ steps.call.outputs.status }}"}
`
	for _, end := range []string{"cancelled", WorkflowRunStopped} {
		t.Run(end, func(t *testing.T) {
			f := newCallFixture(t, gatedChild, caller)
			ctx := context.Background()
			view, err := f.svc.Start(ctx, f.definition(t, "gated-caller"), nil)
			if err != nil {
				t.Fatal(err)
			}
			child := f.children(t, view.Run.ID)[0]
			write := f.store.CancelWorkflowRun
			if end == WorkflowRunStopped {
				write = f.store.StopWorkflowRun
			}
			if err := write(ctx, view.Run.ID, "not now"); err != nil {
				t.Fatal(err)
			}
			if got := f.run(t, child.ID); got.Run.Status != "running" {
				t.Fatalf("child before the reconcile = %s", got.Run.Status)
			}
			f.svc.SweepEndedRuns(ctx)
			f.svc.ReconcileChildRuns(ctx, 0)
			if got := f.run(t, child.ID); got.Run.Status != "cancelled" {
				t.Fatalf("child = %s", got.Run.Status)
			}
			view = f.run(t, view.Run.ID)
			if view.Run.Status != end || stepOf(t, view, "call").Status != "cancelled" || stepOf(t, view, "after").Status != "cancelled" || len(f.notes.starts) != 0 {
				t.Fatalf("parent = %s, call %s, after %s, notes %+v", view.Run.Status, stepOf(t, view, "call").Status, stepOf(t, view, "after").Status, f.notes.starts)
			}
		})
	}
}
