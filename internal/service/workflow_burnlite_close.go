package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/store"
)

// BurnliteCloseStepType closes a task's Burnlite ticket (LEG-540).
const BurnliteCloseStepType = "burnlite.task.close"

// BurnliteTaskCloser closes a Burnlite Task by its key.
type BurnliteTaskCloser interface {
	CloseTask(ctx context.Context, key, reason, summary string) error
}

// ConnectedBurnlite closes Tasks in the Burnlite Legato is connected to,
// with the connection the board shares with agents (LEG-544).
type ConnectedBurnlite struct {
	Connection BurnliteConnection
	// HTTP, when set, replaces the connection's client (tests).
	HTTP *http.Client
}

func (c ConnectedBurnlite) CloseTask(ctx context.Context, key, reason, summary string) error {
	client, _, err := openBurnlite(ctx, c.Connection, c.HTTP)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.CloseTask(ctx, key, reason, summary)
}

// BurnliteCloseExecutor is burnlite.task.close.
type BurnliteCloseExecutor struct {
	store  *store.Store
	closer BurnliteTaskCloser
}

func NewBurnliteCloseExecutor(db *store.Store, closer BurnliteTaskCloser) *BurnliteCloseExecutor {
	return &BurnliteCloseExecutor{store: db, closer: closer}
}

func (e *BurnliteCloseExecutor) Type() string { return BurnliteCloseStepType }

func (e *BurnliteCloseExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *BurnliteCloseExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	taskID, err := interviewTaskID(step.With["task"])
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	summary, err := workflowString(step.With, "summary", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	summary = strings.Join(strings.Fields(summary), " ")
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:199]) + "…"
	}
	reason, _ := workflowString(step.With, "reason", false)
	switch reason = strings.TrimSpace(reason); reason {
	case "":
		reason = burnlite.CloseCompleted
	case burnlite.CloseCompleted, burnlite.CloseNotPlanned:
	default:
		return WorkflowExecutionResult{}, fmt.Errorf("reason must be completed or not_planned, not %q", reason)
	}
	task, err := e.store.GetTask(ctx, taskID)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("task %s: %w", taskID, err)
	}
	if task.Provider == nil || *task.Provider != BurnliteIntegration || task.RemoteID == nil || *task.RemoteID == "" {
		if optional, _ := step.With["optional"].(bool); optional {
			return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"key": "", "closed": false, "skipped": true}}, nil
		}
		return WorkflowExecutionResult{}, fmt.Errorf("task %s is not a Burnlite ticket", taskID)
	}
	key := *task.RemoteID
	if e.closer == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("closing %s: Burnlite is not available here", key)
	}
	if err := e.closer.CloseTask(ctx, key, reason, summary); err != nil {
		if errors.Is(err, burnlite.ErrInsufficientScope) {
			return WorkflowExecutionResult{}, fmt.Errorf("closing %s: Legato's Burnlite connection may only read; connect Legato to Burnlite again (Settings › Integrations, or legato integration connect burnlite) to let it close tickets: %w", key, err)
		}
		return WorkflowExecutionResult{}, fmt.Errorf("closing %s: %w", key, err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"key": key, "closed": true, "skipped": false}}, nil
}
