package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// MergeWait is a github.pr.wait step that waits for a pull request to
// merge, as its needs-you item and the run page show it (LEG-614): the pull
// request, what the latest read said of it, the card it is for and that
// card's agent.
type MergeWait struct {
	Repo   string `json:"repo"`
	Number int    `json:"pr_number"`
	URL    string `json:"url,omitempty"`
	// State is the latest read's (open), "" before one.
	State       string `json:"state,omitempty"`
	HeadBranch  string `json:"head_branch,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
	Conflicts   bool   `json:"conflicts,omitempty"`
	CheckStatus string `json:"check_status,omitempty"`
	// TaskID is the card, TaskTitle its title; "" when the run names none.
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	// Agent is the card's agent: "", "running" or "waiting" (on the
	// operator).
	Agent string `json:"agent,omitempty"`
	// Notify is the step's notify: false makes no needs-you item.
	Notify bool `json:"notify"`
}

// Title is the needs-you item's.
func (w MergeWait) Title() string { return fmt.Sprintf("Review and merge PR #%d", w.Number) }

// Reason is what the item says: the conflict and failing CI first, then
// the card, then what Resume agent would do with its agent.
func (w MergeWait) Reason() string {
	var parts []string
	if w.Conflicts {
		base := w.BaseBranch
		if base == "" {
			base = "main"
		}
		parts = append(parts, fmt.Sprintf("PR #%d conflicts with %s", w.Number, base))
	}
	if w.CheckStatus == "fail" {
		parts = append(parts, "CI failing")
	}
	if len(parts) == 0 {
		if w.TaskTitle != "" {
			parts = append(parts, w.TaskTitle)
		}
		parts = append(parts, w.Repo)
	}
	switch w.Agent {
	case "running":
		parts = append(parts, "The agent is running: a macro goes to it as its next message")
	case "waiting":
		parts = append(parts, "The agent waits on you")
	}
	return strings.Join(parts, " · ")
}

// Revision changes when the pull request starts or stops conflicting or its
// CI changes, so a dismissed item comes back; not when the agent starts or
// stops, which only changes the item's actions.
func (w MergeWait) revision(attempt store.WorkflowStepAttempt) string {
	return fmt.Sprintf("merge:%s:%d:%t:%s", attempt.Path, attempt.AttemptNumber, w.Conflicts, w.CheckStatus)
}

// mergeWait is where a waiting github.pr.wait attempt stands, false for
// any other attempt.
func (s *WorkflowService) mergeWait(ctx context.Context, view *store.WorkflowRunView, attempt store.WorkflowStepAttempt) (MergeWait, bool) {
	if attempt.ExecutorType != PRWaitStepType || attempt.Status != WorkflowStepWaiting {
		return MergeWait{}, false
	}
	with, ok := stepWith(view, attempt.Path)
	if !ok {
		return MergeWait{}, false
	}
	target, err := prWaitTarget(with)
	if err != nil {
		return MergeWait{}, false
	}
	wait := MergeWait{Repo: target.repo, Number: target.number, Notify: with["notify"] != false}
	if executor, ok := s.executors[PRWaitStepType].(*PRWaitExecutor); ok {
		if state, ok := executor.last(attempt.ID); ok {
			wait.State, wait.URL, wait.HeadBranch, wait.BaseBranch = state.State, state.URL, state.HeadBranch, state.BaseBranch
			wait.Conflicts, wait.CheckStatus = conflicted(state), state.CheckStatus
		}
	}
	if wait.URL == "" {
		wait.URL = fmt.Sprintf("https://github.com/%s/pull/%d", target.repo, target.number)
	}
	wait.TaskID, _ = stepAgentTask(with)
	if wait.TaskID == "" {
		wait.TaskID = view.Run.RelatedTaskID
	}
	if wait.TaskID != "" {
		if task, err := s.store.GetTask(ctx, wait.TaskID); err == nil {
			wait.TaskTitle = task.Title
		}
		wait.Agent = s.cardAgent(ctx, wait.TaskID)
	}
	return wait, true
}

// cardAgent is a card's main agent as Resume agent sees it.
func (s *WorkflowService) cardAgent(ctx context.Context, taskID string) string {
	session, err := s.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return ""
	}
	if session.Activity == store.ActivityWaiting {
		return "waiting"
	}
	return "running"
}

func stepWith(view *store.WorkflowRunView, path string) (map[string]any, bool) {
	for _, step := range view.Steps {
		if step.Path != path {
			continue
		}
		var with map[string]any
		if err := json.Unmarshal([]byte(step.ResolvedInputsJSON), &with); err != nil {
			return nil, false
		}
		return with, true
	}
	return nil, false
}

// addMergeWaits marks each waiting merge that notifies as needing the
// operator, puts where it stands in the run's Details, and says whether
// Resume agent is offered.
func (s *WorkflowService) addMergeWaits(ctx context.Context, view *store.WorkflowRunView) {
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		wait, ok := s.mergeWait(ctx, view, *attempt)
		if !ok {
			continue
		}
		if wait.Notify {
			attempt.NeedsAttention, attempt.AttentionReason = true, wait.Reason()
		}
		if view.Details == nil {
			view.Details = map[string]any{}
		}
		view.Details[attempt.Path] = wait
		if view.ResumeTaskID == "" {
			view.ResumeTaskID, view.ResumeAgentState = wait.TaskID, wait.Agent
		}
	}
	if view.ResumeTaskID == "" && view.Run.RelatedTaskID != "" {
		view.ResumeTaskID = view.Run.RelatedTaskID
		view.ResumeAgentState = s.cardAgent(ctx, view.ResumeTaskID)
	}
	view.CanResumeAgent = view.ResumeTaskID != "" && view.ResumeAgentState != "waiting" && s.resumer != nil &&
		(view.Run.Status == "running" || view.Run.Status == "waiting") && !hasAgentStepWaiting(view)
}

// hasAgentStepWaiting says whether a step's own agent is at work: that step
// owns the agent, so the run offers Prompt and Retry there, not Resume.
func hasAgentStepWaiting(view *store.WorkflowRunView) bool {
	for _, attempt := range view.Attempts {
		if attempt.Status == "waiting_agent" {
			return true
		}
	}
	return false
}

// AgentResumer is what Resume agent needs: the macro start the card menu
// uses (MacroService.Start).
type AgentResumer interface {
	Start(ctx context.Context, req MacroStart) (MacroStartResult, error)
}

// AgentKiller stops a task's main agent.
type AgentKiller interface {
	KillAgent(ctx context.Context, taskID string) error
}

// SetAgentResumer lets a run's operator resume its card's agent with a
// macro, and the run stop the agent it resumed when its merge is over.
func (s *WorkflowService) SetAgentResumer(resumer AgentResumer, agents AgentKiller) {
	s.resumer, s.resumedAgents = resumer, agents
}

// ErrResumeAgentWaiting is Resume agent on a card whose agent waits on the
// operator, at a permission prompt or a question: they answer it there.
var ErrResumeAgentWaiting = errors.New("the card's agent waits on the operator: answer it in the agent")

// ResumeAgent puts a macro to work on the run's card (ResumeTaskID): with
// its agent running, the macro is its next message; with none, the card's
// own agent starts with it, resuming its conversation in its clone, or in a
// fresh clone of the pull request's branch when that folder is gone. The
// run goes on waiting. An agent started while a github.pr.wait step waits
// is recorded on that step, which stops it when the pull request merges or
// closes. The card and its ticket are left as they are.
func (s *WorkflowService) ResumeAgent(ctx context.Context, runID, macro string) (MacroStartResult, error) {
	if s.resumer == nil {
		return MacroStartResult{}, ErrMacroAgentsMissing
	}
	if strings.TrimSpace(macro) == "" {
		return MacroStartResult{}, errors.New("name the macro to resume the agent with")
	}
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return MacroStartResult{}, err
	}
	s.addMergeWaits(ctx, view)
	switch {
	case view.Run.Status != "running" && view.Run.Status != "waiting":
		return MacroStartResult{}, fmt.Errorf("run %s is %s: %w", runID, view.Run.Status, store.ErrWorkflowStateConflict)
	case hasAgentStepWaiting(view):
		return MacroStartResult{}, fmt.Errorf("a step's agent is at work; prompt or retry that step: %w", store.ErrWorkflowStateConflict)
	case view.ResumeTaskID == "":
		return MacroStartResult{}, fmt.Errorf("run %s names no card: %w", runID, store.ErrWorkflowStateConflict)
	case view.ResumeAgentState == "waiting":
		return MacroStartResult{}, ErrResumeAgentWaiting
	}
	req := MacroStart{Name: macro, TaskID: view.ResumeTaskID}
	var waiting *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if wait, ok := s.mergeWait(ctx, view, view.Attempts[i]); ok && wait.TaskID == view.ResumeTaskID {
			waiting = &view.Attempts[i]
			req.PR = &MacroPR{Repo: wait.Repo, Number: wait.Number, URL: wait.URL}
			req.Branch = wait.HeadBranch
			break
		}
	}
	result, err := s.resumer.Start(ctx, req)
	if err != nil || !result.Started || waiting == nil {
		return result, err
	}
	session, err := s.store.GetAgentSessionByTaskID(ctx, view.ResumeTaskID)
	if err != nil {
		log.Printf("workflows: run %s: the resumed agent of %s: %v", runID, view.ResumeTaskID, err)
		return result, nil
	}
	if err := s.store.RecordWorkflowAttemptAgent(ctx, waiting.ID, *session); err != nil {
		log.Printf("workflows: run %s: recording the resumed agent of %s: %v", runID, view.ResumeTaskID, err)
	}
	return result, nil
}

// stopResumedAgent stops the agent Resume agent started while attempt
// waited, now its wait is over: only while that very session is still the
// card's running agent, so an agent the operator started since is left
// alone.
func (s *WorkflowService) stopResumedAgent(ctx context.Context, attempt store.WorkflowStepAttempt) {
	if attempt.AgentSessionID == nil || s.resumedAgents == nil {
		return
	}
	recorded, err := s.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil || recorded.Status != "running" {
		return
	}
	current, err := s.store.GetAgentSessionByTaskID(ctx, recorded.TaskID)
	if err != nil || current.ID != recorded.ID {
		return
	}
	if err := s.resumedAgents.KillAgent(ctx, recorded.TaskID); err != nil {
		log.Printf("workflows: run %s: stopping the resumed agent of %s: %v", attempt.RunID, recorded.TaskID, err)
	}
}

// agentHref is the web UI's page of a card's agent, shown for a run.
func agentHref(taskID, runID string) string {
	return "/agents?task=" + url.QueryEscape(taskID) + "&workflow=" + url.QueryEscape(runID)
}
