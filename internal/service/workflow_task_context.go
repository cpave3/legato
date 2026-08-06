package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type workflowTaskContextExporter interface {
	ExportCardContext(context.Context, string, ExportFormat) (string, error)
}

type TaskContextExecutor struct {
	board workflowTaskContextExporter
}

func NewTaskContextExecutor(board workflowTaskContextExporter) *TaskContextExecutor {
	return &TaskContextExecutor{board: board}
}

func (e *TaskContextExecutor) Type() string                                            { return "task.context" }
func (e *TaskContextExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
func (e *TaskContextExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := workflowTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	content, err := e.board.ExportCardContext(ctx, taskID, ExportFormatFull)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("export task context: %w", err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"content": content}}, nil
}

type TaskWorkingDirResolveExecutor struct {
	store *store.Store
}

func NewTaskWorkingDirResolveExecutor(s *store.Store) *TaskWorkingDirResolveExecutor {
	return &TaskWorkingDirResolveExecutor{store: s}
}

func (e *TaskWorkingDirResolveExecutor) Type() string { return "task.working_dir.resolve" }
func (e *TaskWorkingDirResolveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (e *TaskWorkingDirResolveExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := workflowTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	fallback, err := workflowString(step.With, "fallback", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	task, err := e.store.GetTask(ctx, taskID)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("load task working directory: %w", err)
	}
	path, source := fallback, "repository"
	if task.WorktreePath != nil && strings.TrimSpace(*task.WorktreePath) != "" {
		path, source = *task.WorktreePath, "worktree"
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"path": path, "source": source}}, nil
}

func workflowTaskID(value any) (string, error) {
	task, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("task must be a task input")
	}
	taskID, _ := task["id"].(string)
	if taskID == "" {
		return "", fmt.Errorf("task id is required")
	}
	return taskID, nil
}
