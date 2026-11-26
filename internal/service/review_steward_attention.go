package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// What a run's review loop needs of the operator, as the waiting step's
// Attention item (spec LEG-559 § The steward › Surfaces, § Approval after
// re-entry; LEG-593):
//
//   - stuck (workflow_review_stuck): past its pass limit, not converging,
//     a reviewer that died twice, or, for a steward, past its re-entry
//     budget; an author agent that cannot be resumed is stuck too, and its
//     item names what waits for it;
//   - waiting on an author that will not move it alone
//     (workflow_review_author, review_pass_run_author.go);
//   - and, while its steward watches (workflow_review_steward): held
//     (Stop watching), or a person dismissed legato-reviews' approval with
//     no push, which Legato never answers with an approval of its own.
//
// A steward's stuck and held items say whether legato-reviews' approval
// still stands on GitHub, and at which head. A steward that ended idle
// leaves a notice for a week (stewardNotices).

// stewardIdleNotice is how long the notice of a steward that ended idle
// stays, unless the operator dismisses it.
const stewardIdleNotice = 7 * 24 * time.Hour

// authorUnresumable is what the reason of a loop stuck on an author agent
// it could not bring back says (ResumeAuthor, ErrCannotResumeAuthor).
const authorUnresumable = "cannot be resumed"

// need is why the loop a run task hosts needs the operator, and the actions
// it offers, the first the primary.
func (c *ReviewRunCaller) need(ctx context.Context, taskID string) WorkflowStepNeed {
	loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return WorkflowStepNeed{}
	}
	run, err := c.run(ctx, taskID)
	if err != nil {
		return WorkflowStepNeed{}
	}
	if run.handoff {
		// A handoff's loop ends with its handoff, never by End.
		if loop.Phase == ReviewPhaseStuck {
			return WorkflowStepNeed{Reason: "Review loop stuck: " + loop.Reason, Signal: AttentionSignalWorkflowReviewAuthor,
				Actions: []string{ReviewLoopActionAnotherPass, ReviewLoopActionMessage}}
		}
		return WorkflowStepNeed{}
	}
	agent := run.AuthorTask != ""
	if rec, err := c.engine.db.CurrentReviewLoop(ctx, taskID); err == nil && stewarding(rec) {
		return c.stewardNeed(ctx, rec, loop, run)
	}
	switch loop.Phase {
	case ReviewPhaseStuck:
		need := WorkflowStepNeed{Reason: c.stuckReason(ctx, "Review loop stuck: ", loop), Signal: AttentionSignalWorkflowReviewStuck,
			Actions: []string{ReviewLoopActionAnotherPass, ReviewLoopActionEnd}}
		if agent && !strings.Contains(loop.Reason, authorUnresumable) {
			need.Actions = append(need.Actions, ReviewLoopActionMessage)
		}
		return need
	case ReviewPhaseWaitingForAuthor:
		return c.authorNeed(ctx, taskID, run, loop, false)
	}
	return WorkflowStepNeed{}
}

// authorNeed is a loop waiting on an author that will not move it alone,
// nothing while its agent author works.
func (c *ReviewRunCaller) authorNeed(ctx context.Context, taskID string, run *reviewRun, loop *store.HandoffReview, steward bool) WorkflowStepNeed {
	state := c.authorState(ctx, run, loop)
	if !c.authorNeedsYou(ctx, taskID, run, state) {
		return WorkflowStepNeed{}
	}
	need := WorkflowStepNeed{Reason: authorReason(state), Signal: AttentionSignalWorkflowReviewAuthor, Steward: steward,
		Actions: []string{ReviewLoopActionAnotherPass}}
	if run.AuthorTask != "" {
		need.Actions = append(need.Actions, ReviewLoopActionMessage)
	}
	return need
}

// stewardNeed is what a stewarded loop needs: stuck (at its watch, Watch
// again; in a re-entry's review, Another pass), its approval dismissed by
// a person, held, or a re-entry's author that will not move.
func (c *ReviewRunCaller) stewardNeed(ctx context.Context, rec *store.ReviewLoop, loop *store.HandoffReview, run *reviewRun) WorkflowStepNeed {
	agent := run.AuthorTask != ""
	stands := c.engine.approvalStandsText(ctx, rec)
	switch {
	case loop.Phase == ReviewPhaseStuck:
		unresumable := strings.Contains(loop.Reason, authorUnresumable)
		need := WorkflowStepNeed{Reason: joinSentences(c.stuckReason(ctx, "Steward stuck: ", loop), stands),
			Signal: AttentionSignalWorkflowReviewStuck, Steward: true}
		if approvalStands(loop) {
			// Stuck at its watch: Watch again lets it go on (one more
			// re-entry when it is the budget).
			need.Actions = []string{ReviewLoopActionWatchAgain}
		} else {
			need.Actions = []string{ReviewLoopActionAnotherPass}
		}
		if agent && !unresumable {
			need.Actions = append(need.Actions, ReviewLoopActionMessage)
		}
		need.Actions = append(need.Actions, ReviewLoopActionEnd)
		return need
	case confirmed(loop):
		if dismissed := c.engine.dismissedApprovals(ctx, rec); len(dismissed) > 0 {
			need := WorkflowStepNeed{Signal: AttentionSignalWorkflowReviewSteward, Steward: true,
				Reason: joinSentences(fmt.Sprintf("A person dismissed legato-reviews' approval of %s without a push. "+
					"Legato does not approve it again by itself: a push goes back to review, and a clean pass at the new head approves it", strings.Join(dismissed, " and ")), stands)}
			if rec.Held {
				need.Actions = append(need.Actions, ReviewLoopActionWatchAgain)
			}
			if agent {
				need.Actions = append(need.Actions, ReviewLoopActionMessage)
			}
			need.Actions = append(need.Actions, ReviewLoopActionEnd)
			return need
		}
		if rec.Held {
			return WorkflowStepNeed{Signal: AttentionSignalWorkflowReviewSteward, Steward: true,
				Reason:  joinSentences("Steward held (Stop watching): it sends nothing and starts no pass until Watch again; a merge, a close or seven quiet days still end it", stands),
				Actions: []string{ReviewLoopActionWatchAgain, ReviewLoopActionEnd}}
		}
	case loop.Phase == ReviewPhaseWaitingForAuthor:
		return c.authorNeed(ctx, rec.HostTask, run, loop, true)
	}
	return WorkflowStepNeed{}
}

// stuckReason is a stuck loop's reason after prefix; when its author agent
// cannot be resumed, it names what waits for the author.
func (c *ReviewRunCaller) stuckReason(ctx context.Context, prefix string, loop *store.HandoffReview) string {
	reason := prefix + loop.Reason
	if !strings.Contains(loop.Reason, authorUnresumable) {
		return reason
	}
	return joinSentences(reason, "Waiting for it: "+c.engine.waitingForAuthor(ctx, loop)+
		". Start the author's agent, then let the loop go on, or end it")
}

// waitingForAuthor names what a loop's author has to fix or answer: its
// open items by kind, or the review's next round when it has none.
func (s *ReviewPassService) waitingForAuthor(ctx context.Context, loop *store.HandoffReview) string {
	items, err := currentItems(ctx, s.db, loop)
	if err != nil {
		return "the review's next round"
	}
	counts := map[string]int{}
	for _, item := range items {
		if item.Answer == "" {
			counts[item.Kind]++
		}
	}
	var parts []string
	for _, kind := range []struct{ key, one, many string }{
		{ItemBlocking, "blocking finding", "blocking findings"}, {ItemNit, "nit", "nits"},
		{ItemComment, "new comment", "new comments"}, {ItemCheck, "failing check", "failing checks"},
		{ItemMergeability, "mergeability problem", "mergeability problems"}, {ItemMergedLayer, "merged layer", "merged layers"},
	} {
		if n := counts[kind.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, kind.one, kind.many)))
		}
	}
	if len(parts) == 0 {
		return "the review's next round"
	}
	return joinAnd(parts)
}

// approvalStandsText says which of a stewarded loop's approvals still
// stand on GitHub: legato-reviews' APPROVE (or the approval a pass kept) of
// each open layer at the head Confirm approved it at, unless a person
// dismissed it. "legato-reviews' approval at <sha> still stands" for one
// layer; "" when none stands. LEG-594's run page says the same.
func (s *ReviewPassService) approvalStandsText(ctx context.Context, rec *store.ReviewLoop) string {
	if rec.CleanPass == 0 {
		return ""
	}
	layers, err := s.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return ""
	}
	events := map[int]string{}
	if pins, err := s.db.ListHandoffReviewPRs(ctx, rec.HostTask, rec.CleanPass); err == nil {
		for _, pin := range pins {
			events[pin.Number] = pin.Event
		}
	}
	w, read := s.freshWatch(rec.ID)
	var stands []store.ReviewLoopPR
	for _, layer := range layers {
		state := layer.State
		var live *PRWatch
		if read {
			for i := range w.Layers {
				if w.Layers[i].Number == layer.Number {
					live = &w.Layers[i]
					state = live.State
				}
			}
		}
		event := events[layer.Number]
		switch {
		case layer.CleanHead == "" || state != "open":
		case event != ReviewApprove && event != ReviewKept:
		case live != nil && layer.ReviewID != 0 && slices.Contains(live.DismissedReviews, layer.ReviewID):
		default:
			stands = append(stands, layer)
		}
	}
	switch len(stands) {
	case 0:
		return ""
	case 1:
		if len(layers) == 1 {
			return fmt.Sprintf("legato-reviews' approval at %s still stands", short(stands[0].CleanHead))
		}
		return fmt.Sprintf("legato-reviews' approval of #%d at %s still stands", stands[0].Number, short(stands[0].CleanHead))
	}
	names := make([]string, 0, len(stands))
	for _, layer := range stands {
		names = append(names, fmt.Sprintf("#%d at %s", layer.Number, short(layer.CleanHead)))
	}
	return "legato-reviews' approvals of " + joinAnd(names) + " still stand"
}

// dismissedApprovals names the open layers of a stewarded loop whose
// approval a person dismissed with no push ("#12 at bbb222"): the newest
// watch read finds the head still the one Confirm approved, and the review
// Legato posted there among the dismissed ones. A push that dismissed it is
// a re-entry instead, which a clean pass answers with a fresh APPROVE.
func (s *ReviewPassService) dismissedApprovals(ctx context.Context, rec *store.ReviewLoop) []string {
	w, ok := s.freshWatch(rec.ID)
	if !ok {
		return nil
	}
	layers, err := s.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return nil
	}
	var out []string
	for _, live := range w.Layers {
		for _, layer := range layers {
			if layer.Number == live.Number && live.State == "open" && layer.CleanHead != "" && live.HeadSHA == layer.CleanHead &&
				layer.ReviewID != 0 && slices.Contains(live.DismissedReviews, layer.ReviewID) {
				out = append(out, fmt.Sprintf("#%d at %s", layer.Number, short(layer.CleanHead)))
			}
		}
	}
	return out
}

// stewardNotices are the notices of the run loops whose steward ended idle
// in the last week.
func (c *ReviewRunCaller) stewardNotices(ctx context.Context) []WorkflowNotice {
	since := c.engine.opts.Now().Add(-stewardIdleNotice).UTC().Format(time.RFC3339)
	loops, err := c.engine.db.ListReviewLoopsClosedSince(ctx, StewardWokeIdle, since)
	if err != nil {
		return nil
	}
	var out []WorkflowNotice
	for _, rec := range loops {
		if !rec.Steward {
			continue
		}
		layers, _ := c.engine.db.ListReviewLoopPRs(ctx, rec.ID)
		numbers := make([]int, 0, len(layers))
		names := make([]string, 0, len(layers))
		for _, layer := range layers {
			numbers = append(numbers, layer.Number)
			names = append(names, fmt.Sprintf("#%d", layer.Number))
		}
		out = append(out, WorkflowNotice{RunID: rec.RunID, StepID: rec.StepPath, Signal: AttentionSignalWorkflowReviewSteward,
			Revision: "idle:" + rec.ClosedAt, Title: "Review loop on " + reviewStackName(rec.Repo, numbers),
			Reason: fmt.Sprintf("The steward ended idle: seven days with no push, comment, review or check change on %s. Nothing watches it now.", joinAnd(names))})
	}
	return out
}

// joinSentences joins two sentences, either of which may be empty, and
// ends the result with a full stop. b keeps its case: it may open with a
// name (legato-reviews).
func joinSentences(a, b string) string {
	a, b = strings.TrimRight(strings.TrimSpace(a), "."), strings.TrimRight(strings.TrimSpace(b), ".")
	switch {
	case a == "":
		return b + "."
	case b == "":
		return a + "."
	}
	return a + ". " + b + "."
}
