package service

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type TaskNoteExecutor struct {
	store *store.Store
}

func NewTaskNoteExecutor(s *store.Store) *TaskNoteExecutor {
	return &TaskNoteExecutor{store: s}
}
func (e *TaskNoteExecutor) Type() string                                            { return "task.note" }
func (e *TaskNoteExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *TaskNoteExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskValue, ok := step.With["task"].(map[string]any)
	if !ok {
		return WorkflowExecutionResult{}, fmt.Errorf("task must be a task input")
	}
	taskID, _ := taskValue["id"].(string)
	if taskID == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("task id is required")
	}
	body, _ := workflowString(step.With, "body", false)
	if path, _ := workflowString(step.With, "body_path", false); path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("read note body: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > maxWorkflowOutputBytes {
			return WorkflowExecutionResult{}, fmt.Errorf("note body must be a regular file under %d bytes", maxWorkflowOutputBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("read note body: %w", err)
		}
		body = string(data)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("body or body_path is required")
	}
	note, err := AddTaskNote(ctx, e.store, taskID, body, "workflow")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"note_id": note.ID, "task_id": taskID}}, nil
}
