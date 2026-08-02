package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type fakeReflectionRequestCreator struct {
	input      CreateReflectionRequestInput
	calls      int
	err        error
	marked     *store.LearningReflectionRequest
	markReason string
	markErr    error
}

func (f *fakeReflectionRequestCreator) CreateReflectionRequest(_ context.Context, input CreateReflectionRequestInput) (*store.LearningReflectionRequest, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return &store.LearningReflectionRequest{ID: "lrq-1", Prompt: "Report only durable lessons from this task."}, nil
}

func (f *fakeReflectionRequestCreator) MarkReflectionUnavailable(_ context.Context, request *store.LearningReflectionRequest, reason string) error {
	f.marked, f.markReason = request, reason
	return f.markErr
}

type fakeInterviewPrompter struct {
	taskID string
	prompt string
	err    error
}

func (f *fakeInterviewPrompter) PromptAgent(_ context.Context, taskID, prompt string) error {
	f.taskID, f.prompt = taskID, prompt
	return f.err
}

func TestAgentInterviewExecutorBorrowsExistingSession(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "LEG-7", TmuxSession: "legato-LEG-7", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	requests := &fakeReflectionRequestCreator{}
	prompter := &fakeInterviewPrompter{}
	executor := NewAgentInterviewExecutor(requests, prompter, db, "/opt/legato current/legato")

	result, err := executor.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", AttemptID: "wa-1", StepID: "reflect", With: map[string]any{"task": map[string]any{"id": "LEG-7"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "waiting_agent" || result.AgentSession == nil || result.AgentSession.TaskID != "LEG-7" {
		t.Fatalf("result = %#v", result)
	}
	if requests.input.TaskID != "LEG-7" || requests.input.AgentSessionID == nil || *requests.input.AgentSessionID != result.AgentSession.ID {
		t.Fatalf("request input = %#v", requests.input)
	}
	if prompter.taskID != "LEG-7" || !strings.Contains(prompter.prompt, "lrq-1") || !strings.Contains(prompter.prompt, "learn reflection submit") || !strings.Contains(prompter.prompt, "'/opt/legato current/legato'") {
		t.Fatalf("prompt task=%q prompt=%q", prompter.taskID, prompter.prompt)
	}
	for _, guidance := range []string{"future task", "module-specific", "Do not submit a task recap"} {
		if !strings.Contains(requests.input.Prompt, guidance) {
			t.Errorf("reflection prompt missing %q: %q", guidance, requests.input.Prompt)
		}
	}
	attempt := store.WorkflowStepAttempt{TaskID: "workflow-task", AgentSessionID: &result.AgentSession.ID}
	if err := executor.Cancel(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	if err := executor.Finish(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, "LEG-7"); err != nil {
		t.Fatalf("borrowed session was cleaned up: %v", err)
	}
}

func TestLearningWorkflowFailsAtInterviewWhenTaskHasNoAgent(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-no-agent", Title: "task", Status: "Review"}); err != nil {
		t.Fatal(err)
	}
	learning := NewLearningService(db, nil)
	interview := NewAgentInterviewExecutor(learning, &fakeInterviewPrompter{}, db)
	packet := &fakeWorkflowExecutor{typeName: "learning.packet.prepare", result: WorkflowExecutionResult{Status: "succeeded"}}
	agent := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	submit := &fakeWorkflowExecutor{typeName: "learning.proposals.submit", result: WorkflowExecutionResult{Status: "succeeded"}}
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{Adapter: "chimera", PacketMaxItems: 20, PacketMaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}

	view, err := NewWorkflowService(db, []WorkflowExecutor{interview, packet, agent, submit}).Start(ctx, definition, map[string]string{
		"task": "LEG-no-agent", "project_key": "repo:acme/widget", "project_label": "acme/widget",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "failed" || view.Run.CurrentStepID != "interview" || !strings.Contains(view.Run.Error, "no active agent session") {
		t.Fatalf("run = %+v", view.Run)
	}
	if view.Steps[0].Status != "failed" || view.Steps[1].Status != "pending" || view.Steps[2].Status != "pending" || view.Steps[3].Status != "pending" || view.Steps[4].Status != "pending" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	if len(packet.starts) != 0 || len(agent.starts) != 0 || len(submit.starts) != 0 {
		t.Fatalf("downstream starts: packet=%d agent=%d submit=%d", len(packet.starts), len(agent.starts), len(submit.starts))
	}
}

func TestAgentInterviewExecutorFailsWithoutSession(t *testing.T) {
	db := newServiceTestStore(t)
	requests := &fakeReflectionRequestCreator{}
	executor := NewAgentInterviewExecutor(requests, &fakeInterviewPrompter{}, db)
	if _, err := executor.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"task": "LEG-missing"}}); err == nil || !strings.Contains(err.Error(), "no active agent session for task LEG-missing") {
		t.Fatalf("error = %v", err)
	}
	if requests.calls != 0 {
		t.Fatalf("created %d requests without a session", requests.calls)
	}
}

func TestAgentInterviewExecutorDeliveryFailureContinuesUnavailable(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "LEG-8", TmuxSession: "legato-LEG-8", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	requests := &fakeReflectionRequestCreator{}
	executor := NewAgentInterviewExecutor(requests, &fakeInterviewPrompter{err: errors.New("transport unavailable")}, db)
	result, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": "LEG-8"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Outputs["available"] != false || result.Outputs["request_id"] != "lrq-1" || !strings.Contains(result.Outputs["reason"].(string), "transport unavailable") {
		t.Fatalf("result = %#v", result)
	}
	if requests.marked == nil || requests.marked.ID != "lrq-1" || !strings.Contains(requests.markReason, "transport unavailable") {
		t.Fatalf("unavailable request=%#v reason=%q", requests.marked, requests.markReason)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, "LEG-8"); err != nil {
		t.Fatalf("borrowed session was cleaned up: %v", err)
	}
}

func TestWorkflowAdvancesAfterInterviewDeliveryUnavailable(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-10", Title: "task", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "LEG-10", TmuxSession: "legato-LEG-10", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	learning := NewLearningService(db, nil)
	interview := NewAgentInterviewExecutor(learning, &fakeInterviewPrompter{err: errors.New("transport unavailable")}, db)
	packet := &fakeWorkflowExecutor{typeName: "learning.packet.prepare", result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"prepared": true}}}
	definition, err := workflowengine.Parse("delivery.yaml", []byte(`
schema_version: 1
name: delivery
title: Delivery
inputs:
  task: {type: task, required: true}
steps:
  - id: interview
    type: agent.interview
    with: {task: "${{ workflow.inputs.task }}"}
    outputs:
      available: {type: boolean}
      reason: {type: string}
      request_id: {type: string}
  - id: packet
    type: learning.packet.prepare
    with:
      task: "${{ workflow.inputs.task }}"
      available: "${{ steps.interview.outputs.available }}"
      limits: {max_items: 1, max_bytes: 1024}
`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := NewWorkflowService(db, []WorkflowExecutor{interview, packet}).Start(ctx, definition, map[string]string{"task": "LEG-10"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.starts) != 1 || packet.starts[0].With["available"] != false {
		t.Fatalf("packet starts = %#v", packet.starts)
	}
	if view.Run.Status != "succeeded" || view.Steps[0].Status != "succeeded" || view.Steps[1].Status != "succeeded" {
		t.Fatalf("run = %#v steps = %#v", view.Run, view.Steps)
	}
	request, err := db.GetLearningReflectionRequestByAttempt(ctx, view.Attempts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != store.ReflectionRequestUnavailable || request.Error == "" {
		t.Fatalf("request = %#v", request)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, "LEG-10"); err != nil {
		t.Fatalf("borrowed session was cleaned up: %v", err)
	}
}

func TestAgentInterviewExecutorDeliveryFailureFailsIfUnavailableCannotPersist(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "LEG-9", TmuxSession: "legato-LEG-9", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	requests := &fakeReflectionRequestCreator{markErr: store.ErrConflict}
	executor := NewAgentInterviewExecutor(requests, &fakeInterviewPrompter{err: errors.New("transport unavailable")}, db)
	if _, err := executor.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": "LEG-9"}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want persistence conflict", err)
	}
}
