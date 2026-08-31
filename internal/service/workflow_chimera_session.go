package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type ChimeraSessionRemover interface {
	Remove(context.Context, string) (bool, error)
}

type ChimeraCLISessionRemover struct {
	binary string
	run    func(context.Context, string, ...string) ([]byte, error)
}

func NewChimeraCLISessionRemover(binary string) *ChimeraCLISessionRemover {
	return &ChimeraCLISessionRemover{
		binary: binary,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

func (r *ChimeraCLISessionRemover) Remove(ctx context.Context, sessionID string) (bool, error) {
	output, err := r.run(ctx, r.binary, "sessions", "rm", sessionID, "--exact", "--if-exists", "--json")
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return false, fmt.Errorf("chimera sessions rm: %s", message)
	}
	var result struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return false, fmt.Errorf("decode chimera sessions rm output: %w", err)
	}
	return result.Removed, nil
}

type ChimeraSessionRemoveExecutor struct {
	store   *store.Store
	remover ChimeraSessionRemover
}

func NewChimeraSessionRemoveExecutor(s *store.Store, remover ChimeraSessionRemover) *ChimeraSessionRemoveExecutor {
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
	task, err := e.store.GetTask(ctx, taskID)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if task.ChimeraSessionID == nil || *task.ChimeraSessionID == "" {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": false, "metadata_cleared": false}}, nil
	}
	sessionID := *task.ChimeraSessionID
	if _, err := e.store.GetAgentSessionByTaskID(ctx, taskID); err == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("task %s has a running agent; use agent.stop before chimera.session.remove", taskID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return WorkflowExecutionResult{}, err
	}
	removed, err := e.remover.Remove(ctx, sessionID)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("removing Chimera session: %w", err)
	}
	cleared, err := e.store.ClearTaskChimeraSession(ctx, taskID, sessionID)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("clearing Chimera session metadata: %w", err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": removed, "metadata_cleared": cleared}}, nil
}
