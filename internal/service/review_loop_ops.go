package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// The review loop engine's operations by loop ID (spec LEG-559 § Modules,
// § The five steps; LEG-582), beside Open and Close
// (review_loop_lifecycle.go), Tick (review_pass.go) and Submit
// (review_pass_submit.go):
//
//   - Pass and Wait (author mode) say where a loop's pass, and its wait for
//     its author, stand. They only read: the holder's tick does every pass
//     and every delivery.
//   - Confirm reads GitHub's heads after a clean pass and records the loop
//     approved only when every head is the clean pass's pin (invariant 9).
//     No caller reports approved before it.
//   - The operator's actions: Another pass, Message the author and End
//     (AnotherPassOn, MessageAuthorOn, EndOn, or ActOnLoop by name).
//   - The author's Feedback and Answer, found by the caller's identity.
//   - ActiveLoop(repo, pr): the loop that holds a pull request.
//
// The inline github.pr.review_passes step (ReviewRunCaller) and the handoff
// caller (HandoffReviewCaller) are orchestrations over these. See
// docs/ai/integrations.md § Review passes › The engine's operations.

// Refusal codes of an operation on a loop by its ID.
const (
	ReviewRefusalLoopNotFound = "loop_not_found"
	ReviewRefusalLoopClosed   = "loop_closed"
)

// ReviewLoopError refuses an operation on a loop by its ID: none has that ID
// (loop_not_found), or it is closed (loop_closed). It unwraps to
// store.ErrNotFound or store.ErrReviewLoopClosed.
type ReviewLoopError struct {
	LoopID string
	code   string
	err    error
}

// Code is the refusal's code.
func (e *ReviewLoopError) Code() string { return e.code }

func (e *ReviewLoopError) Error() string {
	switch e.code {
	case ReviewRefusalLoopNotFound:
		return fmt.Sprintf("%s: there is no review loop %s", e.code, e.LoopID)
	case ReviewRefusalLoopClosed:
		return fmt.Sprintf("%s: review loop %s is closed", e.code, e.LoopID)
	}
	return e.err.Error()
}

func (e *ReviewLoopError) Unwrap() error { return e.err }

// loopByID is the loop with id, refused typed when there is none.
func (s *ReviewPassService) loopByID(ctx context.Context, id string) (*store.ReviewLoop, error) {
	l, err := s.db.GetReviewLoop(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &ReviewLoopError{LoopID: id, code: ReviewRefusalLoopNotFound, err: store.ErrNotFound}
	}
	return l, err
}

// openLoopByID is the open loop with id, refused typed when there is none or
// it is closed.
func (s *ReviewPassService) openLoopByID(ctx context.Context, id string) (*store.ReviewLoop, error) {
	l, err := s.loopByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if l.State != store.ReviewLoopOpenState {
		return nil, &ReviewLoopError{LoopID: id, code: ReviewRefusalLoopClosed, err: store.ErrReviewLoopClosed}
	}
	return l, nil
}

// ActiveLoop is the loop that holds repo's pull request number (the
// repository's case ignored), nil when none does. ActiveLoopOn also counts a
// step still waiting to open.
func (s *ReviewPassService) ActiveLoop(ctx context.Context, repo string, number int) (*store.ReviewLoop, error) {
	return s.db.ActiveReviewLoop(ctx, repo, number)
}

// What a loop's pass is doing (ReviewPassResult.State).
const (
	// ReviewPassPending: the pass is to start (the author is done), waits
	// for a reviewer slot, or runs and has not posted.
	ReviewPassPending = "pending"
	// ReviewPassStatePosted: its reviews are on GitHub; Verdict says what
	// it found.
	ReviewPassStatePosted = "posted"
	// ReviewPassStateStuck: the loop stopped for the operator.
	ReviewPassStateStuck = "stuck"
	// ReviewPassStateNone: there was nothing to review.
	ReviewPassStateNone = "none"
)

// ReviewPassResult is where a loop's pass stands: its number, what it is
// doing, and once posted its verdict (clean or changes).
type ReviewPassResult struct {
	Pass    int    `json:"pass"`
	State   string `json:"state"`
	Verdict string `json:"verdict,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Pass is where the loop's pass stands. It writes nothing: a pass starts
// when the author is done (Wait), and the holder's tick pins its heads,
// starts the reviewer, takes its submission and posts it.
func (s *ReviewPassService) Pass(ctx context.Context, loopID string) (ReviewPassResult, error) {
	rec, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return ReviewPassResult{}, err
	}
	loop, err := s.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil || loop == nil {
		return ReviewPassResult{}, err
	}
	switch loop.Phase {
	case ReviewPhaseAuthor, ReviewPhaseReady, ReviewPhaseWaitingForReviewer:
		return ReviewPassResult{Pass: loop.Pass + 1, State: ReviewPassPending}, nil
	case ReviewPhaseReviewing:
		return ReviewPassResult{Pass: loop.Pass, State: ReviewPassPending}, nil
	case ReviewPhaseStuck:
		return ReviewPassResult{Pass: loop.Pass, State: ReviewPassStateStuck, Reason: loop.Reason}, nil
	case ReviewPhaseSkipped, ReviewPhaseEnded:
		return ReviewPassResult{Pass: loop.Pass, State: ReviewPassStateNone, Reason: loop.Reason}, nil
	}
	pass, err := s.db.GetHandoffReviewPass(ctx, rec.HostTask, loop.Pass)
	if err != nil {
		return ReviewPassResult{}, err
	}
	return ReviewPassResult{Pass: pass.Pass, State: ReviewPassStatePosted, Verdict: pass.Verdict}, nil
}

// Where a loop's wait for its author stands (ReviewWaitResult.State).
const (
	// ReviewWaitPending: the loop waits for its author (feedback is
	// delivered, or about to be).
	ReviewWaitPending = "pending"
	// ReviewWaitReady: the author is done: it pushed, disputed a finding,
	// or there is nothing more to wait for.
	ReviewWaitReady = "ready"
	// ReviewWaitClosed: the pull requests closed, or there was nothing to
	// review.
	ReviewWaitClosed = "closed"
	// ReviewWaitStuck: the loop stopped for the operator (an author that
	// cannot be resumed, say).
	ReviewWaitStuck = "stuck"
)

// ReviewWaitResult is where a loop's wait for its author stands.
type ReviewWaitResult struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// Wait is where the loop's wait for its author stands, in author mode. It
// writes nothing: the holder's tick delivers the items once the author is
// idle (resuming a dead author through its caller), and moves the loop on
// when the author pushed, disputed a blocking finding, or its pull requests
// closed. A person author gets the quiet time.
func (s *ReviewPassService) Wait(ctx context.Context, loopID string) (ReviewWaitResult, error) {
	rec, err := s.loopByID(ctx, loopID)
	if err != nil {
		return ReviewWaitResult{}, err
	}
	if rec.State != store.ReviewLoopOpenState {
		if rec.Outcome == ReviewOutcomeClosed {
			return ReviewWaitResult{State: ReviewWaitClosed}, nil
		}
		return ReviewWaitResult{}, &ReviewLoopError{LoopID: loopID, code: ReviewRefusalLoopClosed, err: store.ErrReviewLoopClosed}
	}
	loop, err := s.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil || loop == nil {
		return ReviewWaitResult{}, err
	}
	switch loop.Phase {
	case ReviewPhaseAuthor, ReviewPhaseWaitingForAuthor:
		return ReviewWaitResult{State: ReviewWaitPending}, nil
	case ReviewPhaseStuck:
		return ReviewWaitResult{State: ReviewWaitStuck, Reason: loop.Reason}, nil
	case ReviewPhaseSkipped, ReviewPhaseEnded:
		return ReviewWaitResult{State: ReviewWaitClosed, Reason: loop.Reason}, nil
	}
	return ReviewWaitResult{State: ReviewWaitReady}, nil
}

// What Confirm found (ReviewConfirmResult.State).
const (
	// ReviewConfirmApproved: every open layer's head on GitHub is the
	// clean pass's pin; the loop is recorded approved.
	ReviewConfirmApproved = "approved"
	// ReviewConfirmMoved: a head is not its pin; the loop goes on to a
	// new pass.
	ReviewConfirmMoved = "moved"
	// ReviewConfirmClosed: no layer is open any more.
	ReviewConfirmClosed = "closed"
)

// ReviewConfirmResult is what Confirm found: the clean pass, and the heads
// GitHub has of its open layers.
type ReviewConfirmResult struct {
	State string         `json:"state"`
	Pass  int            `json:"pass"`
	Heads map[int]string `json:"heads,omitempty"`
}

// ErrReviewNotClean refuses Confirm on a loop whose last pass is not clean.
var ErrReviewNotClean = errors.New("the review loop is not clean")

// Confirm reads GitHub's heads of a clean loop's layers (spec LEG-559
// github.pr.review.confirm, invariant 9). When every open layer's head is
// the clean pass's pin, the loop is recorded approved: confirmed_at and the
// heads, the pass window reset. When one moved, the loop goes on to a new
// pass. It does not wait for pending checks. GitHub not answering is an
// error, and the loop stays unconfirmed (confirm_clean) for the tick to
// try again: nothing is approved meanwhile. It reads GitHub, so the holder
// calls it.
func (s *ReviewPassService) Confirm(ctx context.Context, loopID string) (ReviewConfirmResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.confirm(ctx, loopID)
}

// confirm is Confirm with the engine's lock held.
func (s *ReviewPassService) confirm(ctx context.Context, loopID string) (ReviewConfirmResult, error) {
	rec, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return ReviewConfirmResult{}, err
	}
	loop, err := s.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil {
		return ReviewConfirmResult{}, err
	}
	if loop == nil || loop.LoopID != rec.ID || loop.Phase != ReviewPhaseClean || loop.Pass == 0 {
		return ReviewConfirmResult{}, fmt.Errorf("%w: review loop %s", ErrReviewNotClean, loopID)
	}
	pins, err := s.db.ListHandoffReviewPRs(ctx, rec.HostTask, loop.Pass)
	if err != nil {
		return ReviewConfirmResult{}, err
	}
	out := ReviewConfirmResult{Pass: loop.Pass, Heads: map[int]string{}}
	moved := false
	for _, pin := range pins {
		repo := pin.Repo
		if repo == "" {
			repo = rec.Repo
		}
		pr, state, err := s.opts.GitHub.PullRequest(ctx, repo, pin.Number)
		if err != nil {
			if !loop.ConfirmClean {
				loop.ConfirmClean = true
				if saveErr := s.save(ctx, loop); saveErr != nil {
					return ReviewConfirmResult{}, saveErr
				}
			}
			return ReviewConfirmResult{}, fmt.Errorf("reading the head of #%d: %w", pin.Number, err)
		}
		if state != "open" {
			continue // closed or merged: no longer reviewed
		}
		out.Heads[pin.Number] = pr.HeadSHA
		moved = moved || pr.HeadSHA != pin.HeadSHA
	}
	switch {
	case len(out.Heads) == 0:
		out.State = ReviewConfirmClosed
		loop.ConfirmClean = false
		return out, s.save(ctx, loop)
	case moved:
		// A push between the clean pass's posts and now: a new pass reviews
		// it (the moved-head rule, D2).
		out.State = ReviewConfirmMoved
		loop.Phase, loop.Reason, loop.ConfirmClean = ReviewPhaseReady, "", false
		return out, s.saveChanged(ctx, loop)
	}
	if err := s.db.ConfirmReviewLoop(ctx, rec.ID, loop.Pass, out.Heads, s.now()); err != nil {
		return ReviewConfirmResult{}, err
	}
	out.State = ReviewConfirmApproved
	return out, nil
}

// confirmed says a loop is clean and confirmed at GitHub's heads: approved
// (invariant 9). Only then may a caller report it approved. The record
// says so (Confirm's confirmed_at at this pass), closed or not: a loop
// closed while clean is not approved unless Confirm found its heads first
// (a run cancelled, or a handoff closed, between the clean post and
// Confirm; a loop the conversion closed so).
func confirmed(loop *store.HandoffReview) bool {
	return loop != nil && loop.Phase == ReviewPhaseClean && !loop.ConfirmClean && loop.ConfirmedAt != "" && loop.CleanPass == loop.Pass
}

// confirmLoop runs Confirm on a clean loop in the tick and tells its caller
// what it found: approved (ReviewStageClean), nothing left open (the loop
// is skipped, ReviewStageNone), or moved (the next pass starts on the
// tick). GitHub not answering is logged once and tried again next tick.
// Call with s.mu held.
func (s *ReviewPassService) confirmLoop(ctx context.Context, l ReviewLoop, loopID string) error {
	result, err := s.confirm(ctx, loopID)
	if err != nil {
		if errors.Is(err, ErrReviewNotClean) || errors.Is(err, store.ErrReviewLoopNotClean) {
			return nil // it moved on meanwhile
		}
		var refused *ReviewLoopError
		if errors.As(err, &refused) {
			return nil // closed meanwhile
		}
		s.setWaiting(l.TaskID, ReviewWaitingOnGitHub)
		s.logConfirmError(l, err)
		return nil
	}
	s.setWaiting(l.TaskID, "")
	s.logGitHubError(l.TaskID, nil)
	switch result.State {
	case ReviewConfirmApproved:
		s.changed(l.TaskID)
		l.stage(ctx, ReviewStageClean)
	case ReviewConfirmClosed:
		loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
		if err != nil || loop == nil {
			return err
		}
		loop.Phase = ReviewPhaseSkipped
		if err := s.saveChanged(ctx, loop); err != nil {
			return err
		}
		l.stage(ctx, ReviewStageNone)
	}
	return nil
}

// confirmAgain makes a clean loop's caller wait for another Confirm (on the
// next tick) before it reports the loop approved: what it needs before then
// (an APPROVE posted, a check settled) did not happen yet. A loop that is no
// longer clean is left alone.
func (s *ReviewPassService) confirmAgain(ctx context.Context, taskID string) {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil || loop.Phase != ReviewPhaseClean || loop.ConfirmClean {
		return
	}
	loop.ConfirmClean = true
	if err := s.save(ctx, loop); err != nil {
		log.Printf("review passes: %s: %v", taskID, err)
	}
}

// logConfirmError logs a loop's failed confirm once, as GitHub errors are.
func (s *ReviewPassService) logConfirmError(l ReviewLoop, err error) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	if s.lastErr == nil {
		s.lastErr = map[string]string{}
	}
	if s.lastErr[l.TaskID] == err.Error() {
		return
	}
	s.lastErr[l.TaskID] = err.Error()
	log.Printf("review passes: confirming the heads of %s's clean pass, trying again: %v", l.Name, err)
}

// Review loop actions an operator takes by loop ID (ActOnLoop).
const (
	ReviewLoopActionAnotherPass = "another_pass"
	ReviewLoopActionMessage     = "message"
	ReviewLoopActionEnd         = "end"
)

// reviewLoopActor is a caller that carries out the operator's actions on
// its own kind of loop: what Another pass, Message the author and End mean
// to it. Each only writes the record, or queues for the agent, as the
// engine's own actions do; the holder's tick does the work.
type reviewLoopActor interface {
	ownsLoop(l *store.ReviewLoop) bool
	anotherPassOn(ctx context.Context, l *store.ReviewLoop) error
	messageAuthorOn(ctx context.Context, l *store.ReviewLoop, text string) error
	endOn(ctx context.Context, l *store.ReviewLoop) error
}

// actorFor is the caller that acts on a loop.
func (s *ReviewPassService) actorFor(l *store.ReviewLoop) (reviewLoopActor, error) {
	// Not under the engine's lock: callers are added once, before the
	// engine runs (as loops reads them).
	for _, caller := range s.callers {
		if actor, ok := caller.(reviewLoopActor); ok && actor.ownsLoop(l) {
			return actor, nil
		}
	}
	return nil, fmt.Errorf("this Legato does not run %s review loops", l.OwnerKind)
}

// ActOnLoop takes an operator's action on a loop by its ID: another_pass,
// message (with text) or end.
func (s *ReviewPassService) ActOnLoop(ctx context.Context, loopID, action, text string) error {
	switch action {
	case ReviewLoopActionAnotherPass:
		return s.AnotherPassOn(ctx, loopID)
	case ReviewLoopActionMessage:
		return s.MessageAuthorOn(ctx, loopID, text)
	case ReviewLoopActionEnd:
		return s.EndOn(ctx, loopID)
	}
	return fmt.Errorf("a review loop takes no action %q", action)
}

// AnotherPassOn is Run another pass on a loop by its ID: one more pass of a
// stuck loop (past its limit, or again after a reviewer that could not
// review), or, on a run's loop that waits for its author, the next pass at
// once. It only writes the record; the tick starts the pass.
func (s *ReviewPassService) AnotherPassOn(ctx context.Context, loopID string) error {
	l, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return err
	}
	actor, err := s.actorFor(l)
	if err != nil {
		return err
	}
	return actor.anotherPassOn(ctx, l)
}

// MessageAuthorOn sends a loop's agent author the operator's words, tagged
// and queued as feedback is; a dead author is resumed first. A person
// author reads GitHub, so there is no one to message.
func (s *ReviewPassService) MessageAuthorOn(ctx context.Context, loopID, text string) error {
	l, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return err
	}
	actor, err := s.actorFor(l)
	if err != nil {
		return err
	}
	return actor.messageAuthorOn(ctx, l, text)
}

// EndOn ends a stuck loop by its ID with outcome stuck: a run's step
// completes, and the workflow goes on. A handoff's loop ends with its
// handoff (Return or Recall), not here.
func (s *ReviewPassService) EndOn(ctx context.Context, loopID string) error {
	l, err := s.openLoopByID(ctx, loopID)
	if err != nil {
		return err
	}
	actor, err := s.actorFor(l)
	if err != nil {
		return err
	}
	return actor.endOn(ctx, l)
}

// Feedback is the author's feedback on the loop it authors, found from its
// session (ReviewAnswerer): the handoff card's, or the run loop its task's
// agent authors.
func (s *ReviewPassService) Feedback(ctx context.Context, who ReviewAnswerer, opts ...ReviewFeedbackOptions) (ReviewFeedbackView, error) {
	return OwnReviewFeedback(ctx, s.db, who, opts...)
}

// Answer records the author's answer to an item of the loop it authors,
// found from its session, and returns the loop's host task.
func (s *ReviewPassService) Answer(ctx context.Context, who ReviewAnswerer, id, answer, note string) (string, error) {
	return AnswerOwnReviewItem(ctx, s.db, who, id, answer, note, s.opts.Now())
}
