package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// The engine's Open and Close over the loop record (spec LEG-559 § Loop
// record schema and claims, § Lifetime; LEG-580). Both callers use them: the
// run caller when the holder's tick opens a github.pr.review_passes step,
// and the handoff caller for a reviewed make-a-change handoff. See
// docs/ai/integrations.md § Review passes › Opening and closing a loop.

// Refusal codes of an open (the github.pr.review.open contract).
const (
	ReviewRefusalLoopActive   = "review_loop_active"
	ReviewRefusalAuthorActive = "author_loop_active"
)

// ReviewLoopActiveError refuses a review loop on a pull request that has an
// active one: one review loop per pull request, whoever started it. LoopID
// is the other loop's, "" for a step still waiting to open; Name and Link
// say whose it is ("run wr-1", "/workflows/runs/wr-1").
type ReviewLoopActiveError struct {
	Repo       string
	PR         int
	Name, Link string
	LoopID     string
}

// Code is the refusal's code, review_loop_active.
func (e *ReviewLoopActiveError) Code() string { return ReviewRefusalLoopActive }

func (e *ReviewLoopActiveError) Error() string {
	return fmt.Sprintf("%s: pull request #%d of %s already has an active review loop: %s (%s)%s",
		e.Code(), e.PR, e.Repo, e.Name, e.Link, loopNamed(e.LoopID))
}

// AuthorLoopActiveError refuses an agent-authored loop whose author task's
// agent already authors an open one: one agent-authored loop per task, so
// its feedback commands find one loop.
type AuthorLoopActiveError struct {
	AuthorTask string
	Name, Link string
	LoopID     string
}

// Code is the refusal's code, author_loop_active.
func (e *AuthorLoopActiveError) Code() string { return ReviewRefusalAuthorActive }

func (e *AuthorLoopActiveError) Error() string {
	return fmt.Sprintf("%s: %s's agent already authors the review loop of %s (%s)%s: one agent-authored loop runs per task",
		e.Code(), e.AuthorTask, e.Name, e.Link, loopNamed(e.LoopID))
}

func loopNamed(id string) string {
	if id == "" {
		return ""
	}
	return ", loop " + id
}

// ReviewRefusalCode is the code of a refused open (review_loop_active,
// author_loop_active), "" for any other error.
func ReviewRefusalCode(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

// reviewLoopName says whose a loop is and where to see it.
func reviewLoopName(l *store.ReviewLoop) (name, link string) {
	switch {
	case l.HandoffID != "" && l.HostTask != "":
		// A handoff's loop, its own or its review-loop run's: the card is
		// where the main and the worker see it.
		return "handed-off task " + l.HostTask, "/board?highlight_task=" + l.HostTask
	case l.OwnerKind == store.ReviewLoopOwnerRun:
		return "run " + l.RunID, "/workflows/runs/" + l.RunID
	case l.OwnerKind == store.ReviewLoopOwnerHandoff:
		return "handed-off task " + l.HostTask, "/board?highlight_task=" + l.HostTask
	}
	return "review loop " + l.ID, ""
}

// refusal types what the loop record refused (*store.ReviewLoopConflictError):
// a pull request another loop holds, an author whose agent authors another
// loop, or a run with another open loop. Any other error is as it was.
func (s *ReviewPassService) refusal(ctx context.Context, err error) error {
	var conflict *store.ReviewLoopConflictError
	if !errors.As(err, &conflict) {
		return err
	}
	holder, getErr := s.db.GetReviewLoop(ctx, conflict.LoopID)
	if getErr != nil {
		return err
	}
	name, link := reviewLoopName(holder)
	switch {
	case errors.Is(err, store.ErrReviewLoopActive):
		return &ReviewLoopActiveError{Repo: conflict.Repo, PR: conflict.PR, Name: name, Link: link, LoopID: holder.ID}
	case errors.Is(err, store.ErrAuthorLoopActive):
		return &AuthorLoopActiveError{AuthorTask: conflict.AuthorTask, Name: name, Link: link, LoopID: holder.ID}
	}
	return fmt.Errorf("%w (%s, %s, loop %s)", err, name, link, holder.ID)
}

// Open opens a review loop on the loop record: the record and the claims on
// its pull requests in one transaction (store.OpenReviewLoop). It is
// idempotent per owner (a run's step, or a handoff): an owner that opened a
// loop before gets it back, created false. On an agent-authored loop, each
// pull request's author is the main slot of the board task it is linked to
// (a task that tracks it on one of its branches), else the loop's author
// task; it is never an input. A refusal is a *ReviewLoopActiveError or an
// *AuthorLoopActiveError naming the other loop; a run with another open
// loop is refused naming it too. Open does no GitHub or agent work: the
// caller discovers the stack first, and the holder's tick reviews.
func (s *ReviewPassService) Open(ctx context.Context, open store.ReviewLoopOpen) (*store.ReviewLoop, bool, error) {
	if open.Loop.AgentAuthor {
		open.PRs = slices.Clone(open.PRs)
		for i := range open.PRs {
			if open.PRs[i].AuthorTask != "" {
				continue
			}
			open.PRs[i].AuthorTask = s.trackedTask(ctx, open.Loop.Repo, open.PRs[i].Number)
			if open.PRs[i].AuthorTask == "" {
				open.PRs[i].AuthorTask = open.Loop.AuthorTask
			}
		}
	}
	loop, created, err := s.db.OpenReviewLoop(ctx, open)
	if err != nil {
		return nil, false, s.refusal(ctx, err)
	}
	if created {
		s.changed(loop.HostTask)
	}
	return loop, created, nil
}

// trackedTask is the board task that tracks repo's pull request on one of
// its branches, "" when none does.
func (s *ReviewPassService) trackedTask(ctx context.Context, repo string, number int) string {
	tracked, err := s.db.ListTrackedTaskBranches(ctx)
	if err != nil {
		return ""
	}
	ids := make([]string, 0, len(tracked))
	for id := range tracked {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		for _, b := range tracked[id] {
			if b.PR != nil && b.PR.PRNumber == number && strings.EqualFold(b.Repo, repo) {
				return id
			}
		}
	}
	return ""
}

// reviewLoopCloser is a caller with side effects of its own when one of its
// loops closes (the run caller removes the reviewer's clone).
type reviewLoopCloser interface {
	loopClosed(ctx context.Context, loop *store.ReviewLoop)
}

// Close closes a loop with its outcome: one transaction releases its
// claims, marks it closed and discards a pass in flight; an approved loop
// keeps its phase, any other ends with the outcome as its reason. Then the
// side effects, which the tick sweeps up if they never happen
// (sweepClosed): the reviewer stops, and each caller's own (the clone
// goes). Closing a closed loop changes nothing (false).
func (s *ReviewPassService) Close(ctx context.Context, loopID, outcome string) (bool, error) {
	phase, reason := ReviewPhaseEnded, outcome
	if outcome == ReviewOutcomeApproved {
		phase, reason = "", ""
	}
	return s.closeLoop(ctx, loopID, phase, reason, outcome)
}

// closeKeepingPhase is Close keeping the loop's phase whatever the outcome:
// a handoff's loop that found nothing to review stays skipped, which its
// Done reads (DoneAllowed).
func (s *ReviewPassService) closeKeepingPhase(ctx context.Context, loopID, outcome string) (bool, error) {
	return s.closeLoop(ctx, loopID, "", "", outcome)
}

// closeLoop closes a loop with outcome, its phase set to phase with reason
// first unless phase is "".
func (s *ReviewPassService) closeLoop(ctx context.Context, loopID, phase, reason, outcome string) (bool, error) {
	loop, closed, err := s.db.EndReviewLoop(ctx, loopID, phase, reason, outcome, s.now())
	if err != nil {
		return false, err
	}
	if !closed {
		return false, nil
	}
	s.closed(ctx, loop)
	s.changed(loop.HostTask)
	return true, nil
}

// closed runs a closed loop's side effects.
func (s *ReviewPassService) closed(ctx context.Context, loop *store.ReviewLoop) {
	if s.opts.Agents != nil && loop.Outcome != ReviewOutcomeApproved {
		// Called inside a tick as well: stop the reviewer without the
		// engine's lock. An approved loop's reviewer finished its pass.
		_ = s.opts.Agents.KillAgentInSlot(ctx, loop.HostTask, SlotReviewer)
	}
	// Not under the engine's lock, which a tick that closes a loop holds:
	// callers are added once, before the engine runs (as loops reads them).
	for _, caller := range s.callers {
		if closer, ok := caller.(reviewLoopCloser); ok {
			closer.loopClosed(ctx, loop)
		}
	}
}

// closedSweepWindow is how far back the sweep looks for closed loops whose
// reviewer may still run.
const closedSweepWindow = 24 * time.Hour

// sweepClosed stops a reviewer still running for a loop that closed (a
// crash between Close's transaction and its side effects): the reviewer
// slot of a task that hosts no open loop, running an execution one of the
// closed loop's passes ran. A later agent in the slot is left alone.
func (s *ReviewPassService) sweepClosed(ctx context.Context) {
	if s.opts.Agents == nil {
		return
	}
	since := s.opts.Now().Add(-closedSweepWindow).UTC().Format(time.RFC3339)
	reviewers, err := s.db.ClosedLoopReviewers(ctx, since)
	if err != nil {
		log.Printf("review passes: the reviewers of closed loops: %v", err)
		return
	}
	for _, r := range reviewers {
		session, err := s.opts.Agents.GetAgentSessionInSlot(ctx, r.HostTask, SlotReviewer)
		if err != nil || session == nil || session.ExecutionID != r.ExecutionID {
			continue
		}
		if err := s.opts.Agents.KillAgentInSlot(ctx, r.HostTask, SlotReviewer); err != nil {
			log.Printf("review passes: stopping the reviewer of closed loop %s: %v", r.LoopID, err)
		}
	}
}

// claimedLoopOn is the loop that claims repo's pull request, as an active
// loop's refusal, unless the task except hosts it; nil when none does.
func (s *ReviewPassService) claimedLoopOn(ctx context.Context, repo string, number int, except string) *ReviewLoopActiveError {
	holder, err := s.db.ActiveReviewLoop(ctx, repo, number)
	if err != nil || holder == nil || (except != "" && holder.HostTask == except) {
		return nil
	}
	name, link := reviewLoopName(holder)
	return &ReviewLoopActiveError{Repo: repo, PR: number, Name: name, Link: link, LoopID: holder.ID}
}
