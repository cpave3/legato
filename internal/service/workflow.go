package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type ResolvedWorkflowStep struct {
	RunID     string
	AttemptID string
	TaskID    string
	StepID    string
	Title     string
	Type      string
	With      map[string]any
	Outputs   map[string]workflowengine.Output
}

type WorkflowExecutionResult struct {
	Status       string
	Outputs      map[string]any
	AgentSession *store.AgentSession
	WorkingDir   string
	Error        string
}

type WorkflowExecutor interface {
	Type() string
	Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error)
	Cancel(context.Context, store.WorkflowStepAttempt) error
}

type WorkflowService struct {
	store     *store.Store
	executors map[string]WorkflowExecutor
	collector *WorkflowOutputCollector
}

func NewWorkflowService(s *store.Store, executors []WorkflowExecutor) *WorkflowService {
	return NewWorkflowServiceWithOutputs(s, executors, "")
}

func NewWorkflowServiceWithOutputs(s *store.Store, executors []WorkflowExecutor, dataDir string) *WorkflowService {
	registry := make(map[string]WorkflowExecutor, len(executors))
	for _, executor := range executors {
		registry[executor.Type()] = executor
	}
	return &WorkflowService{store: s, executors: registry, collector: NewWorkflowOutputCollector(s, dataDir)}
}

func (s *WorkflowService) Start(ctx context.Context, definition *workflowengine.Definition, rawInputs map[string]string, workspaceID *int) (*store.WorkflowRunView, error) {
	inputs, err := definition.NormalizeInputs(rawInputs)
	if err != nil {
		return nil, err
	}
	for _, step := range definition.Steps {
		if _, ok := s.executors[step.Type]; !ok {
			return nil, fmt.Errorf("workflow executor %q is not registered", step.Type)
		}
	}
	runID := workflowID("wr-")
	taskID := store.GenerateTaskID()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.store.CreateTask(ctx, store.Task{ID: taskID, Title: definition.Title, Description: "Workflow run " + runID, DescriptionMD: "Workflow run " + runID, Status: "Backlog", WorkspaceID: workspaceID, CreatedAt: now, UpdatedAt: now}); err != nil {
		return nil, err
	}
	definitionJSON, _ := json.Marshal(definition)
	inputsJSON, _ := json.Marshal(inputs)
	run := store.WorkflowRun{ID: runID, WorkflowName: definition.Name, WorkflowTitle: definition.Title, DefinitionSource: definition.Source, DefinitionDigest: digestBytes(definitionJSON), DefinitionJSON: string(definitionJSON), InputsJSON: string(inputsJSON), Status: "running", CurrentStepID: definition.Steps[0].ID, TaskID: taskID}
	steps := make([]store.WorkflowStepRun, len(definition.Steps))
	for i, step := range definition.Steps {
		encoded, _ := json.Marshal(step)
		steps[i] = store.WorkflowStepRun{RunID: runID, StepID: step.ID, StepOrder: i, Title: step.Title, ExecutorType: step.Type, DefinitionJSON: string(encoded), Status: "pending"}
	}
	if err := s.store.CreateWorkflowRun(ctx, run, steps); err != nil {
		return nil, err
	}
	if err := s.advance(ctx, definition, inputs, runID, taskID, 0, map[string]map[string]any{}); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Show(ctx context.Context, runID string) (*store.WorkflowRunView, error) {
	return s.store.GetWorkflowRun(ctx, runID)
}
func (s *WorkflowService) ListRuns(ctx context.Context, status string, limit int) ([]store.WorkflowRun, error) {
	return s.store.ListWorkflowRuns(ctx, status, limit)
}

func (s *WorkflowService) Fail(ctx context.Context, runID, stepID, attemptID, reason string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	var active *store.WorkflowStepAttempt
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.ID == attemptID && attempt.StepID == stepID && (attempt.Status == "running" || attempt.Status == "waiting_agent") {
			active = attempt
			break
		}
	}
	if active == nil {
		return nil, store.ErrWorkflowStateConflict
	}
	if executor := s.executors[active.ExecutorType]; executor != nil {
		if err := executor.Cancel(ctx, *active); err != nil {
			return nil, fmt.Errorf("stopping failed workflow agent: %w", err)
		}
	}
	if err := s.store.FailWorkflowStep(ctx, runID, stepID, attemptID, reason); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Retry(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if err := s.store.ResetWorkflowStepForRetry(ctx, runID, stepID); err != nil {
		return nil, err
	}
	if err := s.advance(ctx, definition, inputs, runID, view.Run.TaskID, index, prior); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Cancel(ctx context.Context, runID, reason string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.Status == "running" || attempt.Status == "waiting_agent" {
			if executor := s.executors[attempt.ExecutorType]; executor != nil {
				if err := executor.Cancel(ctx, attempt); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := s.store.CancelWorkflowRun(ctx, runID, reason); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Complete(ctx context.Context, runID, stepID, attemptID string, values, artifacts map[string]string) (*store.WorkflowRunView, error) {
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	step := definition.Steps[index]
	var attempt *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if view.Attempts[i].ID == attemptID {
			attempt = &view.Attempts[i]
			break
		}
	}
	if attempt == nil || attempt.StepID != stepID || attempt.Status != "waiting_agent" {
		return nil, store.ErrWorkflowStateConflict
	}
	outputs, err := s.collector.Collect(ctx, WorkflowOutputRequest{RunID: runID, StepID: stepID, AttemptID: attemptID, TaskID: view.Run.TaskID, WorkingDir: attempt.WorkingDir, Declared: step.Outputs, Values: values, Artifacts: artifacts})
	if err != nil {
		return nil, err
	}
	if executor := s.executors[attempt.ExecutorType]; executor != nil {
		if err := executor.Cancel(ctx, *attempt); err != nil {
			return nil, fmt.Errorf("stopping completed workflow agent: %w", err)
		}
	}
	encoded, _ := json.Marshal(outputs)
	next := ""
	if index+1 < len(definition.Steps) {
		next = definition.Steps[index+1].ID
	}
	if err := s.store.CompleteWorkflowStep(ctx, runID, stepID, attemptID, string(encoded), next); err != nil {
		return nil, err
	}
	prior[stepID] = outputs
	if next != "" {
		if err := s.advance(ctx, definition, inputs, runID, view.Run.TaskID, index+1, prior); err != nil {
			return nil, err
		}
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) loadExecution(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, *workflowengine.Definition, map[string]any, map[string]map[string]any, int, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, nil, nil, nil, 0, err
	}
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(view.Run.DefinitionJSON), &definition); err != nil {
		return nil, nil, nil, nil, 0, err
	}
	var inputs map[string]any
	if err := json.Unmarshal([]byte(view.Run.InputsJSON), &inputs); err != nil {
		return nil, nil, nil, nil, 0, err
	}
	prior := map[string]map[string]any{}
	index := -1
	for i, step := range definition.Steps {
		if step.ID == stepID {
			index = i
			break
		}
		if i < len(view.Steps) && view.Steps[i].Status == "succeeded" {
			var values map[string]any
			if err := json.Unmarshal([]byte(view.Steps[i].OutputsJSON), &values); err != nil {
				return nil, nil, nil, nil, 0, err
			}
			prior[step.ID] = values
		}
	}
	if index < 0 {
		return nil, nil, nil, nil, 0, fmt.Errorf("workflow step %q not found", stepID)
	}
	return view, &definition, inputs, prior, index, nil
}

func (s *WorkflowService) advance(ctx context.Context, definition *workflowengine.Definition, inputs map[string]any, runID, taskID string, index int, prior map[string]map[string]any) error {
	for index < len(definition.Steps) {
		step := definition.Steps[index]
		resolved, err := definition.ResolveStep(step.ID, inputs, prior)
		if err != nil {
			return err
		}
		attemptID := workflowID("wa-")
		resolvedJSON, _ := json.Marshal(resolved.With)
		view, err := s.store.GetWorkflowRun(ctx, runID)
		if err != nil {
			return err
		}
		attemptNumber := 1
		for _, persisted := range view.Steps {
			if persisted.StepID == step.ID {
				attemptNumber = persisted.LatestAttempt + 1
				break
			}
		}
		attempt := store.WorkflowStepAttempt{ID: attemptID, RunID: runID, StepID: step.ID, AttemptNumber: attemptNumber, ExecutorType: step.Type, TaskID: taskID, Status: "running"}
		if err := s.store.StartWorkflowStepAttempt(ctx, attempt, string(resolvedJSON)); err != nil {
			return err
		}
		result, execErr := s.executors[step.Type].Start(ctx, ResolvedWorkflowStep{RunID: runID, AttemptID: attemptID, TaskID: taskID, StepID: step.ID, Title: step.Title, Type: step.Type, With: resolved.With, Outputs: step.Outputs})
		if execErr != nil {
			_ = s.store.FailWorkflowStep(ctx, runID, step.ID, attemptID, execErr.Error())
			return execErr
		}
		if result.Status == "waiting_agent" {
			return s.store.MarkWorkflowStepWaiting(ctx, runID, step.ID, attemptID, result.AgentSession, result.WorkingDir)
		}
		if result.Status != "succeeded" {
			return fmt.Errorf("executor %q returned invalid status %q", step.Type, result.Status)
		}
		outputsJSON, _ := json.Marshal(result.Outputs)
		next := ""
		if index+1 < len(definition.Steps) {
			next = definition.Steps[index+1].ID
		}
		if err := s.store.CompleteWorkflowStep(ctx, runID, step.ID, attemptID, string(outputsJSON), next); err != nil {
			return err
		}
		prior[step.ID] = result.Outputs
		index++
	}
	return nil
}

func workflowID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}
