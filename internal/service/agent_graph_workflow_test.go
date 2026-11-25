package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// graphStep is one top-level step of a run made for a graph test: its ID,
// executor, how it starts and its status.
type graphStep struct {
	id, executor, start, status string
}

// workflowRun makes a run with its top-level steps, at current, on task
// (the run's own task, "" for none), with the workflow service reading it.
func (f *graphFixture) workflowRun(t *testing.T, id, status, current, task string, steps ...graphStep) {
	t.Helper()
	ctx := context.Background()
	if task != "" {
		if _, err := f.store.GetTask(ctx, task); err != nil {
			createTask(t, f.store, task)
		}
	}
	type defStep struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
		Start string `json:"start,omitempty"`
	}
	definition := struct {
		SchemaVersion int       `json:"schema_version"`
		Name          string    `json:"name"`
		Steps         []defStep `json:"steps"`
	}{SchemaVersion: 1, Name: "flow"}
	rows := make([]store.WorkflowStep, len(steps))
	for i, step := range steps {
		definition.Steps = append(definition.Steps, defStep{ID: step.id, Title: "Step " + step.id, Type: step.executor, Start: step.start})
		rows[i] = store.WorkflowStep{RunID: id, StepID: step.id, StepOrder: i, Title: "Step " + step.id, ExecutorType: step.executor, Status: "pending"}
	}
	raw, _ := json.Marshal(definition)
	if err := f.store.CreateWorkflowRun(ctx, store.WorkflowRun{ID: id, WorkflowName: "flow", WorkflowTitle: "Run " + id,
		DefinitionJSON: string(raw), InputsJSON: "{}", Status: status, CurrentStepID: current, TaskID: task}, rows); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.status == "pending" || step.status == "" {
			continue
		}
		if _, err := f.store.DB().ExecContext(ctx, `UPDATE workflow_steps SET status = ? WHERE run_id = ? AND path = ?`, step.status, id, step.id); err != nil {
			t.Fatal(err)
		}
	}
}

// agentStep starts step of run as an agent.run attempt and an agent for
// it on task, the step waiting on that agent.
func (f *graphFixture) agentStep(t *testing.T, run, step, task string) {
	t.Helper()
	ctx := context.Background()
	attempt := "att-" + run + "-" + step
	if err := f.store.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: attempt, RunID: run, StepID: step, AttemptNumber: 1,
		ExecutorType: "agent.run", TaskID: task, Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetTask(ctx, task); err != nil {
		createTask(t, f.store, task)
	}
	if err := f.svc.SpawnAgent(ctx, task, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir(),
		WorkflowRunID: run, WorkflowAttemptID: attempt}); err != nil {
		t.Fatalf("starting the agent of %s/%s: %v", run, step, err)
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkWorkflowStepWaiting(ctx, run, step, attempt, session, "", task); err != nil {
		t.Fatal(err)
	}
}

// interview starts step of run as an agent.interview attempt that
// borrows task's running agent.
func (f *graphFixture) interview(t *testing.T, run, step, task string) {
	t.Helper()
	ctx := context.Background()
	attempt := "att-" + run + "-" + step
	if err := f.store.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: attempt, RunID: run, StepID: step, AttemptNumber: 1,
		ExecutorType: "agent.interview", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkWorkflowStepWaiting(ctx, run, step, attempt, session, "", ""); err != nil {
		t.Fatal(err)
	}
}

// reviewStep starts step of run as its review loop at pass, keyed on the
// run's task, reviewing the work of author.
func (f *graphFixture) reviewStep(t *testing.T, run, step, runTask, author string, pass int) {
	t.Helper()
	ctx := context.Background()
	attempt := "att-" + run + "-" + step
	loop := reviewRun{Repo: "cpave3/legato", PR: 42, AuthorTask: author, LinkedTask: author, RunID: run, StepID: step, AttemptID: attempt}
	if err := openReviewRun(ctx, f.store, runTask, loop, ReviewPassSettings{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.store.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: attempt, RunID: run, StepID: step, AttemptNumber: 1,
		ExecutorType: ReviewPassesStepType, TaskID: runTask, Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.WaitWorkflowStepOnService(ctx, run, step, attempt); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: runTask, Phase: ReviewPhaseReviewing, Pass: pass}); err != nil {
		t.Fatal(err)
	}
}

func newWorkflowGraphFixture(t *testing.T) *graphFixture {
	t.Helper()
	f := newGraphFixture(t)
	f.graph = NewAgentGraphService(AgentGraphOptions{
		Store: f.store, Agents: f.svc, Related: f.related, Runs: NewWorkflowService(f.store, nil),
	})
	return f
}

func workflowsOf(t *testing.T, graph AgentGraph, agent string) []AgentWorkflowPlace {
	t.Helper()
	place, ok := graph.Agents[agent]
	if !ok {
		t.Fatalf("%s is not on the graph: %+v", agent, graph.Agents)
	}
	return place.Workflows
}

func TestAWorkflowAgentIsPlacedOnItsRunsStep(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	f.workflowRun(t, "wr-1", "running", "triage", "",
		graphStep{"context", "task.context", "automatic", "succeeded"},
		graphStep{"triage", "agent.run", "automatic", ""},
		graphStep{"note", "task.note", "automatic", ""})
	f.agentStep(t, "wr-1", "triage", "tri-1")

	graph := f.read(t)
	if got, want := workflowsOf(t, graph, "tri-1"), []AgentWorkflowPlace{{RunID: "wr-1", StepID: "triage", Role: AgentRoleAgent}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tri-1's workflows = %+v, want %+v", got, want)
	}
	run, ok := graph.Runs["wr-1"]
	if !ok {
		t.Fatalf("runs = %+v, want wr-1", graph.Runs)
	}
	if run.Status != "running" || run.CurrentStepID != "triage" || run.WorkflowTitle != "Run wr-1" {
		t.Errorf("wr-1 = %+v, want running at triage, titled Run wr-1", run)
	}
	var steps []string
	for _, step := range run.Steps {
		steps = append(steps, step.StepID+":"+step.Status)
	}
	if want := []string{"context:succeeded", "triage:waiting_agent", "note:pending"}; !reflect.DeepEqual(steps, want) {
		t.Errorf("wr-1's steps = %v, want %v", steps, want)
	}
}

func TestAnAgentWithNoRunHasNoWorkflow(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	createTask(t, f.store, "task-a")
	f.spawn(t, "task-a", "")

	graph := f.read(t)
	if got := workflowsOf(t, graph, "task-a"); got != nil {
		t.Errorf("task-a's workflows = %+v, want none", got)
	}
	if len(graph.Runs) != 0 {
		t.Errorf("runs = %+v, want none", graph.Runs)
	}
}

func TestAKeptAgentIsTheAuthorOnTheRunsCurrentStep(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	ctx := context.Background()
	f.workflowRun(t, "wr-2", "running", "implement", "run-2",
		graphStep{"implement", "agent.run", "automatic", ""},
		graphStep{"review", ReviewPassesStepType, "automatic", ""})
	f.agentStep(t, "wr-2", "implement", "LEG-1")
	// The implement step keeps its agent and the run moves on to review.
	if err := f.store.CompleteWorkflowStep(ctx, "wr-2", "implement", "att-wr-2-implement", "{}", "review"); err != nil {
		t.Fatal(err)
	}
	f.reviewStep(t, "wr-2", "review", "run-2", "LEG-1", 2)
	f.spawn(t, "run-2", SlotReviewer)

	graph := f.read(t)
	if got, want := workflowsOf(t, graph, "LEG-1"), []AgentWorkflowPlace{{RunID: "wr-2", StepID: "review", Role: AgentRoleAuthor}}; !reflect.DeepEqual(got, want) {
		t.Errorf("LEG-1's workflows = %+v, want %+v", got, want)
	}
	if got, want := workflowsOf(t, graph, "run-2_reviewer"), []AgentWorkflowPlace{{RunID: "wr-2", StepID: "review", Role: AgentRoleReviewer}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the reviewer's workflows = %+v, want %+v", got, want)
	}
	if len(graph.Runs) != 1 {
		t.Fatalf("runs = %+v, want the one run both work for", graph.Runs)
	}
	if got := graph.Runs["wr-2"]; got.CurrentStepID != "review" || got.Pass != 2 {
		t.Errorf("wr-2 = %+v, want at review, pass 2", got)
	}
}

func TestAnInterviewedAgentIsPlacedOnTheInterviewStep(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	createTask(t, f.store, "LEG-2")
	f.spawn(t, "LEG-2", "")
	f.workflowRun(t, "wr-3", "running", "ask", "",
		graphStep{"ask", "agent.interview", "automatic", ""})
	f.interview(t, "wr-3", "ask", "LEG-2")

	if got, want := workflowsOf(t, f.read(t), "LEG-2"), []AgentWorkflowPlace{{RunID: "wr-3", StepID: "ask", Role: AgentRoleInterview}}; !reflect.DeepEqual(got, want) {
		t.Errorf("LEG-2's workflows = %+v, want %+v", got, want)
	}
}

// An agent.run step on a task (on: <task>) waits on the task's own agent,
// which has no run of its own: it is placed on the step by its session.
func TestATasksOwnAgentAStepWaitsOnIsPlacedOnTheStep(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-4")
	f.spawn(t, "LEG-4", "")
	f.workflowRun(t, "wr-4", "running", "implement", "",
		graphStep{"implement", "agent.run", "automatic", ""})
	if err := f.store.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: "att-4", RunID: "wr-4", StepID: "implement", AttemptNumber: 1,
		ExecutorType: "agent.run", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, "LEG-4")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkWorkflowStepWaiting(ctx, "wr-4", "implement", "att-4", session, "", "LEG-4"); err != nil {
		t.Fatal(err)
	}

	if got, want := workflowsOf(t, f.read(t), "LEG-4"), []AgentWorkflowPlace{{RunID: "wr-4", StepID: "implement", Role: AgentRoleAgent}}; !reflect.DeepEqual(got, want) {
		t.Errorf("LEG-4's workflows = %+v, want %+v", got, want)
	}
}

func TestAnAgentARunWaitsOnToEndIsAwaited(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	createTask(t, f.store, "LEG-3")
	f.spawn(t, "LEG-3", "")
	f.workflowRun(t, "wr-4", "waiting", "after", "LEG-3",
		graphStep{"dispatch", "agent.dispatch", "automatic", "succeeded"},
		graphStep{"after", "task.note", "agent_ended", "waiting_manual"})

	if got, want := workflowsOf(t, f.read(t), "LEG-3"), []AgentWorkflowPlace{{RunID: "wr-4", StepID: "after", Role: AgentRoleAwaited}}; !reflect.DeepEqual(got, want) {
		t.Errorf("LEG-3's workflows = %+v, want %+v", got, want)
	}
}

func TestAFinishedRunIsLeftOut(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	ctx := context.Background()
	f.workflowRun(t, "wr-5", "running", "implement", "run-5",
		graphStep{"implement", "agent.run", "automatic", ""})
	f.agentStep(t, "wr-5", "implement", "LEG-5")
	// The step keeps its agent and the run ends.
	if err := f.store.CompleteWorkflowStep(ctx, "wr-5", "implement", "att-wr-5-implement", "{}", ""); err != nil {
		t.Fatal(err)
	}

	graph := f.read(t)
	if got := workflowsOf(t, graph, "LEG-5"); got != nil {
		t.Errorf("LEG-5's workflows = %+v, want none once its run has finished", got)
	}
	if len(graph.Runs) != 0 {
		t.Errorf("runs = %+v, want none", graph.Runs)
	}
}

func TestARunWithNoRunningAgentIsLeftOut(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	f.workflowRun(t, "wr-6", "running", "note", "",
		graphStep{"note", "task.note", "automatic", ""})
	createTask(t, f.store, "task-a")
	f.spawn(t, "task-a", "")

	if runs := f.read(t).Runs; len(runs) != 0 {
		t.Errorf("runs = %+v, want none: no agent works for wr-6", runs)
	}
}

func TestAnAgentInTwoRunsListsTheOneThatNeedsYouFirst(t *testing.T) {
	f := newWorkflowGraphFixture(t)
	// wr-a's agent.run step is LEG-4's; LEG-4 is idle, so wr-a needs you.
	f.workflowRun(t, "wr-a", "running", "implement", "",
		graphStep{"implement", "agent.run", "automatic", ""})
	f.agentStep(t, "wr-a", "implement", "LEG-4")
	// wr-b, the newer by ID, waits for LEG-4 to end.
	f.workflowRun(t, "wr-b", "waiting", "after", "LEG-4",
		graphStep{"after", "task.note", "agent_ended", "waiting_manual"})

	graph := f.read(t)
	want := []AgentWorkflowPlace{
		{RunID: "wr-a", StepID: "implement", Role: AgentRoleAgent},
		{RunID: "wr-b", StepID: "after", Role: AgentRoleAwaited},
	}
	if got := workflowsOf(t, graph, "LEG-4"); !reflect.DeepEqual(got, want) {
		t.Errorf("LEG-4's workflows = %+v, want %+v", got, want)
	}
	if !graph.Runs["wr-a"].NeedsAttention || graph.Runs["wr-b"].NeedsAttention {
		t.Errorf("runs = %+v, want only wr-a needing you", graph.Runs)
	}
}
