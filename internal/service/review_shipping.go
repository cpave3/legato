package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The Shipping page (LEG-706): every review loop Legato looks after, whichever
// caller opened it, with where it stands as the run page says it
// (ReviewRunCaller.details), its layers' CI and mergeability, the steward's
// last wake, the author agent and the reviewer, and the actions the loop's
// waiting step takes now. It reads only, so any process with the run caller
// answers. Served by GET /api/shipping and `legato review-pass list`.

// The phases of a loop on the Shipping page.
const (
	// ShippingAuthoring is a handoff's loop before its author is ready.
	ShippingAuthoring = "authoring"
	// ShippingReviewing is a pass under way, or due (pass n of max).
	ShippingReviewing = "reviewing"
	// ShippingChangesRequested is a loop waiting for its author's fixes.
	ShippingChangesRequested = "changes_requested"
	// ShippingApproved is a clean pass confirmed at GitHub's heads, with no
	// steward to watch it.
	ShippingApproved = "approved"
	// ShippingWatching is a steward watching the approved pull requests.
	ShippingWatching = "watching"
	// ShippingHeld is a steward the operator stopped watching.
	ShippingHeld = "held"
	// ShippingStuck is a loop that needs the operator to go on: out of
	// passes, or a steward at its re-entry limit.
	ShippingStuck = "stuck"
	// ShippingWaitingForMerge is a loop closed approved whose caller still
	// runs: it waits for the merge.
	ShippingWaitingForMerge = "waiting_for_merge"
	// ShippingEnded is a loop that ended; its outcome says how.
	ShippingEnded = "ended"
)

// The CI of a loop, as its last read of the counted checks found it.
const (
	ShippingChecksPassing = "passing"
	ShippingChecksFailing = "failing"
	ShippingChecksPending = "pending"
)

// ShippingEndedWindow is how far back the Shipping page lists ended loops.
const ShippingEndedWindow = 7 * 24 * time.Hour

// ShippingView is the Shipping page's list: the open loops, then those
// waiting for a merge, then those that ended in the last ShippingEndedWindow,
// newest first.
type ShippingView struct {
	Loops []ShippingLoop `json:"loops"`
}

// ShippingLoop is one loop on the Shipping page.
type ShippingLoop struct {
	LoopID string `json:"loop_id"`
	// Kind is ReviewLoopKindRun, ReviewLoopKindChildRun or
	// ReviewLoopKindHandoff.
	Kind string `json:"kind"`
	// Caller is the workflow whose run opened the loop, at the top of its
	// run's parents (ticket-to-merged-pr, review-loop for the card menu), or
	// "handoff"; CallerTitle that run's title.
	Caller      string `json:"caller"`
	CallerTitle string `json:"caller_title,omitempty"`
	// RunID is the run whose steps move the loop, Step the path of its step
	// that waits on the loop: the actions go to it. Both empty for a
	// handoff's own loop.
	RunID string `json:"run_id,omitempty"`
	Step  string `json:"step,omitempty"`
	// Link is where the web UI shows the loop: its run's page, or the
	// handed-off card.
	Link string `json:"link"`
	// Task is the board task the pull requests belong to.
	Task *ShippingTask `json:"task,omitempty"`
	Repo string        `json:"repo"`
	// Layers are the pull requests, bottom of the stack first.
	Layers  []ShippingLayer `json:"layers"`
	Phase   string          `json:"phase"`
	Summary string          `json:"summary"`
	Reason  string          `json:"reason,omitempty"`
	// Pass is the current pass, MaxPasses those allowed before it goes
	// stuck (0 for this Legato's setting).
	Pass      int `json:"pass"`
	MaxPasses int `json:"max_passes"`
	// Checks is ShippingChecks*, "" before the checks were read.
	Checks   string               `json:"checks,omitempty"`
	Findings *ReviewFindingCounts `json:"findings,omitempty"`
	// Steward is the steward while it watches: held, its last wake and
	// when, its re-entries and its idle deadline.
	Steward        *ReviewStewardView `json:"steward,omitempty"`
	ApprovalStands string             `json:"approval_stands,omitempty"`
	Author         *ShippingAuthor    `json:"author,omitempty"`
	Reviewer       string             `json:"reviewer,omitempty"`
	ReviewerModel  string             `json:"reviewer_model,omitempty"`
	// Actions are what the waiting step takes now, by the step action route:
	// another_pass, message, stop_watching, watch_again, end.
	Actions  []string `json:"actions,omitempty"`
	NeedsYou bool     `json:"needs_you"`
	Open     bool     `json:"open"`
	Outcome  string   `json:"outcome,omitempty"`
	// UpdatedAt is when the loop last changed, ClosedAt when it closed.
	UpdatedAt string `json:"updated_at,omitempty"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

// ShippingTask is a board task by its ID and title.
type ShippingTask struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// ShippingAuthor is a loop's agent author: its task and what its agent is
// doing (working, background, waiting, idle, error), or stopped when no
// agent runs.
type ShippingAuthor struct {
	Task  string `json:"task"`
	Title string `json:"title,omitempty"`
	State string `json:"state"`
}

// ShippingAuthorStopped is an author task with no agent running.
const ShippingAuthorStopped = "stopped"

// ShippingLayer is one pull request of a loop: as the run page shows it,
// with the counted checks that fail on it.
type ShippingLayer struct {
	ReviewRunLayer
	FailingChecks []string `json:"failing_checks,omitempty"`
}

// reviewWaitingSteps are the step types that wait on a loop and take its
// actions: the review loop steps, and the old inline step.
var reviewWaitingSteps = map[string]bool{
	ReviewPassStepType: true, ReviewWaitStepType: true, ReviewConfirmStepType: true, ReviewPassesStepType: true,
}

// Shipping lists every open loop, those closed approved whose caller still
// waits for the merge, and those closed in the last ShippingEndedWindow.
func (c *ReviewRunCaller) Shipping(ctx context.Context) (ShippingView, error) {
	db := c.engine.db
	open, err := db.ListOpenReviewLoops(ctx)
	if err != nil {
		return ShippingView{}, err
	}
	since := c.engine.opts.Now().Add(-ShippingEndedWindow).UTC().Format(time.RFC3339)
	closed, err := db.ListReviewLoopsClosedAfter(ctx, since)
	if err != nil {
		return ShippingView{}, err
	}
	// A merge waited for longer than the window is still waited for.
	waits, err := db.ListApprovedLoopsAwaitingCallers(ctx, since)
	if err != nil {
		return ShippingView{}, err
	}
	closed = append(closed, waits...)
	view := ShippingView{Loops: []ShippingLoop{}}
	var waiting, ended []ShippingLoop
	for i := range open {
		view.Loops = append(view.Loops, c.shippingLoop(ctx, &open[i]))
	}
	for i := range closed {
		loop := c.shippingLoop(ctx, &closed[i])
		switch {
		case loop.Phase == ShippingWaitingForMerge:
			waiting = append(waiting, loop)
		case loop.ClosedAt >= since:
			ended = append(ended, loop)
		}
	}
	sort.SliceStable(ended, func(i, j int) bool { return ended[i].ClosedAt > ended[j].ClosedAt })
	view.Loops = append(append(view.Loops, waiting...), ended...)
	return view, nil
}

// shippingLoop is one loop record as the Shipping page shows it.
func (c *ReviewRunCaller) shippingLoop(ctx context.Context, rec *store.ReviewLoop) ShippingLoop {
	db := c.engine.db
	_, link := reviewLoopName(rec)
	loop := ShippingLoop{LoopID: rec.ID, Repo: rec.Repo, Link: link, Open: rec.State == store.ReviewLoopOpenState,
		Pass: rec.Pass, MaxPasses: rec.MaxPasses, UpdatedAt: rec.UpdatedAt, ClosedAt: rec.ClosedAt, Layers: []ShippingLayer{}}
	if !loop.Open {
		loop.Outcome = rec.Outcome
	}
	chain := c.runChain(ctx, rec.RunID)
	loop.Kind, loop.Caller = ReviewLoopKindRun, workflowengine.ReviewLoopWorkflowName
	if len(chain) > 0 {
		top := chain[len(chain)-1]
		loop.Caller, loop.CallerTitle = top.WorkflowName, top.WorkflowTitle
		if len(chain) > 1 {
			loop.Kind = ReviewLoopKindChildRun
		}
	}
	if rec.OwnerKind == store.ReviewLoopOwnerHandoff || rec.HandoffID != "" {
		loop.Kind, loop.Caller = ReviewLoopKindHandoff, ReviewLoopKindHandoff
	}
	if rec.OwnerKind == store.ReviewLoopOwnerRun {
		loop.RunID = rec.RunID
	}

	d, _ := c.details(ctx, rec.HostTask).(ReviewRunDetails)
	if d.LoopID != "" && d.LoopID != rec.ID {
		// The host task's current loop is another: this one's facts are its
		// record's alone.
		d = ReviewRunDetails{Phase: ReviewPhaseEnded}
	}
	loop.Summary, loop.Reason, loop.Findings, loop.Steward = d.Summary, d.Reason, d.Findings, d.Steward
	loop.ApprovalStands, loop.Actions, loop.NeedsYou = d.ApprovalStands, d.Actions, d.NeedsYou
	loop.Reviewer, loop.ReviewerModel = rec.Reviewer, rec.ReviewerModel
	if rec.ReviewerTier != "" {
		loop.Reviewer += ":" + rec.ReviewerTier
	}
	if loop.Open && len(loop.Actions) > 0 {
		loop.Step = waitingLoopStep(ctx, db, rec.RunID)
		if loop.Step == "" {
			loop.Actions = nil
		}
	}

	failing, onBase := c.failingChecks(ctx, rec.ID), checksOnBase(ctx, db, rec.ID)
	layers, _ := db.ListReviewLoopPRs(ctx, rec.ID)
	for _, layer := range layers {
		url := layer.URL
		if url == "" {
			url = "https://github.com/" + rec.Repo + "/pull/" + strconv.Itoa(layer.Number)
		}
		loop.Layers = append(loop.Layers, ShippingLayer{
			ReviewRunLayer: ReviewRunLayer{Number: layer.Number, URL: url, Title: layer.Title, State: layer.State, Draft: layer.Draft, Mergeable: layer.Mergeable,
				ChecksOnBase: onBase[layer.Number]},
			FailingChecks: failing[layer.Number],
		})
	}
	switch {
	case len(failing) > 0:
		loop.Checks = ShippingChecksFailing
	case rec.ChecksPending > 0:
		loop.Checks = ShippingChecksPending
	case rec.ChecksPolledAt != "":
		loop.Checks = ShippingChecksPassing
	}

	loop.Task = c.shippingTask(ctx, rec)
	if rec.AgentAuthor && rec.AuthorTask != "" {
		loop.Author = c.shippingAuthor(ctx, rec.AuthorTask)
	}
	if loop.Open && (rec.Phase == ReviewPhaseReady || rec.Phase == ReviewPhaseWaitingForReviewer) {
		// The record keeps the last pass until the next one starts; the run
		// page names the pass to come.
		loop.Pass = rec.Pass + 1
	}
	loop.Phase = shippingPhase(rec, d, chain)
	if !loop.Open {
		loop.Summary, loop.Actions, loop.NeedsYou, loop.Steward = closedSummary(loop.Phase, rec.Outcome), nil, false, nil
	}
	switch loop.Phase {
	case ShippingStuck, ShippingHeld:
		loop.NeedsYou = true
	case ShippingWatching:
		loop.NeedsYou = loop.NeedsYou || loop.Checks == ShippingChecksFailing
	}
	return loop
}

// shippingPhase is where a loop stands, from its record and the run page's
// details of it.
func shippingPhase(rec *store.ReviewLoop, d ReviewRunDetails, chain []store.WorkflowRun) string {
	if rec.State != store.ReviewLoopOpenState {
		if rec.Outcome == ReviewOutcomeApproved {
			// Any run up the chain of calls that still runs waits on it.
			for _, run := range chain {
				if runLive(run.Status) {
					return ShippingWaitingForMerge
				}
			}
		}
		return ShippingEnded
	}
	switch {
	case d.Phase == ReviewPhaseStuck || rec.Phase == ReviewPhaseStuck:
		return ShippingStuck
	case stewarding(rec) && rec.Held:
		return ShippingHeld
	case d.Phase == ReviewPhaseReviewing:
		return ShippingReviewing
	case d.Phase == ReviewPhaseWaitingForAuthor:
		return ShippingChangesRequested
	case d.Phase == ReviewPhaseClean && stewarding(rec):
		return ShippingWatching
	case d.Phase == ReviewPhaseClean:
		return ShippingApproved
	case d.Phase == ReviewPhaseAuthor && rec.Pass == 0 && rec.HandoffID != "":
		return ShippingAuthoring
	}
	return ShippingReviewing
}

// closedSummary is what a closed loop's row says.
func closedSummary(phase, outcome string) string {
	if phase == ShippingWaitingForMerge {
		return "Approved · waiting for the merge"
	}
	words := map[string]string{
		ReviewOutcomeApproved: "Approved", ReviewOutcomeMerged: "Merged", ReviewOutcomeClosed: "Closed, not merged",
		ReviewOutcomeEnded: "Ended", ReviewOutcomeStuck: "Ended while stuck", ReviewOutcomeCancelled: "Cancelled", StewardWokeIdle: "Ended idle",
	}
	if w, ok := words[outcome]; ok {
		return w
	}
	return "Ended"
}

// runLive says a run's status is not an end.
func runLive(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", WorkflowRunStopped:
		return false
	}
	return status != ""
}

// runChain is a run and its parents, the run first; empty when the run is
// not found.
func (c *ReviewRunCaller) runChain(ctx context.Context, runID string) []store.WorkflowRun {
	var chain []store.WorkflowRun
	for id := runID; id != "" && len(chain) < 8; {
		view, err := c.engine.db.GetWorkflowRun(ctx, id)
		if err != nil {
			break
		}
		chain = append(chain, view.Run)
		id = view.Run.ParentRunID
	}
	return chain
}

// waitingLoopStep is the path of a run's newest waiting step that waits on
// its loop, "" when none does.
func waitingLoopStep(ctx context.Context, db *store.Store, runID string) string {
	if runID == "" {
		return ""
	}
	view, err := db.GetWorkflowRun(ctx, runID)
	if err != nil {
		return ""
	}
	for i := len(view.Attempts) - 1; i >= 0; i-- {
		attempt := view.Attempts[i]
		if attempt.Status == WorkflowStepWaiting && reviewWaitingSteps[attempt.ExecutorType] {
			return attempt.Path
		}
	}
	return ""
}

// failingChecks are the counted checks failing on each layer, by its
// number: the loop's check items still open.
func (c *ReviewRunCaller) failingChecks(ctx context.Context, loopID string) map[int][]string {
	items, err := c.engine.db.ListReviewLoopItems(ctx, loopID)
	if err != nil {
		return nil
	}
	failing := map[int][]string{}
	for _, item := range items {
		if item.Kind != ItemCheck || item.Closed {
			continue
		}
		failing[item.PR] = append(failing[item.PR], checkName(item))
	}
	if len(failing) == 0 {
		return nil
	}
	return failing
}

// checksOnBase are the checks failing on each layer that its base fails
// too, by its number: the loop's check items closed as AnswerOnBase.
func checksOnBase(ctx context.Context, db *store.Store, loopID string) map[int][]string {
	items, err := db.ListReviewLoopItems(ctx, loopID)
	if err != nil {
		return nil
	}
	out := map[int][]string{}
	for _, item := range items {
		if item.Kind == ItemCheck && item.Closed && item.Answer == AnswerOnBase {
			out[item.PR] = append(out[item.PR], checkName(item))
		}
	}
	return out
}

// checkName is a check item's check: its ID is k-<pr>-<name>.
func checkName(item store.ReviewLoopItem) string {
	prefix := "k-" + strconv.Itoa(item.PR) + "-"
	if name, ok := strings.CutPrefix(item.ID, prefix); ok && name != "" {
		return name
	}
	return item.Title
}

// shippingTask is the board task a loop's pull requests belong to: its
// author's, else its linked task, else the task that hosts it.
func (c *ReviewRunCaller) shippingTask(ctx context.Context, rec *store.ReviewLoop) *ShippingTask {
	for _, id := range []string{rec.AuthorTask, rec.LinkedTask, rec.HostTask} {
		if id == "" {
			continue
		}
		task, err := c.engine.db.GetTask(ctx, id)
		if errors.Is(err, store.ErrNotFound) || task == nil {
			continue
		}
		if err != nil {
			return &ShippingTask{ID: id}
		}
		return &ShippingTask{ID: id, Title: task.Title}
	}
	return nil
}

// shippingAuthor is an author task and what its agent is doing.
func (c *ReviewRunCaller) shippingAuthor(ctx context.Context, taskID string) *ShippingAuthor {
	author := &ShippingAuthor{Task: taskID, State: ShippingAuthorStopped}
	if task, err := c.engine.db.GetTask(ctx, taskID); err == nil && task != nil {
		author.Title = task.Title
	}
	if session, err := c.authorSession(ctx, taskID); err == nil && session != nil && session.Status == "running" {
		author.State = session.Activity
		if author.State == "" {
			author.State = "idle"
		}
	}
	return author
}
