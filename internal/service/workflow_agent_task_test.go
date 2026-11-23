package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// taskAgentWorkflow is a run whose agent.run step works as the selected
// task's own agent (LEG-540).
const taskAgentWorkflow = `
schema_version: 1
name: on-the-card
title: On the card
inputs:
  task: {type: task, required: true}
steps:
 - id: implement
   title: Implement
   type: agent.run
   with: {task: "${{ workflow.inputs.task }}", prompt: Build it, agent: claude-code}
   outputs:
     pr_number: {type: integer}
`

func startTaskAgentRun(t *testing.T, yaml string, with func(*AgentRunExecutor)) (AgentService, *store.Store, *mockTmux, *WorkflowService, *store.WorkflowRunView) {
	t.Helper()
	agents, s, mt := newTestAgentService(t)
	createTask(t, s, "LEG-7")
	executor := NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))
	if with != nil {
		with(executor)
	}
	workflows := NewWorkflowService(s, []WorkflowExecutor{executor})
	def, err := workflowengine.Parse("card.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(context.Background(), def, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	return agents, s, mt, workflows, view
}

func TestAnAgentRunStepWithATaskRunsAsThatTasksOwnAgent(t *testing.T) {
	_, s, _, _, view := startTaskAgentRun(t, taskAgentWorkflow, nil)
	ctx := context.Background()
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" || attempt.TaskID != "LEG-7" {
		t.Fatalf("attempt = %+v; want it waiting on LEG-7's agent", attempt)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil {
		t.Fatalf("LEG-7 has no agent: %v", err)
	}
	if session.WorkflowRunID != view.Run.ID || session.ExecutionKind != store.ExecutionWorkflowAgent || session.AttributionTaskID != "LEG-7" {
		t.Fatalf("session = %+v", session)
	}
	// The tmux session is the card's: its agent tool's LEGATO_TASK_ID,
	// which task_context reads, is LEG-7.
	if session.TmuxSession != "legato-LEG-7" {
		t.Fatalf("tmux session = %q", session.TmuxSession)
	}
	if _, err := s.GetAgentSessionByTaskID(ctx, view.Run.TaskID); err == nil {
		t.Fatal("the run's own task has an agent too")
	}
}

func TestAMacroOnATasksOwnAgentIsSentAsTheCardWouldSendIt(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-7", Title: "Fix login", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	agents := &fakeWorkflowAgentService{}
	run := NewAgentRunExecutor(agents)
	run.SetMacros(NewMacroService(settings))
	result, err := run.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-task", StepID: "implement",
		With: map[string]any{"task": map[string]any{"id": "LEG-7"}, "macro": "Implement", "repo": "acme/web"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(agents.opts.Assignment, "Task LEG-7: Fix login\n\nImplement this task.") {
		t.Fatalf("assignment = %q", agents.opts.Assignment)
	}
	if agents.taskID != "LEG-7" || agents.opts.ExtraEnv["LEGATO_TASK_ID"] != "LEG-7" || agents.opts.AttributionTaskID != "LEG-7" || agents.opts.Repo != "acme/web" {
		t.Fatalf("spawned on %q with %+v", agents.taskID, agents.opts)
	}
	if result.TaskID != "LEG-7" {
		t.Fatalf("result task = %q", result.TaskID)
	}
}

func TestAnAgentRunStepLeavesAnAgentAPersonStartedOnTheCard(t *testing.T) {
	agents, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "LEG-7")
	if err := agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))})
	def, err := workflowengine.Parse("card.yaml", []byte(taskAgentWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, def, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || !strings.Contains(view.Attempts[0].Error, "task LEG-7 already has an agent running") {
		t.Fatalf("run = %s, attempt = %+v", view.Run.Status, view.Attempts[0])
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil || session.WorkflowRunID != "" {
		t.Fatalf("the person's agent = %+v, %v", session, err)
	}
}

func TestCompletingTheStepRemovesTheCardsAgentUnlessItIsKept(t *testing.T) {
	ctx := context.Background()
	for _, keep := range []bool{false, true} {
		yaml := taskAgentWorkflow
		if keep {
			yaml = strings.Replace(yaml, "agent: claude-code}", "agent: claude-code, keep_agent: true}", 1)
		}
		_, s, _, workflows, view := startTaskAgentRun(t, yaml, nil)
		attempt := view.Attempts[0]
		done, err := workflows.Complete(ctx, view.Run.ID, "implement", attempt.ID, map[string]string{"pr_number": "12"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if done.Run.Status != "succeeded" {
			t.Fatalf("keep=%v: run = %s", keep, done.Run.Status)
		}
		_, err = s.GetAgentSessionByTaskID(ctx, "LEG-7")
		if running := err == nil; running != keep {
			t.Fatalf("keep=%v: the card's agent running = %v", keep, running)
		}
	}
}

func TestCancellingTheRunStopsTheCardsAgent(t *testing.T) {
	ctx := context.Background()
	_, s, _, workflows, view := startTaskAgentRun(t, taskAgentWorkflow, nil)
	if _, err := workflows.Cancel(ctx, view.Run.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentSessionByTaskID(ctx, "LEG-7"); err == nil {
		t.Fatal("the card's agent still runs")
	}
}

// The card shows its agent's stage: a task's own agent keeps it at the
// task's place, not the workflow execution's.
func TestATasksOwnWorkflowAgentKeepsItsStageOnTheCard(t *testing.T) {
	ctx := context.Background()
	_, s, _, _, _ := startTaskAgentRun(t, taskAgentWorkflow, nil)
	stage, err := s.GetWorkStage(ctx, TaskPlace("LEG-7", ""))
	if err != nil || stage == nil {
		t.Fatalf("the card's stage = %+v, %v", stage, err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil {
		t.Fatal(err)
	}
	if place := SessionPlace(*session); place != TaskPlace("LEG-7", "") {
		t.Fatalf("place = %+v", place)
	}
}
