package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// fakeCompactor is the agents' tools as the compact step reaches them: each
// ask is answered with the next of answers, the last one repeating.
type fakeCompactor struct {
	mu      sync.Mutex
	answers []error
	asks    []compactAsk
	// during runs inside an ask the agent takes, before the ask returns:
	// the agent's events can arrive that early.
	during func(taskID string)
}

type compactAsk struct{ taskID, instructions string }

func (f *fakeCompactor) CompactAgent(_ context.Context, taskID, instructions string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks = append(f.asks, compactAsk{taskID, instructions})
	var answer error
	if len(f.answers) > 0 {
		answer = f.answers[0]
	}
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	during := f.during
	f.mu.Unlock()
	if answer == nil && during != nil {
		during(taskID)
	}
	f.mu.Lock()
	return answer
}

func (f *fakeCompactor) answer(errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = errs
}

func (f *fakeCompactor) asked() []compactAsk {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]compactAsk(nil), f.asks...)
}

const compactWorkflow = `
schema_version: 1
name: compact
title: Compact
steps:
 - id: compact
   title: Compact the agent
   type: agent.compact
   with: WITH
 - id: after
   title: After
   type: task.context
   with: {task: "${{ workflow.task }}"}
`

type compactRun struct {
	db        *store.Store
	workflows *WorkflowService
	compactor *fakeCompactor
	run       *store.WorkflowRunView
	now       *time.Time
}

func startCompact(t *testing.T, with string, answers ...error) *compactRun {
	t.Helper()
	return startCompactWith(t, with, &fakeCompactor{answers: answers})
}

func startCompactWith(t *testing.T, with string, compactor *fakeCompactor) *compactRun {
	t.Helper()
	run := newCompactRun(t, compactor)
	run.start(t, with)
	return run
}

// newCompactRun is a Legato whose agents compactor compacts.
func newCompactRun(t *testing.T, compactor *fakeCompactor) *compactRun {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().UTC()
	executor := NewAgentCompactExecutor(compactor)
	run := &compactRun{db: db, compactor: compactor, now: &now}
	executor.SetClock(func() time.Time { return *run.now })
	run.workflows = NewWorkflowService(db, []WorkflowExecutor{executor, NewTaskContextExecutor(NewBoardService(db, nil))})
	return run
}

// start runs the compact workflow, its step taking with.
func (r *compactRun) start(t *testing.T, with string) {
	t.Helper()
	def, err := workflowengine.Parse("compact.yaml", []byte(strings.ReplaceAll(compactWorkflow, "WITH", with)))
	if err != nil {
		t.Fatal(err)
	}
	if r.run, err = r.workflows.Start(context.Background(), def, nil); err != nil {
		t.Fatal(err)
	}
}

// event is what the agent of the run's task said.
func (r *compactRun) event(t *testing.T, eventType, data string) {
	t.Helper()
	r.workflows.ObserveCompaction(context.Background(), WatchedAgent{TaskID: r.run.Run.TaskID, Slot: store.SlotMain},
		chimera.EventEnvelope{Type: eventType, Data: []byte(data)})
}

func TestACompactStepCompactsTheAgentAndWaitsForTheCompactionToFinish(t *testing.T) {
	run := startCompact(t, `{task: "${{ workflow.task }}", instructions: "Keep the API decisions"}`)
	asks := run.compactor.asked()
	if len(asks) != 1 || asks[0].taskID != run.run.Run.TaskID || asks[0].instructions != "Keep the API decisions" {
		t.Fatalf("asks = %+v; want the run task's agent asked once with the focus", asks)
	}
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s; want it waiting for the compaction to finish", status)
	}
	run.event(t, "compaction_started", `{"type":"compaction_started","reason":"manual"}`)
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s after compaction_started", status)
	}
	run.event(t, "compaction_finished", `{"type":"compaction_finished","tokensBefore":66815,"tokensAfter":48945}`)
	status, after := stepStatus(t, run.workflows, run.run.Run.ID, "compact")
	if status != "succeeded" || after.Run.Status != "succeeded" {
		t.Fatalf("step = %s, run = %s; want the compaction to complete the step and the run", status, after.Run.Status)
	}
	outputs := stepOutputs(t, after, "compact")
	if outputs["tokens_before"] != float64(66815) || outputs["tokens_after"] != float64(48945) {
		t.Fatalf("outputs = %v; want the sizes the agent reported", outputs)
	}
}

func TestACompactStepWaitsForTheAgentsTurnToEndBeforeCompacting(t *testing.T) {
	// The agent completed the step before inside its turn, which still runs.
	run := startCompact(t, `{task: "${{ workflow.task }}"}`, ErrAgentWorking, ErrAgentWorking, nil)
	ctx := context.Background()
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s; want it waiting for the agent's turn to end", status)
	}
	// A failure of a compaction the step did not ask for is not its own.
	run.event(t, "compaction_failed", `{"type":"compaction_failed","error":"a hook vetoed it"}`)
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s after a compaction it did not ask for failed", status)
	}
	for range 2 {
		if err := run.workflows.CheckCompactions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if asks := run.compactor.asked(); len(asks) != 3 {
		t.Fatalf("asked %d times; want the agent asked at the start and at each check until it took it", len(asks))
	}
	// Asked and compacting: checks ask no more.
	if err := run.workflows.CheckCompactions(ctx); err != nil {
		t.Fatal(err)
	}
	if asks := run.compactor.asked(); len(asks) != 3 {
		t.Fatalf("asked %d times; want no ask once the agent took one", len(asks))
	}
	run.event(t, "compaction_started", `{"type":"compaction_started","reason":"manual"}`)
	run.event(t, "compaction_finished", `{"type":"compaction_finished"}`)
	status, after := stepStatus(t, run.workflows, run.run.Run.ID, "compact")
	if status != "succeeded" || after.Run.Status != "succeeded" {
		t.Fatalf("step = %s, run = %s", status, after.Run.Status)
	}
	if outputs := stepOutputs(t, after, "compact"); len(outputs) != 0 {
		t.Fatalf("outputs = %v; want none when the tool reported no sizes", outputs)
	}
}

// stepError is a step's recorded error.
func stepError(t *testing.T, workflows *WorkflowService, runID, step string) string {
	t.Helper()
	_, view := stepStatus(t, workflows, runID, step)
	for _, s := range view.Steps {
		if s.StepID == step {
			return s.Error
		}
	}
	return ""
}

func TestACompactStepFailsWhenTheAgentIsStillWorkingAtTheDeadline(t *testing.T) {
	run := startCompact(t, `{task: "${{ workflow.task }}"}`, ErrAgentWorking)
	ctx := context.Background()
	*run.now = run.now.Add(CompactStepLimit - time.Minute)
	if err := run.workflows.CheckCompactions(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s before the deadline", status)
	}
	*run.now = run.now.Add(2 * time.Minute)
	if err := run.workflows.CheckCompactions(ctx); err != nil {
		t.Fatal(err)
	}
	status, after := stepStatus(t, run.workflows, run.run.Run.ID, "compact")
	if status != "failed" || after.Run.Status != "failed" {
		t.Fatalf("step = %s, run = %s; want both failed at the deadline", status, after.Run.Status)
	}
	if got := stepError(t, run.workflows, run.run.Run.ID, "compact"); got != "The agent was still working 15 minutes after the step started, so it was not compacted." {
		t.Fatalf("error = %q", got)
	}
}

func TestACompactStepFailsWhenItSeesNoEndToTheCompactionByTheDeadline(t *testing.T) {
	run := startCompact(t, `{task: "${{ workflow.task }}"}`)
	*run.now = run.now.Add(CompactStepLimit + time.Minute)
	if err := run.workflows.CheckCompactions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != "failed" {
		t.Fatalf("step = %s; want it failed at the deadline", status)
	}
	if got := stepError(t, run.workflows, run.run.Run.ID, "compact"); got != "Legato saw no end to the compaction within 15 minutes of the step starting. Retry the step to compact the agent again." {
		t.Fatalf("error = %q", got)
	}
}

func TestACompactStepFailsInTheRefusalsWordsWhenTheAgentCannotBeCompacted(t *testing.T) {
	refusal := refuseCompact(ErrCompactFocusUnsupported, "Codex compacts with its own prompt only and takes no focus instructions. Compact without them.")
	run := startCompact(t, `{task: "${{ workflow.task }}", instructions: "Keep the plan"}`, refusal)
	workflows, view := run.workflows, run.run
	if status, _ := stepStatus(t, workflows, view.Run.ID, "compact"); status != "failed" {
		t.Fatalf("step = %s; want it failed at once", status)
	}
	if got := stepError(t, workflows, view.Run.ID, "compact"); got != refusal.Error() {
		t.Fatalf("error = %q; want the refusal's words", got)
	}
}

func TestACompactStepFailsWhenItsAgentEndsBeforeItCouldBeCompacted(t *testing.T) {
	run := startCompact(t, `{task: "${{ workflow.task }}"}`, ErrAgentWorking, store.ErrNotFound)
	if err := run.workflows.CheckCompactions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != "failed" {
		t.Fatalf("step = %s; want it failed once the agent is gone", status)
	}
	want := "task " + run.run.Run.TaskID + " has no agent running to compact"
	if got := stepError(t, run.workflows, run.run.Run.ID, "compact"); got != want {
		t.Fatalf("error = %q; want %q", got, want)
	}
}

func TestACompactStepFailsWhenTheCompactionItAskedForFails(t *testing.T) {
	run := startCompact(t, `{task: "${{ workflow.task }}"}`)
	run.event(t, "compaction_failed", `{"type":"compaction_failed","error":"a PreCompact hook vetoed it"}`)
	if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != "failed" {
		t.Fatalf("step = %s; want it failed", status)
	}
	if got := stepError(t, run.workflows, run.run.Run.ID, "compact"); got != "The compaction failed: a PreCompact hook vetoed it" {
		t.Fatalf("error = %q", got)
	}
}

func TestTheCompactionsAnAgentsStreamReportsReachItsCompactStep(t *testing.T) {
	// The watcher follows task-1's agent, as it does every native agent.
	agent := newWatcherFixture(t)
	agent.start(t)
	// The agent is at its turn when the step starts, and idle after.
	run := startCompact(t, `{task: task-1}`, ErrAgentWorking, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := run.workflows.StartCompactWaits(ctx, agent.watcher, time.Millisecond)
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for len(run.compactor.asked()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("asked %d times; want the wait loop to ask again once the turn ends", len(run.compactor.asked()))
		}
		time.Sleep(time.Millisecond)
	}
	agent.stream.emit("compaction_started", map[string]any{"reason": "manual"})
	agent.stream.emit("compaction_finished", map[string]any{"tokensBefore": 1200, "tokensAfter": 300})
	for {
		status, after := stepStatus(t, run.workflows, run.run.Run.ID, "compact")
		if status == "succeeded" {
			if outputs := stepOutputs(t, after, "compact"); outputs["tokens_after"] != float64(300) {
				t.Fatalf("outputs = %v", outputs)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("step = %s; want the agent's compaction_finished to complete it", status)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestACompactionThatEndsBeforeItsAskReturnsStillEndsTheStep(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers []error
	}{
		{"asked when the step starts", nil},
		{"asked again once the turn ends", []error{ErrAgentWorking, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compactor := &fakeCompactor{answers: tc.answers}
			run := newCompactRun(t, compactor)
			// The agent's hook vetoes it at once: its event comes before
			// the ask's answer, and before the step is stored as waiting.
			compactor.during = func(taskID string) {
				agent := WatchedAgent{TaskID: taskID, Slot: store.SlotMain}
				for _, event := range []string{`{"type":"compaction_started","reason":"manual"}`, `{"type":"compaction_failed","error":"a PreCompact hook vetoed it"}`} {
					var typed struct{ Type string }
					_ = json.Unmarshal([]byte(event), &typed)
					run.workflows.ObserveCompaction(context.Background(), agent, chimera.EventEnvelope{Type: typed.Type, Data: []byte(event)})
				}
			}
			run.start(t, `{task: "${{ workflow.task }}"}`)
			for range 2 {
				if err := run.workflows.CheckCompactions(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if status, _ := stepStatus(t, run.workflows, run.run.Run.ID, "compact"); status != "failed" {
				t.Fatalf("step = %s; want the early failure to fail it", status)
			}
			if got := stepError(t, run.workflows, run.run.Run.ID, "compact"); got != "The compaction failed: a PreCompact hook vetoed it" {
				t.Fatalf("error = %q", got)
			}
		})
	}
}

const twoCompactsWorkflow = `
schema_version: 1
name: compact-twice
title: Compact twice
steps:
 - id: first
   title: Compact
   type: agent.compact
   with: {task: "${{ workflow.task }}"}
 - id: second
   title: Compact again
   type: agent.compact
   with: {task: "${{ workflow.task }}"}
`

func TestALateCopyOfOneCompactionsEndDoesNotEndTheNextCompactStep(t *testing.T) {
	// serve runs the workflow; the TUI beside it follows the same agent,
	// with its copy of the agent's stream running late: here it gets the
	// first compaction's whole story, as a reconnect replays it, only after
	// it has found the second step.
	run := newCompactRun(t, &fakeCompactor{})
	serveWorkflows := NewWorkflowService(run.db, []WorkflowExecutor{NewAgentCompactExecutor(&fakeCompactor{})})
	def, err := workflowengine.Parse("twice.yaml", []byte(twoCompactsWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	view, err := serveWorkflows.Start(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := WatchedAgent{TaskID: view.Run.TaskID, Slot: store.SlotMain}
	ctx := context.Background()
	say := func(workflows *WorkflowService, eventType string) {
		workflows.ObserveCompaction(ctx, agent, chimera.EventEnvelope{Type: eventType, Data: []byte(`{"type":"` + eventType + `"}`)})
	}
	// The TUI asks too; the agent refuses it while serve's compactions run.
	tui := NewWorkflowService(run.db, []WorkflowExecutor{NewAgentCompactExecutor(&fakeCompactor{answers: []error{ErrAgentWorking}})})
	say(serveWorkflows, "compaction_started")
	say(serveWorkflows, "compaction_finished")
	if status, _ := stepStatus(t, serveWorkflows, view.Run.ID, "first"); status != "succeeded" {
		t.Fatalf("first = %s", status)
	}
	if status, _ := stepStatus(t, serveWorkflows, view.Run.ID, "second"); status != WorkflowStepWaiting {
		t.Fatalf("second = %s; want it waiting on its own compaction", status)
	}
	if err := tui.CheckCompactions(ctx); err != nil {
		t.Fatal(err)
	}
	say(tui, "compaction_started")
	say(tui, "compaction_finished")
	if status, _ := stepStatus(t, serveWorkflows, view.Run.ID, "second"); status != WorkflowStepWaiting {
		t.Fatalf("second = %s; want the first compaction's end not to end it", status)
	}
	say(serveWorkflows, "compaction_started")
	say(serveWorkflows, "compaction_finished")
	if status, _ := stepStatus(t, serveWorkflows, view.Run.ID, "second"); status != "succeeded" {
		t.Fatalf("second = %s; want its own compaction to end it", status)
	}
}

func TestACompactionWhoseStartWentUnheardStillEndsItsStep(t *testing.T) {
	// The step asked before the agent's stream was followed: the start of
	// its compaction came before, its end after.
	run := startCompact(t, `{task: "${{ workflow.task }}"}`)
	run.event(t, "compaction_finished", `{"type":"compaction_finished","tokensAfter":900}`)
	status, after := stepStatus(t, run.workflows, run.run.Run.ID, "compact")
	if status != "succeeded" {
		t.Fatalf("step = %s; want the end of the compaction it asked for to end it", status)
	}
	if outputs := stepOutputs(t, after, "compact"); outputs["tokens_after"] != float64(900) {
		t.Fatalf("outputs = %v", outputs)
	}
}
