package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

type WorkflowAttentionSource interface {
	ListRuns(ctx context.Context, status string, limit int) ([]store.WorkflowRun, error)
	Show(ctx context.Context, runID string) (*store.WorkflowRunView, error)
	TriggerStep(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error)
	Retry(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error)
	PromptStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error)
}

type WorkflowAttentionAdapter struct {
	workflows WorkflowAttentionSource
}

func NewWorkflowAttentionAdapter(workflows WorkflowAttentionSource) *WorkflowAttentionAdapter {
	return &WorkflowAttentionAdapter{workflows: workflows}
}

func (a *WorkflowAttentionAdapter) Producer() string { return "workflows" }

type workflowAttentionState struct {
	runID    string
	stepID   string
	kind     string // manual | failed | waiting_agent
	revision string
	runTitle string
	taskID   string
	reason   string
	action   AttentionAction
}

func (a *WorkflowAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	runs, err := a.workflows.ListRuns(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	var states []workflowAttentionState
	for _, run := range runs {
		if run.NeedsAttention || run.Status == "failed" {
			view, err := a.workflows.Show(ctx, run.ID)
			if err != nil {
				return nil, fmt.Errorf("loading workflow run %s for attention: %w", run.ID, err)
			}
			states = append(states, a.collectRunStates(run, view)...)
		}
	}
	items := make([]AttentionInput, 0, len(states))
	for _, s := range states {
		signalKind := AttentionSignalWorkflowWaitingAgent
		urgency := "normal"
		switch s.kind {
		case "manual":
			signalKind = AttentionSignalWorkflowManualGate
		case "failed":
			signalKind = AttentionSignalWorkflowFailed
			urgency = "high"
		}
		items = append(items, AttentionInput{
			SignalKind: signalKind,
			SourceKey:  fmt.Sprintf("%s:%s", s.runID, s.stepID), SourceRevision: s.revision,
			TaskID: s.taskID, Title: s.runTitle, Reason: s.reason,
			Category: "workflow", Urgency: urgency,
			Navigation: AttentionNavigation{Kind: "workflow_run", Target: s.runID},
			Actions: []AttentionAction{
				{ID: "open", Label: "Open run", Kind: "navigate", Intent: "primary"},
				s.action,
			},
		})
	}
	return items, nil
}

func (a *WorkflowAttentionAdapter) collectRunStates(run store.WorkflowRun, view *store.WorkflowRunView) []workflowAttentionState {
	var states []workflowAttentionState
	for _, step := range view.Steps {
		if step.Status == "waiting_manual" {
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: step.StepID, kind: "manual",
				revision: fmt.Sprintf("manual:%s:%s", step.StepID, step.Status),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: "Manual action required: " + step.Title,
				action: AttentionAction{ID: "trigger", Label: "Trigger", Kind: "execute", RequiresConfirm: true},
			})
		}
		if step.Status == "failed" {
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: step.StepID, kind: "failed",
				revision: fmt.Sprintf("failed:%s:%s", step.StepID, step.Status),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: "Step failed: " + step.Title,
				action: AttentionAction{ID: "retry", Label: "Retry", Kind: "execute", RequiresConfirm: true},
			})
		}
	}
	for _, attempt := range view.Attempts {
		if !attempt.NeedsAttention || attempt.Status != "waiting_agent" {
			continue
		}
		states = append(states, workflowAttentionState{
			runID: run.ID, stepID: attempt.StepID, kind: "waiting_agent",
			revision: fmt.Sprintf("agent:%s:%s:%d", attempt.StepID, attempt.Status, attempt.AttemptNumber),
			runTitle: run.WorkflowTitle, taskID: run.TaskID,
			reason: attempt.AttentionReason,
			action: AttentionAction{ID: "prompt", Label: "Prompt to continue", Kind: "execute"},
		})
	}
	return states
}

func (a *WorkflowAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	runID, stepID, ok := splitWorkflowSourceKey(request.SourceKey)
	if !ok {
		return fmt.Errorf("invalid workflow attention source key %q", request.SourceKey)
	}
	switch request.ActionID {
	case "trigger":
		_, err := a.workflows.TriggerStep(ctx, runID, stepID, request.Confirmed)
		return err
	case "retry":
		_, err := a.workflows.Retry(ctx, runID, stepID, false)
		return err
	case "prompt":
		_, err := a.workflows.PromptStep(ctx, runID, stepID)
		return err
	default:
		return fmt.Errorf("unsupported workflow attention action %q", request.ActionID)
	}
}

// Ensure unused import is referenced

func splitWorkflowSourceKey(sourceKey string) (runID, stepID string, ok bool) {
	for i := 0; i < len(sourceKey); i++ {
		if sourceKey[i] == ':' {
			return sourceKey[:i], sourceKey[i+1:], true
		}
	}
	return "", "", false
}
