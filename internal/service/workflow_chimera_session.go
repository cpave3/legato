package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type chimeraSessionRemover interface {
	Remove(context.Context, string) (bool, error)
}

type ChimeraSessionRemoveExecutor struct {
	store   *store.Store
	remover chimeraSessionRemover
}

func NewChimeraSessionRemoveExecutor(s *store.Store, remover chimeraSessionRemover) *ChimeraSessionRemoveExecutor {
	return &ChimeraSessionRemoveExecutor{store: s, remover: remover}
}

func (e *ChimeraSessionRemoveExecutor) Type() string { return "chimera.session.remove" }
func (e *ChimeraSessionRemoveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func (e *ChimeraSessionRemoveExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskInput, ok := step.With["task"].(map[string]any)
	if !ok {
		return WorkflowExecutionResult{}, fmt.Errorf("task must be a task input")
	}
	taskID, _ := taskInput["id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("task id is required")
	}
	sessionID, err := e.store.ReserveChimeraCleanup(ctx, taskID, step.RunID, step.StepID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if sessionID == "" {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": false, "metadata_cleared": false}}, nil
	}
	removed, err := e.remover.Remove(ctx, sessionID)
	if err != nil {
		if releaseErr := e.store.ReleaseChimeraCleanup(ctx, taskID, step.RunID, step.StepID, sessionID); releaseErr != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("removing Chimera session: %w (also failed to release cleanup reservation: %v)", err, releaseErr)
		}
		return WorkflowExecutionResult{}, fmt.Errorf("removing Chimera session: %w", err)
	}
	cleared, err := e.store.CompleteChimeraCleanup(ctx, taskID, sessionID, step.RunID, step.StepID)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("clearing Chimera session metadata: %w", err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": removed, "metadata_cleared": cleared}}, nil
}
