package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// CreateReflectionRequestInput is the request-creation contract consumed by
// the interview executor and implemented by the learning service.
type CreateReflectionRequestInput struct {
	WorkflowRunID  string `json:"workflow_run_id"`
	StepID         string `json:"step_id"`
	AttemptID      string `json:"attempt_id"`
	TaskID         string `json:"task_id"`
	AgentSessionID *int   `json:"agent_session_id,omitempty"`
	Prompt         string `json:"prompt"`
}

// ReflectionRequestManager keeps request persistence behind the learning
// service boundary rather than coupling this executor to learning storage.
type ReflectionRequestManager interface {
	CreateReflectionRequest(context.Context, CreateReflectionRequestInput) (*store.LearningReflectionRequest, error)
	MarkReflectionUnavailable(context.Context, *store.LearningReflectionRequest, string) error
}

type interviewAgentPrompter interface {
	PromptAgent(context.Context, string, string) error
}

const durableReflectionPrompt = `Reflect on this task and report only lessons that would change a decision on a future task. Extract reusable user preferences, repository-wide conventions, module-specific rules, technical invariants, repeatable process improvements, and recurring pitfalls. State the applicability boundary: user-global, repository-wide, or the named module/subsystem. Prefer the underlying rule and why it matters over the task-specific symptom, file, identifier, or fix. Do not submit a task recap, completed-work summary, transcript, secret, or implementation detail useful only for this task. Omit an observation when no plausible future situation would benefit from it.`

// AgentInterviewExecutor asks an already-running task agent for a reflection.
// The session is borrowed: this executor never owns or cleans it up.
type AgentInterviewExecutor struct {
	requests   ReflectionRequestManager
	agents     interviewAgentPrompter
	store      *store.Store
	binaryPath string
}

func NewAgentInterviewExecutor(requests ReflectionRequestManager, agents interviewAgentPrompter, s *store.Store, binaryPath ...string) *AgentInterviewExecutor {
	executor := &AgentInterviewExecutor{requests: requests, agents: agents, store: s}
	if len(binaryPath) > 0 {
		executor.binaryPath = binaryPath[0]
	}
	return executor
}

func (e *AgentInterviewExecutor) Type() string { return "agent.interview" }

func (e *AgentInterviewExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := interviewTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	session, err := e.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
				"available": false,
				"reason":    "no active agent session for task " + taskID,
			}}, nil
		}
		return WorkflowExecutionResult{}, fmt.Errorf("find interview session: %w", err)
	}
	prompt := durableReflectionPrompt
	if configured, promptErr := workflowString(step.With, "prompt", false); promptErr != nil {
		return WorkflowExecutionResult{}, promptErr
	} else if configured != "" {
		prompt = configured
	}
	sessionID := session.ID
	request, err := e.requests.CreateReflectionRequest(ctx, CreateReflectionRequestInput{
		WorkflowRunID: step.RunID, StepID: step.StepID, AttemptID: step.AttemptID,
		TaskID: taskID, AgentSessionID: &sessionID, Prompt: prompt,
	})
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("create reflection request: %w", err)
	}
	if request == nil || request.ID == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("create reflection request: request ID is required")
	}
	if err := e.agents.PromptAgent(ctx, taskID, buildInterviewPrompt(request, e.binaryPath)); err != nil {
		reason := fmt.Sprintf("deliver reflection request: %v", err)
		if markErr := e.requests.MarkReflectionUnavailable(ctx, request, reason); markErr != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("mark reflection request %s unavailable after delivery failure: %w", request.ID, markErr)
		}
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
			"available": false, "reason": reason, "request_id": request.ID,
		}}, nil
	}
	return WorkflowExecutionResult{
		Status:       "waiting_agent",
		AgentSession: session,
		Outputs:      map[string]any{"available": true, "request_id": request.ID},
	}, nil
}

func (e *AgentInterviewExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *AgentInterviewExecutor) Finish(context.Context, store.WorkflowStepAttempt) error { return nil }

func interviewTaskID(value any) (string, error) {
	switch task := value.(type) {
	case string:
		if task != "" {
			return task, nil
		}
	case map[string]any:
		id, _ := task["id"].(string)
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("task must be a task input or task ID")
}

func buildInterviewPrompt(request *store.LearningReflectionRequest, binaryPath string) string {
	command := "legato"
	if binaryPath != "" {
		command = shellWorkflowCommand(binaryPath)
	}
	return "## Legato reflection request (schema version 1)\n" +
		"Request ID: " + request.ID + "\n\n" + request.Prompt + "\n\n" +
		"Submit the bounded structured reflection for this exact request with `" + command +
		" learn reflection submit --request " + request.ID + "`. " +
		"A conversational reply or terminal output does not submit the reflection."
}
