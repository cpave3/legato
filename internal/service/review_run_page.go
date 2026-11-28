package service

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// The run page's review loop (spec LEG-559 stories 11–13, 20, 23; LEG-594):
// what ReviewRunDetails says beyond the phase and the reviews, read from
// the loop record, so a process without the review engine shows it too.

// recordDetails adds the loop record's part to a run's loop details: its
// ID, outcome, reviewer, findings and layers; the steward while it
// watches; that legato-reviews' approval still stands while it is held or
// stuck; the actions its waiting step takes; and whether its author agent
// was left running.
func (c *ReviewRunCaller) recordDetails(ctx context.Context, d *ReviewRunDetails, taskID string, loop *store.HandoffReview) {
	db := c.engine.db
	rec, err := db.CurrentReviewLoop(ctx, taskID)
	if err != nil || rec == nil || (loop.LoopID != "" && loop.LoopID != rec.ID) {
		return
	}
	d.LoopID = rec.ID
	if rec.State != store.ReviewLoopOpenState {
		d.Outcome = rec.Outcome
	}
	d.Reviewer, d.ReviewerModel = rec.Reviewer, rec.ReviewerModel
	if rec.ReviewerTier != "" {
		d.Reviewer += ":" + rec.ReviewerTier
	}
	if findings, err := db.ListReviewLoopFindings(ctx, rec.ID); err == nil {
		counts := countFindings(findings)
		d.Findings = &counts
	}
	layers, _ := db.ListReviewLoopPRs(ctx, rec.ID)
	onBase := checksOnBase(ctx, db, rec.ID)
	for _, layer := range layers {
		url := layer.URL
		if url == "" {
			url = fmt.Sprintf("https://github.com/%s/pull/%d", rec.Repo, layer.Number)
		}
		d.Layers = append(d.Layers, ReviewRunLayer{Number: layer.Number, URL: url, Title: layer.Title,
			State: layer.State, Draft: layer.Draft, Mergeable: layer.Mergeable, ChecksOnBase: onBase[layer.Number],
			DraftedByLegato: layer.LegatoDraftedAt, DraftProblem: layer.DraftProblem})
	}
	run, _ := c.run(ctx, taskID)
	if stewarding(rec) {
		d.Steward = &ReviewStewardView{Held: rec.Held, LastWake: rec.LastWake, LastWakeAt: rec.LastWakeAt,
			Reentries: rec.Reentries, MaxReentries: rec.MaxReentries, IdleDeadline: rec.IdleDeadline}
		switch {
		case loop.Phase == ReviewPhaseStuck:
		case rec.Held:
			d.Summary = "Held"
		case confirmed(loop):
			d.Summary = "Stewarding"
		}
		if (rec.Held || loop.Phase == ReviewPhaseStuck) && approvalStands(loop) {
			// As the Attention items say it: a layer a person dismissed or
			// closed is left out (approvalStandsText).
			d.ApprovalStands = c.engine.approvalStandsText(ctx, rec)
		}
	}
	d.Actions = loopActions(rec, loop, run)
	if run != nil && !run.handoff && run.AuthorTask != "" {
		d.AuthorTask = run.AuthorTask
		d.AuthorLeftRunning = c.authorLeftRunning(ctx, rec, run.AuthorTask)
	}
}

// loopActions are the operator's actions a loop's waiting step takes now:
// a stuck loop's Another pass (a steward stuck at its watch: Watch again),
// Message the author and End (a handoff's ends with its handoff); a
// steward's Stop watching or Watch again, and End; a loop waiting for its
// author, Another pass and Message the author.
func loopActions(rec *store.ReviewLoop, loop *store.HandoffReview, run *reviewRun) []string {
	if rec.State != store.ReviewLoopOpenState || run == nil {
		return nil
	}
	message := func(actions []string) []string {
		if run.AuthorTask != "" {
			return append(actions, ReviewLoopActionMessage)
		}
		return actions
	}
	switch {
	case loop.Phase == ReviewPhaseStuck:
		actions := []string{ReviewLoopActionAnotherPass}
		if stewarding(rec) && approvalStands(loop) {
			actions = []string{ReviewLoopActionWatchAgain}
		}
		actions = message(actions)
		if run.handoff {
			return actions
		}
		return append(actions, ReviewLoopActionEnd)
	case stewarding(rec) && rec.Held:
		return []string{ReviewLoopActionWatchAgain, ReviewLoopActionEnd}
	case stewarding(rec) && confirmed(loop):
		return []string{ReviewLoopActionStopWatching, ReviewLoopActionEnd}
	case stewarding(rec):
		// In a re-entry's review: End ends the steward wherever it is.
		return []string{ReviewLoopActionEnd}
	case loop.Phase == ReviewPhaseWaitingForAuthor:
		return message([]string{ReviewLoopActionAnotherPass})
	}
	return nil
}

// authorLeftRunning says a loop's author agent still runs after the loop
// was ended stuck, ended, or cancelled, or its run failed: the parent's
// agent.stop stops it only after merged, closed or idle.
func (c *ReviewRunCaller) authorLeftRunning(ctx context.Context, rec *store.ReviewLoop, authorTask string) bool {
	left := false
	if rec.State != store.ReviewLoopOpenState {
		switch rec.Outcome {
		case ReviewOutcomeStuck, ReviewOutcomeEnded, ReviewOutcomeCancelled:
			left = true
		}
	} else if view, err := c.engine.db.GetWorkflowRun(ctx, rec.RunID); err == nil && view.Run.Status == "failed" {
		left = true
	}
	if !left || c.engine.opts.Agents == nil {
		return false
	}
	session, err := c.engine.opts.Agents.GetAgentSession(ctx, authorTask)
	return err == nil && session != nil
}
