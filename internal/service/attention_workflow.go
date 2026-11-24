package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// WorkflowAttentionEvents are the events the workflows producer reconciles
// on: a run's own changes, and its agents' (a step's agent that ends or
// stops, a review loop's agent author that stops or starts work again).
var WorkflowAttentionEvents = []events.EventType{
	events.EventWorkflowChanged, events.EventAgentActivityChanged, events.EventAgentStarted,
	events.EventAgentDied, events.EventAgentCompleted,
}

type WorkflowAttentionSource interface {
	ListRuns(ctx context.Context, filter store.WorkflowRunFilter) ([]store.WorkflowRun, error)
	Show(ctx context.Context, runID string) (*store.WorkflowRunView, error)
	TriggerStep(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error)
	Retry(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error)
	PromptStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error)
	StepAction(ctx context.Context, runID, stepID, action, text string) (*store.WorkflowRunView, error)
	ResumeAgent(ctx context.Context, runID, macro string) (MacroStartResult, error)
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
	kind     string // manual | failed | waiting_agent | waiting | author | merge
	revision string
	runTitle string
	// title, when set, is the item's in place of the run's.
	title string
	// card names the run's related card, "" for none.
	card   string
	taskID string
	reason string
	action AttentionAction
	// more are the actions after the first, for a step waiting on its
	// service.
	more []AttentionAction
}

func (a *WorkflowAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	runs, err := a.workflows.ListRuns(ctx, store.WorkflowRunFilter{})
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
			runStates := a.collectRunStates(run, view)
			for i := range runStates {
				// The item's task is the run's own: name the card it is for.
				runStates[i].card = runCardDetail(run)
			}
			states = append(states, runStates...)
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
		case "waiting":
			signalKind = AttentionSignalWorkflowReviewStuck
		case "author":
			signalKind = AttentionSignalWorkflowReviewAuthor
		case "merge":
			signalKind = AttentionSignalWorkflowMergeWaiting
		}
		title := s.runTitle
		if s.title != "" {
			title = s.title
		}
		items = append(items, AttentionInput{
			SignalKind: signalKind,
			SourceKey:  fmt.Sprintf("%s:%s", s.runID, s.stepID), SourceRevision: s.revision,
			TaskID: s.taskID, Title: title, Reason: s.reason, Detail: s.card,
			Category: "workflow", Urgency: urgency,
			Navigation: AttentionNavigation{Kind: "workflow_run", Target: s.runID},
			Actions: append([]AttentionAction{
				{ID: "open", Label: "Open run", Kind: "navigate", Intent: "primary"},
				s.action,
			}, s.more...),
		})
	}
	return items, nil
}

// runCardDetail names the card a run is for, "" for none.
func runCardDetail(run store.WorkflowRun) string {
	if run.RelatedTaskKey == "" {
		return ""
	}
	if run.RelatedTaskTitle == "" {
		return "For " + run.RelatedTaskKey
	}
	return "For " + run.RelatedTaskKey + ": " + run.RelatedTaskTitle
}

func (a *WorkflowAttentionAdapter) collectRunStates(run store.WorkflowRun, view *store.WorkflowRunView) []workflowAttentionState {
	var states []workflowAttentionState
	for _, step := range view.Steps {
		if step.Status == "waiting_manual" {
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: step.Path, kind: "manual",
				revision: fmt.Sprintf("manual:%s:%s", step.Path, step.Status),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: "Manual action required: " + step.Title,
				action: AttentionAction{ID: "trigger", Label: "Trigger", Kind: "execute", RequiresConfirm: true},
			})
		}
		if step.Status == "failed" {
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: step.Path, kind: "failed",
				revision: fmt.Sprintf("failed:%s:%s", step.Path, step.Status),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: "Step failed: " + step.Title,
				action: AttentionAction{ID: "retry", Label: "Retry", Kind: "execute", RequiresConfirm: true},
			})
		}
	}
	for _, attempt := range view.Attempts {
		if wait, ok := view.Details[attempt.Path].(MergeWait); ok && attempt.ExecutorType == PRWaitStepType && attempt.Status == WorkflowStepWaiting {
			if attempt.NeedsAttention {
				states = append(states, mergeWaitState(run, attempt, wait))
			}
			continue
		}
		if attempt.NeedsAttention && attempt.Status == WorkflowStepWaiting {
			// A step waiting on its service that needs the person: a
			// review loop stuck (it offers end), or waiting for an
			// author that will not move it alone.
			kind := "author"
			var more []AttentionAction
			for _, id := range attempt.AttentionActions {
				switch id {
				case "end":
					kind = "waiting"
					more = append(more, AttentionAction{ID: "end", Label: "End", Kind: "execute", RequiresConfirm: true,
						Confirmation: "End the review loop as stuck? The run goes on to its next step."})
				case "message":
					more = append(more, AttentionAction{ID: "message", Label: "Message the author", Kind: "execute", RequiresText: true})
				}
			}
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: attempt.Path, kind: kind,
				revision: fmt.Sprintf("waiting:%s:%d:%s", attempt.Path, attempt.AttemptNumber, attempt.AttentionReason),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: attempt.AttentionReason,
				action: AttentionAction{ID: "another_pass", Label: "Run another pass", Kind: "execute"},
				more:   more,
			})
			continue
		}
		if !attempt.NeedsAttention || attempt.Status != "waiting_agent" {
			continue
		}
		states = append(states, workflowAttentionState{
			runID: run.ID, stepID: attempt.Path, kind: "waiting_agent",
			revision: fmt.Sprintf("agent:%s:%s:%d", attempt.Path, attempt.Status, attempt.AttemptNumber),
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
	case "another_pass", "end", "message":
		_, err := a.workflows.StepAction(ctx, runID, stepID, request.ActionID, request.Text)
		return err
	case "resume_agent":
		_, err := a.workflows.ResumeAgent(ctx, runID, request.Text)
		return err
	default:
		return fmt.Errorf("unsupported workflow attention action %q", request.ActionID)
	}
}

// mergeWaitState is a waiting merge's item, on the card: Open PR, and
// Resume agent with a macro (Send a macro while the agent runs, with Open
// agent beside it); Open agent alone while the agent waits on the operator.
func mergeWaitState(run store.WorkflowRun, attempt store.WorkflowStepAttempt, wait MergeWait) workflowAttentionState {
	taskID := wait.TaskID
	if taskID == "" {
		taskID = run.TaskID
	}
	var more []AttentionAction
	if wait.TaskID != "" {
		switch wait.Agent {
		case "":
			more = append(more, AttentionAction{ID: "resume_agent", Label: "Resume agent", Kind: "execute", Picks: AttentionPicksMacro})
		case "running":
			more = append(more, AttentionAction{ID: "resume_agent", Label: "Send a macro", Kind: "execute", Picks: AttentionPicksMacro})
		}
		if wait.Agent != "" {
			more = append(more, AttentionAction{ID: "open_agent", Label: "Open agent", Kind: "navigate", Href: agentHref(wait.TaskID, run.ID)})
		}
	}
	return workflowAttentionState{
		runID: run.ID, stepID: attempt.Path, kind: "merge",
		revision: wait.revision(attempt),
		runTitle: run.WorkflowTitle, title: wait.Title(), taskID: taskID,
		reason: wait.Reason(),
		action: AttentionAction{ID: "open_pr", Label: "Open PR", Kind: "navigate", Href: wait.URL},
		more:   more,
	}
}

func splitWorkflowSourceKey(sourceKey string) (runID, stepID string, ok bool) {
	for i := 0; i < len(sourceKey); i++ {
		if sourceKey[i] == ':' {
			return sourceKey[:i], sourceKey[i+1:], true
		}
	}
	return "", "", false
}
