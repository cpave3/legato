package service

import (
	"context"
	"encoding/json"
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
	kind     string // manual | failed | waiting_agent | completed
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
		// Active runs with needs_attention (failed/manual/waiting_agent)
		if run.NeedsAttention {
			view, err := a.workflows.Show(ctx, run.ID)
			if err != nil {
				continue
			}
			states = append(states, a.collectRunStates(run, view)...)
			continue
		}
		// Completed runs with notify_on_complete in their definition
		if run.Status == "succeeded" || run.Status == "failed" {
			if hasNotifyOnComplete(run.DefinitionJSON) {
				states = append(states, workflowAttentionState{
					runID:    run.ID,
					stepID:   "_complete",
					kind:     "completed",
					revision: fmt.Sprintf("completed:%s:%s", run.ID, run.Status),
					runTitle: run.WorkflowTitle,
					taskID:   run.TaskID,
					reason:   "Workflow " + run.Status + ": " + run.WorkflowTitle,
					action:   AttentionAction{ID: "dismiss", Label: "Dismiss", Kind: "dismiss"},
				})
			}
		}
	}
	items := make([]AttentionInput, 0, len(states))
	for _, s := range states {
		items = append(items, AttentionInput{
			SourceKey: fmt.Sprintf("%s:%s", s.runID, s.stepID), SourceRevision: s.revision,
			TaskID: s.taskID, Title: s.runTitle, Reason: s.reason,
			Category: "workflow", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "workflow_run", Target: s.runID},
			Actions: []AttentionAction{
				{ID: "open", Label: "Open run", Kind: "navigate", Intent: "primary"},
				s.action,
			},
		})
	}
	return items, nil
}

// hasNotifyOnComplete parses a workflow definition JSON and returns whether
// notify_on_complete is set. Returns false on parse error.
func hasNotifyOnComplete(definitionJSON string) bool {
	var def struct {
		NotifyOnComplete bool `json:"notify_on_complete"`
	}
	if err := json.Unmarshal([]byte(definitionJSON), &def); err != nil {
		return false
	}
	return def.NotifyOnComplete
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
	case "dismiss":
		// Dismiss is handled by the attention service (marks the item as
		// dismissed). No workflow action needed.
		return nil
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
