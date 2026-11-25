package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// scriptedExecutor fails its first starts with the errors given, then
// succeeds with outputs.
type scriptedExecutor struct {
	typeName string
	failures []error
	outputs  map[string]any
	starts   []ResolvedWorkflowStep
}

func (f *scriptedExecutor) Type() string { return f.typeName }
func (f *scriptedExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	f.starts = append(f.starts, step)
	if len(f.failures) > 0 {
		err := f.failures[0]
		f.failures = f.failures[1:]
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: f.outputs}, nil
}
func (f *scriptedExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func controlService(t *testing.T, executors ...WorkflowExecutor) (*WorkflowService, *store.Store) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return NewWorkflowService(s, executors), s
}

func parseControlWorkflow(t *testing.T, text string) *workflowengine.Definition {
	t.Helper()
	definition, err := workflowengine.Parse("control.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func stepOf(t *testing.T, view *store.WorkflowRunView, path string) store.WorkflowStep {
	t.Helper()
	for _, step := range view.Steps {
		if step.Path == path {
			return step
		}
	}
	t.Fatalf("no step %s in %+v", path, view.Steps)
	return store.WorkflowStep{}
}

func outputsOf(t *testing.T, step store.WorkflowStep) map[string]any {
	t.Helper()
	var outputs map[string]any
	if err := json.Unmarshal([]byte(step.OutputsJSON), &outputs); err != nil {
		t.Fatal(err)
	}
	return outputs
}

const endToEndControl = `schema_version: 2
name: control
title: Control
inputs:
  deliver: {type: boolean, default: false}
steps:
  - id: find
    type: task.context
    with: {task: "${{ workflow.task }}"}
    on_failure: continue
  - id: busy
    if: {field: steps.find.outputs.error_code, equals: review_loop_active}
    type: task.note
    with: {task: "${{ workflow.task }}", note: "Busy: ${{ steps.find.outputs.error }}"}
  - id: deliver
    if: {field: workflow.inputs.deliver, equals: true}
    type: task.note
    with: {task: "${{ workflow.task }}", note: delivered}
  - id: ask
    type: gate
    with: {title: "Go on?", body: "${{ steps.deliver.outputs.note_id ?? 'Nothing was delivered.' }}"}
  - id: after
    if: {field: steps.ask.outputs.choice, equals: Continue}
    type: task.note
    with: {task: "${{ workflow.task }}", note: "chose ${{ steps.ask.outputs.choice }}"}
`

// A workflow with an if: on a step, a continue failure and a gate runs end
// to end (LEG-486).
func TestIfContinueAndGateRunEndToEnd(t *testing.T) {
	find := &scriptedExecutor{typeName: "task.context", failures: []error{&WorkflowStepError{Code: "review_loop_active", Link: "/workflows/runs/wr-other", Message: "another loop holds the pull request"}}}
	note := &scriptedExecutor{typeName: "task.note", outputs: map[string]any{"note_id": "n1"}}
	svc, _ := controlService(t, find, note)
	ctx := context.Background()

	view, err := svc.Start(ctx, parseControlWorkflow(t, endToEndControl), nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := stepOf(t, view, "find")
	if failed.Status != "failed" {
		t.Fatalf("find = %s", failed.Status)
	}
	if got := outputsOf(t, failed); got["status"] != "failed" || got["error"] != "another loop holds the pull request" || got["error_code"] != "review_loop_active" {
		t.Fatalf("find's outputs = %v", got)
	}
	if len(note.starts) != 1 || note.starts[0].StepID != "busy" || note.starts[0].With["note"] != "Busy: another loop holds the pull request" {
		t.Fatalf("note starts = %+v", note.starts)
	}
	if got := stepOf(t, view, "deliver").Status; got != "skipped" {
		t.Fatalf("deliver = %s, want skipped", got)
	}
	if view.Run.Status != "running" || view.Run.CurrentStepID != "ask" || stepOf(t, view, "ask").Status != WorkflowStepWaiting {
		t.Fatalf("run = %s at %s, ask %s", view.Run.Status, view.Run.CurrentStepID, stepOf(t, view, "ask").Status)
	}

	shown, err := svc.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	gate, ok := shown.Details["ask"].(GateDetails)
	if !ok || gate.Title != "Go on?" || gate.Body != "Nothing was delivered." || strings.Join(gate.Options, ",") != "Continue,Stop" {
		t.Fatalf("gate details = %#v", shown.Details["ask"])
	}

	view, err = svc.StepAction(ctx, view.Run.ID, "ask", GateChoiceAction("Continue"), "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %s, want succeeded", view.Run.Status)
	}
	if got := outputsOf(t, stepOf(t, view, "ask")); got["choice"] != "Continue" {
		t.Fatalf("ask's outputs = %v", got)
	}
	if len(note.starts) != 2 || note.starts[1].With["note"] != "chose Continue" {
		t.Fatalf("note starts = %+v", note.starts)
	}
}

func TestAGatesStopEndsTheRunStopped(t *testing.T) {
	note := &scriptedExecutor{typeName: "task.note"}
	svc, _ := controlService(t, note)
	ctx := context.Background()
	view, err := svc.Start(ctx, parseControlWorkflow(t, `schema_version: 2
name: gate
title: Gate
steps:
  - {id: ask, type: gate, with: {title: Ship it, options: [Ship, Stop]}}
  - {id: after, type: task.note, with: {task: "${{ workflow.task }}", note: shipped}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StepAction(ctx, view.Run.ID, "ask", GateChoiceAction("Hold"), ""); err == nil {
		t.Fatal("a choice the gate does not offer was taken")
	}
	view, err = svc.StepAction(ctx, view.Run.ID, "ask", GateChoiceAction("Stop"), "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "stopped" || len(note.starts) != 0 {
		t.Fatalf("run = %s, note starts %d", view.Run.Status, len(note.starts))
	}
}

const pauseControl = `schema_version: 2
name: pause
title: Pause
steps:
  - {id: open, type: task.context, with: {task: "${{ workflow.task }}"}, on_failure: pause}
  - {id: after, type: task.note, with: {task: "${{ workflow.task }}", note: "open said ${{ steps.open.outputs.content ?? 'nothing' }}"}}
`

func TestAPauseGateRetries(t *testing.T) {
	open := &scriptedExecutor{typeName: "task.context", failures: []error{&WorkflowStepError{Code: "review_loop_active", Link: "/workflows/runs/wr-other", Message: "another loop holds it"}}, outputs: map[string]any{"content": "ok"}}
	note := &scriptedExecutor{typeName: "task.note"}
	svc, _ := controlService(t, open, note)
	ctx := context.Background()
	view, err := svc.Start(ctx, parseControlWorkflow(t, pauseControl), nil)
	if err != nil {
		t.Fatal(err)
	}
	paused := stepOf(t, view, "open")
	if view.Run.Status != "waiting" || paused.Status != WorkflowStepPaused || paused.ErrorCode != "review_loop_active" || paused.ErrorLink != "/workflows/runs/wr-other" {
		t.Fatalf("run %s, open %+v", view.Run.Status, paused)
	}
	shown, err := svc.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !shown.Run.NeedsAttention || !strings.Contains(shown.Run.AttentionReason, "/workflows/runs/wr-other") {
		t.Fatalf("attention = %v %q", shown.Run.NeedsAttention, shown.Run.AttentionReason)
	}
	view, err = svc.StepAction(ctx, view.Run.ID, "open", PausedRetry, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || len(open.starts) != 2 || len(note.starts) != 1 || note.starts[0].With["note"] != "open said ok" {
		t.Fatalf("run %s, open starts %d, note starts %+v", view.Run.Status, len(open.starts), note.starts)
	}
}

func TestAPauseGateSkipsAndStops(t *testing.T) {
	for _, action := range []string{PausedSkip, PausedStop} {
		open := &scriptedExecutor{typeName: "task.context", failures: []error{&WorkflowStepError{Message: "no"}}}
		note := &scriptedExecutor{typeName: "task.note"}
		svc, _ := controlService(t, open, note)
		ctx := context.Background()
		view, err := svc.Start(ctx, parseControlWorkflow(t, pauseControl), nil)
		if err != nil {
			t.Fatal(err)
		}
		view, err = svc.StepAction(ctx, view.Run.ID, "open", action, "")
		if err != nil {
			t.Fatal(err)
		}
		switch action {
		case PausedSkip:
			// A skipped step's outputs are missing: the fallback applies.
			if view.Run.Status != "succeeded" || stepOf(t, view, "open").Status != "skipped" || len(note.starts) != 1 || note.starts[0].With["note"] != "open said nothing" {
				t.Fatalf("skip: run %s, open %s, note starts %+v", view.Run.Status, stepOf(t, view, "open").Status, note.starts)
			}
		case PausedStop:
			if view.Run.Status != "stopped" || len(note.starts) != 0 {
				t.Fatalf("stop: run %s, note starts %d", view.Run.Status, len(note.starts))
			}
		}
	}
}

// A failure that ends an agent's step outside the run's advance (the agent
// fails it, or its session ends) still follows on_failure: continue.
func TestContinueAfterAnAgentStepFails(t *testing.T) {
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	note := &scriptedExecutor{typeName: "task.note"}
	svc, _ := controlService(t, agent, note)
	ctx := context.Background()
	view, err := svc.Start(ctx, parseControlWorkflow(t, `schema_version: 2
name: agent
title: Agent
steps:
  - {id: work, type: agent.run, with: {prompt: go}, on_failure: continue}
  - {id: after, if: {field: steps.work.outputs.status, equals: failed}, type: task.note, with: {task: "${{ workflow.task }}", note: "${{ steps.work.outputs.error }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	view, err = svc.Fail(ctx, view.Run.ID, "work", attempt.ID, "the agent gave up")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" || len(note.starts) != 1 || note.starts[0].With["note"] != "the agent gave up" {
		t.Fatalf("run %s, note starts %+v", view.Run.Status, note.starts)
	}
}

func TestTheGateAndPauseRaiseAttention(t *testing.T) {
	open := &scriptedExecutor{typeName: "task.context", failures: []error{&WorkflowStepError{Code: "review_loop_active", Link: "/workflows/runs/wr-other", Message: "another loop holds it"}}}
	svc, _ := controlService(t, open)
	ctx := context.Background()
	paused, err := svc.Start(ctx, parseControlWorkflow(t, `schema_version: 2
name: pause
title: Pause
steps:
  - {id: open, type: task.context, with: {task: "${{ workflow.task }}"}, on_failure: pause}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	gated, err := svc.Start(ctx, parseControlWorkflow(t, `schema_version: 2
name: gate
title: Gate
steps:
  - {id: ask, type: gate, with: {title: Merge now, options: [Merge, Wait, Stop]}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := NewWorkflowAttentionAdapter(svc).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actions := func(item AttentionInput) string {
		var ids []string
		for _, action := range item.Actions {
			ids = append(ids, action.ID+"="+action.Label)
		}
		return strings.Join(ids, ",")
	}
	var sawGate, sawPause bool
	for _, item := range items {
		switch item.SourceKey {
		case gated.Run.ID + ":ask":
			sawGate = true
			if item.SignalKind != AttentionSignalWorkflowGate || item.Title != "Merge now" ||
				actions(item) != "open=Open run,choose:Merge=Merge,choose:Wait=Wait,choose:Stop=Stop" {
				t.Errorf("gate item = %+v (%s)", item, actions(item))
			}
		case paused.Run.ID + ":open":
			sawPause = true
			if item.SignalKind != AttentionSignalWorkflowFailed || !strings.Contains(item.Reason, "/workflows/runs/wr-other") ||
				actions(item) != "open=Open run,retry=Retry,skip=Skip,stop=Stop" {
				t.Errorf("pause item = %+v (%s)", item, actions(item))
			}
		}
	}
	if !sawGate || !sawPause {
		t.Fatalf("items = %+v", items)
	}
	adapter := NewWorkflowAttentionAdapter(svc)
	if err := adapter.Execute(ctx, AttentionActionRequest{SourceKey: gated.Run.ID + ":ask", ActionID: "choose:Merge"}); err != nil {
		t.Fatal(err)
	}
	if view, _ := svc.Show(ctx, gated.Run.ID); view.Run.Status != "succeeded" {
		t.Fatalf("gated run = %s", view.Run.Status)
	}
	if err := adapter.Execute(ctx, AttentionActionRequest{SourceKey: paused.Run.ID + ":open", ActionID: "stop"}); err != nil {
		t.Fatal(err)
	}
	if view, _ := svc.Show(ctx, paused.Run.ID); view.Run.Status != "stopped" {
		t.Fatalf("paused run = %s", view.Run.Status)
	}
}

// The agent service fails a step whose agent ended straight in the store:
// a continued failure is left due an advance, which a host takes.
func TestAContinuedFailureInTheStoreIsAdvancedByAHost(t *testing.T) {
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	note := &scriptedExecutor{typeName: "task.note"}
	svc, s := controlService(t, agent, note)
	ctx := context.Background()
	view, err := svc.Start(ctx, parseControlWorkflow(t, `schema_version: 2
name: agent
title: Agent
steps:
  - {id: work, type: agent.run, with: {prompt: go}, on_failure: continue}
  - {id: after, type: task.note, with: {task: "${{ workflow.task }}", note: "${{ steps.work.outputs.error }}"}}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailWorkflowStep(ctx, view.Run.ID, "work", view.Attempts[0].ID, "the agent's session ended"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceDue(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ = svc.Show(ctx, view.Run.ID)
	if view.Run.Status != "succeeded" || len(note.starts) != 1 || note.starts[0].With["note"] != "the agent's session ended" {
		t.Fatalf("run %s, note starts %+v", view.Run.Status, note.starts)
	}
}

const gateBodyControl = `schema_version: 2
name: gate-body
title: Gate body
steps:
  - id: write
    type: agent.run
    with: {prompt: write}
    outputs:
      report: {type: file, path: report.md}
  - id: ask
    type: gate
    with: {title: "Go on?", body_path: "%s"}
`

// A gate reads body_path only from a file the run's own steps output, which
// Legato copied into its data folder: never a path in an agent's checkout,
// which the agent could make a link to a file its sandbox hides (LEG-486
// review).
func TestAGateReadsBodyPathOnlyFromTheRunsOwnFileOutputs(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T, bodyPath string) (*WorkflowService, *store.WorkflowRunView, string) {
		t.Helper()
		db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "report.md"), []byte("# Report"), 0o600); err != nil {
			t.Fatal(err)
		}
		agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent", WorkingDir: workDir}}
		svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{agent}, t.TempDir())
		if bodyPath == "" {
			bodyPath = "${{ steps.write.outputs.report.path }}"
		} else {
			bodyPath = strings.ReplaceAll(bodyPath, "WORKDIR", workDir)
		}
		view, err := svc.Start(ctx, parseControlWorkflow(t, fmt.Sprintf(gateBodyControl, bodyPath)), nil)
		if err != nil {
			t.Fatal(err)
		}
		runID := view.Run.ID
		// A gate that refuses its body_path fails as it starts, which the
		// completion reports: the run is read back either way.
		_, _ = svc.Complete(ctx, runID, "write", view.Attempts[0].ID, nil, map[string]string{"report": "report.md"})
		view, err = svc.Show(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		return svc, view, workDir
	}

	t.Run("an earlier step's file output", func(t *testing.T) {
		svc, view, workDir := start(t, "")
		// The agent's own copy changes later: the gate shows what was
		// collected.
		if err := os.WriteFile(filepath.Join(workDir, "report.md"), []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		shown, err := svc.Show(ctx, view.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gate, ok := shown.Details["ask"].(GateDetails); !ok || gate.Body != "# Report" {
			t.Fatalf("gate details = %#v", shown.Details["ask"])
		}
	})

	t.Run("a path in the agent's checkout, linked to a hidden file", func(t *testing.T) {
		secret := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(secret, []byte("SENTINEL"), 0o600); err != nil {
			t.Fatal(err)
		}
		svc, view, workDir := start(t, "WORKDIR/report.md")
		if err := os.Remove(filepath.Join(workDir, "report.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(workDir, "report.md")); err != nil {
			t.Fatal(err)
		}
		shown, err := svc.Show(ctx, view.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprintf("%#v", shown.Details), "SENTINEL") {
			t.Fatalf("the gate showed a file outside the run's outputs: %#v", shown.Details)
		}
		if got := stepOf(t, shown, "ask"); got.Status != "failed" || !strings.Contains(got.Error, "body_path") {
			t.Fatalf("ask = %s %q, want failed over body_path", got.Status, got.Error)
		}
	})
}

// A step whose expressions cannot be resolved (here, an output of a step
// if: skipped, with no fallback) fails as a step that started and failed
// does, and follows its on_failure: the run is never left running at a
// step with no attempt (LEG-486 review).
func TestAStepWhoseExpressionsFailFollowsItsOnFailure(t *testing.T) {
	for _, tc := range []struct {
		onFailure, runStatus, stepStatus string
	}{
		{"fail", "failed", "failed"},
		{"pause", "waiting", WorkflowStepPaused},
		{"continue", "succeeded", "failed"},
	} {
		t.Run(tc.onFailure, func(t *testing.T) {
			note := &scriptedExecutor{typeName: "task.note"}
			svc, _ := controlService(t, note)
			view, err := svc.Start(context.Background(), parseControlWorkflow(t, fmt.Sprintf(`schema_version: 2
name: missing
title: Missing
inputs:
  produce: {type: boolean, default: false}
steps:
  - {id: a, if: {field: workflow.inputs.produce, equals: true}, type: task.note, with: {task: "${{ workflow.task }}", note: a}}
  - {id: b, type: task.note, with: {task: "${{ workflow.task }}", note: "Summary: ${{ steps.a.outputs.note_id }}"}, on_failure: %s}
`, tc.onFailure)), nil)
			if view == nil {
				t.Fatalf("Start gave no run: %v", err)
			}
			b := stepOf(t, view, "b")
			if view.Run.Status != tc.runStatus || b.Status != tc.stepStatus || b.LatestAttempt != 1 || !strings.Contains(b.Error, "steps.a.outputs.note_id") {
				t.Fatalf("run %s, b %s (attempt %d, error %q); want run %s, b %s with the missing output named", view.Run.Status, b.Status, b.LatestAttempt, b.Error, tc.runStatus, tc.stepStatus)
			}
			if len(note.starts) != 0 {
				t.Fatalf("note starts = %+v", note.starts)
			}
		})
	}
}

// Retrying a step suspended when its agent was interrupted starts it again,
// whatever its on_failure says: retiring the interrupted attempt is not the
// step's failure, so a continue never moves the run past it (LEG-486
// review).
func TestRetryingASuspendedStepIgnoresItsOnFailure(t *testing.T) {
	for _, onFailure := range []string{"continue", "pause"} {
		t.Run(onFailure, func(t *testing.T) {
			agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
			svc, s := controlService(t, agent)
			ctx := context.Background()
			view, err := svc.Start(ctx, parseControlWorkflow(t, fmt.Sprintf(`schema_version: 2
name: agent
title: Agent
steps:
  - {id: work, type: agent.run, with: {prompt: go}, on_failure: %s}
`, onFailure)), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SuspendWorkflowStep(ctx, view.Run.ID, "work", view.Attempts[0].ID, "the agent was interrupted"); err != nil {
				t.Fatal(err)
			}
			view, err = svc.Retry(ctx, view.Run.ID, "work", false)
			if err != nil {
				t.Fatal(err)
			}
			if view.Run.Status != "running" || view.Run.CurrentStepID != "work" || len(view.Attempts) != 2 || view.Attempts[1].Status != "waiting_agent" {
				t.Fatalf("run %s at %q, attempts %+v; want a second attempt waiting on its agent", view.Run.Status, view.Run.CurrentStepID, view.Attempts)
			}
		})
	}
}
