package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
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
	// signal, when set, is the item's signal kind (a step waiting on its
	// service names it).
	signal string
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
		case "gate":
			signalKind = AttentionSignalWorkflowGate
		case "paused":
			signalKind = AttentionSignalWorkflowFailed
			urgency = "high"
		}
		if s.signal != "" {
			signalKind = s.signal
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
	return append(items, a.notices(ctx)...), nil
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
		if step.Status == WorkflowStepPaused {
			states = append(states, workflowAttentionState{
				runID: run.ID, stepID: step.Path, kind: "paused",
				revision: fmt.Sprintf("paused:%s:%d", step.Path, step.LatestAttempt),
				runTitle: run.WorkflowTitle, taskID: run.TaskID,
				reason: pausedReason(step),
				action: AttentionAction{ID: PausedRetry, Label: "Retry", Kind: "execute"},
				more: []AttentionAction{
					{ID: PausedSkip, Label: "Skip", Kind: "execute", RequiresConfirm: true, Confirmation: "Skip the step? Its outputs are missing, and the run goes on."},
					{ID: PausedStop, Label: "Stop", Kind: "execute", RequiresConfirm: true, Confirmation: "Stop the run?"},
				},
			})
		}
		// A failed step of a run that goes on was continued: only the
		// failure holding a failed run needs the operator, and only it can
		// be retried. A loop's pass's failed step is its loop's failure,
		// which Retry is on (LEG-490).
		if step.Status == "failed" && run.Status == "failed" && step.ParentPath == "" && step.Path == run.CurrentStepID {
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
		if attempt.NeedsAttention && attempt.Status == WorkflowStepWaiting && attempt.ExecutorType == workflowengine.GateStepType {
			states = append(states, gateState(run, attempt))
			continue
		}
		if attempt.NeedsAttention && attempt.Status == WorkflowStepWaiting {
			// A step waiting on its service that needs the operator: a
			// review loop stuck, waiting for an author that will not move
			// it alone, or its steward held or its approval dismissed.
			states = append(states, waitingState(run, attempt))
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

// waitingState is the item of a step waiting on its service that needs the
// operator, with the actions it offers, the first the primary. A step that
// names no signal offers another_pass first, and is stuck when it offers
// end.
func waitingState(run store.WorkflowRun, attempt store.WorkflowStepAttempt) workflowAttentionState {
	ids, kind := attempt.AttentionActions, "author"
	if attempt.AttentionSignal == "" {
		ids = append([]string{ReviewLoopActionAnotherPass}, ids...)
		if slices.Contains(ids, ReviewLoopActionEnd) {
			kind = "waiting"
		}
	}
	state := workflowAttentionState{
		runID: run.ID, stepID: attempt.Path, kind: kind, signal: attempt.AttentionSignal,
		revision: fmt.Sprintf("waiting:%s:%d:%s", attempt.Path, attempt.AttemptNumber, attempt.AttentionReason),
		runTitle: run.WorkflowTitle, taskID: run.TaskID,
		reason: attempt.AttentionReason,
	}
	for i, id := range ids {
		action := waitingAction(id, attempt.AttentionSteward)
		if i == 0 {
			state.action = action
			continue
		}
		state.more = append(state.more, action)
	}
	return state
}

// waitingAction is how an item offers a waiting step's action. End asks
// first, in a steward's words when the loop is stewarded: it ends the
// steward (outcome ended), not a stuck review (outcome stuck).
func waitingAction(id string, steward bool) AttentionAction {
	switch id {
	case ReviewLoopActionAnotherPass:
		return AttentionAction{ID: id, Label: "Run another pass", Kind: "execute"}
	case ReviewLoopActionWatchAgain:
		return AttentionAction{ID: id, Label: "Watch again", Kind: "execute"}
	case ReviewLoopActionStopWatching:
		return AttentionAction{ID: id, Label: "Stop watching", Kind: "execute"}
	case ReviewLoopActionMessage:
		return AttentionAction{ID: id, Label: "Message the author", Kind: "execute", RequiresText: true}
	case ReviewLoopActionEnd:
		if steward {
			return AttentionAction{ID: id, Label: "End", Kind: "execute", RequiresConfirm: true,
				Confirmation: StewardEndConfirmation}
		}
		return AttentionAction{ID: id, Label: "End", Kind: "execute", RequiresConfirm: true,
			Confirmation: "End the review loop as stuck? The run goes on to its next step."}
	}
	return AttentionAction{ID: id, Label: id, Kind: "execute"}
}

// StewardEndConfirmation is what End asks of a stewarded loop.
const StewardEndConfirmation = "End the steward? The review loop ends with outcome ended: nothing watches the pull requests after, " +
	"legato-reviews' approval stays on GitHub as it is, and the run goes on to its next step."

// notices are the items of the executors' notices (a steward that ended
// idle): Open run only, until the operator dismisses one or it goes.
func (a *WorkflowAttentionAdapter) notices(ctx context.Context) []AttentionInput {
	source, ok := a.workflows.(interface {
		Notices(ctx context.Context) []WorkflowNotice
	})
	if !ok {
		return nil
	}
	var items []AttentionInput
	for _, n := range source.Notices(ctx) {
		item := AttentionInput{
			SignalKind: n.Signal, SourceKey: n.RunID + ":" + n.StepID, SourceRevision: n.Revision,
			Title: n.Title, Reason: n.Reason, Category: "workflow", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "workflow_run", Target: n.RunID},
			Actions:    []AttentionAction{{ID: "open", Label: "Open run", Kind: "navigate", Intent: "primary"}},
		}
		if view, err := a.workflows.Show(ctx, n.RunID); err == nil {
			item.TaskID, item.Detail = view.Run.TaskID, runCardDetail(view.Run)
		}
		items = append(items, item)
	}
	return items
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
	case "another_pass", "end", "message", ReviewLoopActionWatchAgain, ReviewLoopActionStopWatching, PausedSkip, PausedStop:
		_, err := a.workflows.StepAction(ctx, runID, stepID, request.ActionID, request.Text)
		return err
	case "resume_agent":
		_, err := a.workflows.ResumeAgent(ctx, runID, request.Text)
		return err
	default:
		if strings.HasPrefix(request.ActionID, gateChoicePrefix) {
			_, err := a.workflows.StepAction(ctx, runID, stepID, request.ActionID, request.Text)
			return err
		}
		return fmt.Errorf("unsupported workflow attention action %q", request.ActionID)
	}
}

// gateState is a gate's item: its question, and an action for each option
// (Stop asks first).
func gateState(run store.WorkflowRun, attempt store.WorkflowStepAttempt) workflowAttentionState {
	var actions []AttentionAction
	for _, id := range attempt.AttentionActions {
		option := strings.TrimPrefix(id, gateChoicePrefix)
		action := AttentionAction{ID: id, Label: option, Kind: "execute"}
		if strings.EqualFold(option, workflowengine.GateStop) {
			action.RequiresConfirm, action.Confirmation = true, "Stop the run?"
		}
		actions = append(actions, action)
	}
	state := workflowAttentionState{
		runID: run.ID, stepID: attempt.Path, kind: "gate",
		revision: fmt.Sprintf("gate:%s:%d", attempt.Path, attempt.AttemptNumber),
		runTitle: run.WorkflowTitle, title: attempt.AttentionReason, taskID: run.TaskID,
		reason: run.WorkflowTitle + " asks you to choose",
	}
	if len(actions) > 0 {
		state.action, state.more = actions[0], actions[1:]
	}
	return state
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
