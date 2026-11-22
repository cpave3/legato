package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func storedStage(t *testing.T, db *store.Store, place WorkStagePlace) string {
	t.Helper()
	got, err := db.GetWorkStage(context.Background(), place)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		return ""
	}
	return got.Stage
}

// S10: a swarm played through. The conductor explores, plans (and needs
// the operator while its plan waits), implements once the plan is
// applied, and is finished with the swarm. Worker st-a implements, is in
// review once built and finished once closed done; worker st-b dies and
// keeps its stage.
func TestASwarmsStages(t *testing.T) {
	sw, agents, db, _ := newTestSwarmService(t)
	ctx := context.Background()
	seedParentTask(t, db, "parent-1")
	bus := events.New()
	stages := NewWorkStageService(db, bus)
	t.Cleanup(StartWorkStageMilestones(ctx, bus, stages, db))
	conductor := TaskPlace("parent-1", "")

	if err := sw.StartSwarm(ctx, "parent-1", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := stages.Start(ctx, conductor, ""); err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.EventPlanProposed, Payload: events.PlanProposedPayload{ParentTaskID: "parent-1", PlanPath: "/p.yaml"}})
	stageSoon(t, stages, conductor, store.WorkStagePlanning)
	if err := sw.InsertPendingPlan(ctx, "parent-1", "/p.yaml", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(ctx, "parent-1", store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	listed, err := agents.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.TaskID == "parent-1" && a.Glance() != GlanceNeedsYou {
			t.Fatalf("a conductor waiting on its plan reads %s, want needs you", a.Glance())
		}
	}

	plan := &swarm.Plan{
		Swarm: swarm.PlanHeader{ParentTaskID: "parent-1", WorkingDir: t.TempDir()},
		Steps: []swarm.PlanStep{{Subtasks: []swarm.PlanSubtask{{Title: "Backend", Role: "backend"}}}},
	}
	if err := sw.ApplyApprovedPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if got := storedStage(t, db, conductor); got != store.WorkStageImplementing {
		t.Fatalf("after the plan is applied the conductor is %q", got)
	}

	seedSubtask(t, db, "st-aaaaaaaa01", "parent-1", "in_progress")
	seedSubtask(t, db, "st-bbbbbbbb01", "parent-1", "in_progress")
	workerA, workerB := TaskPlace("st-aaaaaaaa01", ""), TaskPlace("st-bbbbbbbb01", "")
	for _, w := range []WorkStagePlace{workerA, workerB} {
		_ = stages.Start(ctx, w, "")
		_, _ = stages.Evidence(ctx, w, "", "edited x.go", false)
	}

	if err := sw.Built(ctx, "st-aaaaaaaa01"); err != nil {
		t.Fatal(err)
	}
	if got := storedStage(t, db, workerA); got != store.WorkStageReviewing {
		t.Fatalf("a built worker is %q", got)
	}
	if err := sw.Close(ctx, "st-aaaaaaaa01"); err != nil {
		t.Fatal(err)
	}
	if got := storedStage(t, db, workerA); got != store.WorkStageFinished {
		t.Fatalf("a worker closed done is %q", got)
	}

	sw.HandleAgentDied(ctx, "parent-1", "st-bbbbbbbb01", "worker")
	if got := storedStage(t, db, workerB); got != store.WorkStageImplementing {
		t.Fatalf("a worker that died is %q, want it to keep its stage", got)
	}

	if err := sw.Finish(ctx, "parent-1", "all done"); err != nil {
		t.Fatal(err)
	}
	if got := storedStage(t, db, conductor); got != store.WorkStageFinished {
		t.Fatalf("after the swarm finished the conductor is %q", got)
	}
	if got := storedStage(t, db, workerB); got != store.WorkStageImplementing {
		t.Fatalf("a cancelled worker is %q, want it to keep its stage", got)
	}
}

// sessionWorkflowExecutor starts each agent step as a workflow agent with
// an execution of its own.
type sessionWorkflowExecutor struct {
	db     *store.Store
	starts int
}

func (e *sessionWorkflowExecutor) Type() string { return "agent.run" }
func (e *sessionWorkflowExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.starts++
	executionID := fmt.Sprintf("ex-step-%d", e.starts)
	tmux := fmt.Sprintf("legato-wf-%d", e.starts)
	id, err := e.db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: step.TaskID, TmuxSession: tmux, AgentKind: "claude-code", Status: "running"})
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := e.db.CreateExecution(ctx, store.Execution{ID: executionID, Kind: store.ExecutionWorkflowAgent, Status: store.ExecutionRunning,
		TaskID: step.TaskID, ExecutionTaskID: step.TaskID, TaskTitle: "T", AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		return WorkflowExecutionResult{}, err
	}
	session, err := e.db.GetAgentSessionByID(ctx, id)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "waiting_agent", AgentSession: session}, nil
}
func (e *sessionWorkflowExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

// S11: a two-step workflow's step 1 agent is finished once its step
// succeeds, on its execution's place; step 2's agent is not.
func TestAWorkflowStepsAgentIsFinishedWhenItsStepSucceeds(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	definition, err := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: chain\ntitle: Chain\nsteps:\n  - id: one\n    title: One\n    type: agent.run\n    with: {prompt: one}\n  - id: two\n    title: Two\n    type: agent.run\n    with: {prompt: two}\n"))
	if err != nil {
		t.Fatal(err)
	}
	executor := &sessionWorkflowExecutor{db: db}
	svc := NewWorkflowServiceWithOutputs(db, []WorkflowExecutor{executor}, t.TempDir())
	view, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := view.Run.TaskID
	stepOne := WorkStagePlace{TaskID: task, Slot: store.SlotMain, PlaceExecutionID: "ex-step-1"}
	stepTwo := WorkStagePlace{TaskID: task, Slot: store.SlotMain, PlaceExecutionID: "ex-step-2"}
	if _, err := svc.Complete(ctx, view.Run.ID, "one", view.Attempts[0].ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := storedStage(t, db, stepOne); got != store.WorkStageFinished {
		t.Fatalf("step one's agent is %q, want finished", got)
	}
	if got := storedStage(t, db, stepTwo); got == store.WorkStageFinished {
		t.Fatal("step two's agent is finished before its step")
	}
	if got := storedStage(t, db, TaskPlace(task, "")); got != "" {
		t.Fatalf("the run task's own place got %q", got)
	}
}

// A swarm worker's task is its sub-task, which is no task row: the agents
// list titles it from the sub-task.
func TestASwarmWorkerIsTitledFromItsSubtask(t *testing.T) {
	sw, agents, db, _ := newTestSwarmService(t)
	ctx := context.Background()
	seedParentTask(t, db, "parent-1")
	seedSubtask(t, db, "st-cccccccc01", "parent-1", "queued")
	if err := sw.Dispatch(ctx, "st-cccccccc01"); err != nil {
		t.Fatal(err)
	}
	listed, err := agents.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.SubtaskID == "st-cccccccc01" {
			if a.Title != "sub st-cccccccc01" {
				t.Fatalf("worker title = %q", a.Title)
			}
			return
		}
	}
	t.Fatalf("no worker listed in %+v", listed)
}
