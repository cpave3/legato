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
	relatedTaskID := ""
	for key, spec := range definition.Inputs {
		if spec.Type != "task" {
			continue
		}
		id, _ := inputs[key].(string)
		task, taskErr := s.store.GetTask(ctx, id)
		if taskErr != nil {
			return nil, fmt.Errorf("workflow input %q: task %q not found: %w", key, id, taskErr)
		}
		inputs[key] = workflowTaskContext(task)
		if relatedTaskID == "" {
			relatedTaskID = task.ID
		}
	}
	for _, step := range definition.Steps {
		if _, ok := s.executors[step.Type]; !ok {
			return nil, fmt.Errorf("workflow executor %q is not registered", step.Type)
		}
	}
	if workspaceID == nil {
		color := "#6366f1"
		id, workspaceErr := s.store.EnsureWorkspace(ctx, store.Workspace{Name: "Workflows", Color: &color, SortOrder: 1000})
		if workspaceErr != nil {
			return nil, fmt.Errorf("ensure workflow workspace: %w", workspaceErr)
		}
		workspaceID = &id
	}
	runID := workflowID("wr-")
	taskID := store.GenerateTaskID()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.store.CreateTask(ctx, store.Task{ID: taskID, Title: definition.Title, Description: "Workflow run " + runID, DescriptionMD: "Workflow run " + runID, Status: "Backlog", WorkspaceID: workspaceID, CreatedAt: now, UpdatedAt: now}); err != nil {
		return nil, err
	}
	definitionJSON, _ := json.Marshal(definition)
	inputsJSON, _ := json.Marshal(inputs)
	run := store.WorkflowRun{ID: runID, WorkflowName: definition.Name, WorkflowTitle: definition.Title, DefinitionSource: definition.Source, DefinitionDigest: digestBytes(definitionJSON), DefinitionJSON: string(definitionJSON), InputsJSON: string(inputsJSON), Status: "running", CurrentStepID: definition.Steps[0].ID, TaskID: taskID, RelatedTaskID: relatedTaskID}
	steps := make([]store.WorkflowStepRun, len(definition.Steps))
	for i, step := range definition.Steps {
		encoded, _ := json.Marshal(step)
		steps[i] = store.WorkflowStepRun{RunID: runID, StepID: step.ID, StepOrder: i, Title: step.Title, ExecutorType: step.Type, DefinitionJSON: string(encoded), Status: "pending"}
	}
	if err := s.store.CreateWorkflowRun(ctx, run, steps); err != nil {
		return nil, err
	}
	if err := s.advance(ctx, definition, inputs, runID, taskID, 0, map[string]map[string]any{}); err != nil {
		// The run is already durable and the executor records its failure.
		// Return that visible run instead of encouraging an accidental duplicate launch.
		view, loadErr := s.store.GetWorkflowRun(ctx, runID)
		if loadErr != nil {
			return nil, err
		}
		return view, nil
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func workflowTaskContext(task *store.Task) map[string]any {
	provider, remoteID := "", ""
	if task.Provider != nil {
		provider = *task.Provider
	}
	if task.RemoteID != nil {
		remoteID = *task.RemoteID
	}
	description := task.DescriptionMD
	if description == "" {
		description = task.Description
	}
	return map[string]any{"id": task.ID, "title": task.Title, "description": description, "status": task.Status, "priority": task.Priority, "provider": provider, "remote_id": remoteID}
}

func (s *WorkflowService) Artifact(ctx context.Context, artifactID string) (*store.WorkflowArtifact, error) {
	return s.store.GetWorkflowArtifact(ctx, artifactID)
}

func (s *WorkflowService) Show(ctx context.Context, runID string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	s.enrichWorkflowAttention(ctx, view)
	return view, nil
}

func (s *WorkflowService) enrichWorkflowAttention(ctx context.Context, view *store.WorkflowRunView) {
	for _, step := range view.Steps {
		if step.Status == "waiting_manual" {
			view.Run.NeedsAttention = true
			view.Run.AttentionReason = "manual action required: " + step.Title
			break
		}
	}
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.ExecutorType != "agent.run" || attempt.Status != "waiting_agent" {
			continue
		}
		session, err := s.store.GetAgentSessionByTaskID(ctx, attempt.TaskID)
		if err != nil {
			attempt.NeedsAttention = true
			attempt.AttentionReason = "agent session ended before step completion"
			continue
		}
		attempt.AgentActivity = session.Activity
		if session.Activity != "working" {
			attempt.NeedsAttention = true
			if session.Activity == "waiting" {
				attempt.AttentionReason = "agent waiting for input"
			} else {
				attempt.AttentionReason = "agent idle before step completion"
			}
		}
	}
}
func (s *WorkflowService) ListRuns(ctx context.Context, status string, limit int) ([]store.WorkflowRun, error) {
	runs, err := s.store.ListWorkflowRuns(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].Status != "running" && runs[i].Status != "waiting" {
			continue
		}
		view, viewErr := s.store.GetWorkflowRun(ctx, runs[i].ID)
		if viewErr != nil {
			continue
		}
		s.enrichWorkflowAttention(ctx, view)
		if view.Run.NeedsAttention {
			runs[i].NeedsAttention = true
			runs[i].AttentionReason = view.Run.AttentionReason
			continue
		}
		for _, attempt := range view.Attempts {
			if attempt.NeedsAttention {
				runs[i].NeedsAttention = true
				runs[i].AttentionReason = attempt.AttentionReason
				break
			}
		}
	}
	return runs, nil
}

func (s *WorkflowService) TriggerStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	view, definition, inputs, prior, index, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if view.Run.Status != "waiting" || view.Run.CurrentStepID != stepID || definition.Steps[index].Trigger != "manual" || view.Steps[index].Status != "waiting_manual" {
		return nil, store.ErrWorkflowStateConflict
	}
	if err := s.store.TriggerWorkflowStep(ctx, runID, stepID); err != nil {
		return nil, err
	}
	if err := s.advance(ctx, definition, inputs, runID, view.Run.TaskID, index, prior, stepID); err != nil {
		updated, loadErr := s.store.GetWorkflowRun(ctx, runID)
		if loadErr != nil {
			return nil, err
		}
		return updated, nil
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) PromptStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	view, err := s.Show(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.StepID != stepID || !attempt.NeedsAttention || attempt.Status != "waiting_agent" {
			continue
		}
		executor := s.executors[attempt.ExecutorType]
		prompter, ok := executor.(interface {
			Prompt(context.Context, store.WorkflowStepAttempt, string) error
		})
		if !ok {
			return nil, fmt.Errorf("workflow step %q cannot be prompted", stepID)
		}
		message := "Continue your workflow assignment now. The step is not complete; finish the declared outputs and run the exact completion command from your assignment."
		if err := prompter.Prompt(ctx, attempt, message); err != nil {
			return nil, err
		}
		return s.Show(ctx, runID)
	}
	return nil, fmt.Errorf("workflow step %q does not currently need attention", stepID)
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
	completed, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if executor, ok := s.executors[attempt.ExecutorType].(interface {
		Finish(context.Context, store.WorkflowStepAttempt) error
	}); ok {
		_ = executor.Finish(ctx, *attempt)
	}
	return completed, nil
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

func (s *WorkflowService) advance(ctx context.Context, definition *workflowengine.Definition, inputs map[string]any, runID, taskID string, index int, prior map[string]map[string]any, triggeredStep ...string) error {
	for index < len(definition.Steps) {
		step := definition.Steps[index]
		isTriggered := len(triggeredStep) > 0 && triggeredStep[0] == step.ID
		resolved, err := definition.ResolveStep(step.ID, inputs, prior)
		if err != nil {
			return err
		}
		if step.Trigger == "manual" && !isTriggered {
			resolvedJSON, _ := json.Marshal(resolved.With)
			if err := s.store.WaitWorkflowStepManual(ctx, runID, step.ID, string(resolvedJSON)); err != nil {
				return err
			}
			return nil
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
