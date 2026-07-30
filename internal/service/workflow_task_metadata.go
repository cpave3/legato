package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

type TaskMetadataExecutor struct{ store *store.Store }

func NewTaskMetadataExecutor(s *store.Store) *TaskMetadataExecutor {
	return &TaskMetadataExecutor{store: s}
}
func (e *TaskMetadataExecutor) Type() string                                            { return "task.metadata.set" }
func (e *TaskMetadataExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *TaskMetadataExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	task, ok := step.With["task"].(map[string]any)
	if !ok {
		return WorkflowExecutionResult{}, fmt.Errorf("task must be a task input")
	}
	taskID, _ := task["id"].(string)
	values, ok := step.With["values"].(map[string]any)
	if !ok || len(values) == 0 {
		return WorkflowExecutionResult{}, fmt.Errorf("values must be a non-empty object")
	}
	for path, value := range values {
		parts, err := store.ValidateTaskMetadataPath(path)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		if err := e.store.SetTaskMetadata(ctx, taskID, parts, value); err != nil {
			return WorkflowExecutionResult{}, err
		}
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
}
