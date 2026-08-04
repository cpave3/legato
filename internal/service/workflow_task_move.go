package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type WorkflowTaskMover interface {
	MoveTask(context.Context, string, string) error
}

type workflowTaskMover struct {
	board BoardService
	sync  CardMoveSync
}

func NewWorkflowTaskMover(board BoardService, syncService CardMoveSync) WorkflowTaskMover {
	return &workflowTaskMover{board: board, sync: syncService}
}

func (m *workflowTaskMover) MoveTask(ctx context.Context, taskID, column string) error {
	if m.sync != nil {
		return m.sync.PushMove(ctx, taskID, column)
	}
	return m.board.MoveCard(ctx, taskID, column)
}

type TaskMoveExecutor struct{ mover WorkflowTaskMover }

func NewTaskMoveExecutor(mover WorkflowTaskMover) *TaskMoveExecutor {
	return &TaskMoveExecutor{mover: mover}
}

func (e *TaskMoveExecutor) Type() string { return "task.move" }
func (e *TaskMoveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}
func (e *TaskMoveExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	task, ok := step.With["task"].(map[string]any)
	if !ok {
		return WorkflowExecutionResult{}, fmt.Errorf("task must be a task input")
	}
	taskID, _ := task["id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("task id is required")
	}
	column, ok := step.With["column"].(string)
	if !ok || strings.TrimSpace(column) == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("column must be a non-empty string")
	}
	if err := e.mover.MoveTask(ctx, taskID, column); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{}}, nil
}
