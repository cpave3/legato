package service

import (
	"context"
	"errors"
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
	optional, _ := step.With["optional"].(bool)
	var taskID string
	switch task := step.With["task"].(type) {
	case map[string]any:
		taskID, _ = task["id"].(string)
	case string:
		// A task ID, such as a step's linked_task output.
		taskID = strings.TrimSpace(task)
	case nil:
	default:
		return WorkflowExecutionResult{}, fmt.Errorf("task must be a task input or a task ID")
	}
	if taskID == "" {
		// optional: true makes "no task" a skip, for a note on a task that
		// may not exist, such as the board task a pull request belongs to.
		if optional {
			return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"note_id": "", "task_id": "", "skipped": true}}, nil
		}
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
	outputs := map[string]any{"note_id": note.ID, "task_id": taskID}
	// A note on a task away on a worker stays here, where it is when the
	// task returns; it is not sent on, so the worker's agent works without
	// it. The step says so rather than pretend it reached the work.
	var away *TaskAwayError
	if err := RefuseAway(ctx, e.store, taskID); errors.As(err, &away) {
		outputs["away_on"] = away.Worker
		outputs["warning"] = fmt.Sprintf("the task is away on %s: the note is kept here, and the worker's agent won't see it", away.Worker)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: outputs}, nil
}
