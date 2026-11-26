package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// HandoffReviewBlock is a handoff's review as its summary row says it.
type HandoffReviewBlock = ReviewLoopBlock

// HandoffReviewCallerOptions are what the handoff caller drives.
type HandoffReviewCallerOptions struct {
	// Board moves a reviewed card between its columns.
	Board interface {
		MoveCard(ctx context.Context, id, column string) error
	}
	// ResumeAuthor brings back an author that is not running
	// (HandoffWorkerService.ResumeAuthor); nil cannot.
	ResumeAuthor func(ctx context.Context, taskID string) error
	// Bus hears that a handoff's review changed; nil for none.
	Bus *events.Bus
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// FetchPR fetches a pull request's head into the repository's cache
	// (RepositoryService.PullHead), which the job clone borrows objects
	// from: a layer of the stack the author's clone has no branch of is
	// reviewed at its pinned head. Nil fetches nothing.
	FetchPR func(ctx context.Context, repo string, number int) error
	// Runs starts and cancels each reviewed handoff's review-loop run
	// (WorkflowService; spec LEG-559 D10). A handoff's review runs only in
	// one: without Runs, review passes apply to no handoff.
	Runs HandoffReviewRuns
	// PullRequests says, from this worker's pull_requests.rules, whether a
	// handoff's repository marks its approved stack ready (a draft rule
	// with ready_after_review): the run's ready input (LEG-456). Nil leaves
	// every stack a draft.
	PullRequests ReadyAfterReviewSource
}

// HandoffReviewRuns are the workflow runs a handoff's review runs in: the
// built-in review-loop, started with the handoff binding, and cancelled when
// the handoff ends (WorkflowService).
type HandoffReviewRuns interface {
	StartHandoffReviewLoop(ctx context.Context, req HandoffReviewLoopStart) (*store.WorkflowRunView, error)
	Cancel(ctx context.Context, runID, reason string) (*store.WorkflowRunView, error)
}

// HandoffReviewCaller is the review-pass engine's caller for a worker's
// make-a-change handoffs: a loop per open handoff whose review passes are on,
// keyed on the card. The card's column drives it: Done means the author is
// done, a pass moves the card to the review column, feedback to the work
// column, a clean pass to Done, and a handoff is done only after a clean
// review (DoneAllowed). Comments from people and bots and counted checks are
// items the author handles too. It is what the handoff side asks of review
// passes (HandoffReviews).
//
// Each handoff's loop belongs to a review-loop run the caller starts
// (startRun, LEG-600): the run's steps follow the loop, and this caller
// moves it on as a handoff's, under the binding the run carries. No loop
// moves on outside such a run: one its handoff owns (every handoff loop
// opened before LEG-600, converted or not) is adopted by a review-loop run
// in the holder's first tick (sweepLoops, LEG-601), and a record a handoff
// owns otherwise only keeps a main's "review passes off" for it.
type HandoffReviewCaller struct {
	*ReviewPassService
	opts HandoffReviewCallerOptions
}

// NewHandoffReviewCaller registers the handoff caller with the engine.
func NewHandoffReviewCaller(engine *ReviewPassService, opts HandoffReviewCallerOptions) *HandoffReviewCaller {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &HandoffReviewCaller{ReviewPassService: engine, opts: opts}
	engine.AddCaller(c)
	return c
}

// applies says whether a handoff runs review passes: a make-a-change one,
// unless its main turned them off for it, on a worker that runs review-loop
// (Runs).
func (c *HandoffReviewCaller) applies(h *store.Handoff) bool {
	if c.opts.Runs == nil || !c.config().Enabled || h.Purpose != HandoffMakeChange || h.ClosedAt != nil {
		return false
	}
	loop, err := c.db.GetHandoffReview(context.Background(), h.TaskID)
	if err != nil {
		return false
	}
	return loopSettings(loop, c.config().Defaults).Enabled
}

// handoffLoop is a handoff's loop as the engine moves it on: rec is the
// card's loop record, which a review-loop run owns under the handoff
// binding, so it writes to GitHub for that run (and its handoff,
// CheckReviewLoopWrite).
func handoffLoop(h *store.Handoff, rec *store.ReviewLoop) ReviewLoop {
	return ReviewLoop{TaskID: h.TaskID, Repo: h.Repo, AuthorTaskID: h.TaskID, CountComments: true, CountChecks: true,
		Name: "handed-off task " + h.TaskID, Link: "/board?highlight_task=" + h.TaskID,
		Owner: store.HandoffReviewOwner{RunID: rec.RunID}}
}

// openLoop opens the loop of a card's handoff when review passes are on for
// it: its review-loop run (startRun). With them off it opens nothing; the
// card's record that keeps them off is written with the settings.
func (c *HandoffReviewCaller) openLoop(ctx context.Context, h *store.Handoff, enabled bool) error {
	if !enabled || c.opts.Runs == nil {
		return nil
	}
	return c.startRun(ctx, h)
}

// startRun starts the handoff's review-loop run with its binding (spec
// LEG-559 D10): a top-level run of the built-in, related to the card, no
// steward, and a loop record the run's open step adopts, which names the
// handoff and the card's columns. Its ready input is the worker's rule for
// the repository (ready_after_review on a draft rule, LEG-456). A handoff
// that has a loop already (its run's, or one it owns, which adoptLoops
// gives a run) starts none.
func (c *HandoffReviewCaller) startRun(ctx context.Context, h *store.Handoff) error {
	if l, err := c.db.CurrentReviewLoop(ctx, h.TaskID); err != nil {
		return err
	} else if l != nil && (l.State == store.ReviewLoopOpenState || (l.HandoffID == h.HandoffID && l.MainInstanceID == h.MainInstanceID)) {
		return nil
	}
	columns := handoffColumnsJSON(c.config())
	of := store.HandoffOfCard{MainInstanceID: h.MainInstanceID, HandoffID: h.HandoffID, Repo: h.Repo}
	ready := c.ready(ctx, h)
	_, err := c.opts.Runs.StartHandoffReviewLoop(ctx, c.runStart(ctx, h, func(ctx context.Context, runID string) error {
		open := store.HandoffRunReviewLoopOpen(h.TaskID, of, runID, ReviewLoopOpenStepID, columns)
		// As the open step records a run's ready: the author is told the
		// run marks its pull requests ready.
		open.Loop.Ready = ready
		_, _, err := c.Open(ctx, open)
		return err
	}))
	return err
}

// ready says whether the handoff's repository marks its approved stack
// ready: this worker's pull_requests.rules, a draft rule with
// ready_after_review (LEG-456).
func (c *HandoffReviewCaller) ready(ctx context.Context, h *store.Handoff) bool {
	return c.opts.PullRequests != nil && c.opts.PullRequests.ReadyAfterReview(ctx, h.Repo)
}

// runStart is the review-loop run of a handoff, with bind writing its
// binding: what a new handoff's run (startRun) and an adopting one
// (adoptLoop) both start. Its ready input is the worker's rule (ready).
func (c *HandoffReviewCaller) runStart(ctx context.Context, h *store.Handoff, bind func(ctx context.Context, runID string) error) HandoffReviewLoopStart {
	return HandoffReviewLoopStart{Repo: h.Repo, Card: h.TaskID, Ready: c.ready(ctx, h), Bind: bind}
}

// adoptLoop gives a loop the handoff owns (opened before LEG-600) to a new
// review-loop run of the handoff (spec LEG-559 § Handoffs, adoption): the
// run's binding is the loop itself, its owner changed from the handoff to
// the run in one transaction (store.AdoptHandoffReviewLoop), its claims,
// passes, rulings and items kept. A pass in flight goes on, and the run's
// pass step reads it when it is posted.
func (c *HandoffReviewCaller) adoptLoop(ctx context.Context, h *store.Handoff, l *store.ReviewLoop) error {
	columns := handoffColumnsJSON(c.config())
	_, err := c.opts.Runs.StartHandoffReviewLoop(ctx, c.runStart(ctx, h, func(ctx context.Context, runID string) error {
		_, err := c.db.AdoptHandoffReviewLoop(ctx, l.ID, runID, ReviewLoopOpenStepID, columns)
		return err
	}))
	return err
}

// StartReview starts the review of a card's handoff when review passes
// apply to it: at accept, so its comments and checks count from then on.
func (c *HandoffReviewCaller) StartReview(ctx context.Context, taskID string) error {
	h, err := c.db.OpenHandoffForTask(ctx, taskID)
	if err != nil || !c.applies(h) {
		return err
	}
	return c.openLoop(ctx, h, true)
}

// SetSettings opens the loop of a card's handoff (openLoop) and keeps its
// review settings (a main's override, or the worker's defaults) on it.
func (c *HandoffReviewCaller) SetSettings(ctx context.Context, taskID string, settings ReviewPassSettings) error {
	if h, err := c.db.OpenHandoffForTask(ctx, taskID); err == nil {
		if err := c.openLoop(ctx, h, settings.Enabled && c.config().Enabled && h.Purpose == HandoffMakeChange); err != nil {
			return err
		}
	}
	return c.ReviewPassService.SetSettings(ctx, taskID, settings)
}

// OpenWaiting, in the holder's tick: the loops handoffs own are adopted by
// review-loop runs, and those of ended handoffs closed, and the review-loop
// run of a handoff that ended where its end never reached a run cancelled
// (sweepLoops); then each reviewed handoff whose card has no loop of its
// own yet gets its run.
func (c *HandoffReviewCaller) OpenWaiting(ctx context.Context) {
	c.sweepLoops(ctx)
	open, err := c.db.ListOpenHandoffs(ctx)
	if err != nil {
		log.Printf("review passes: %v", err)
		return
	}
	for i := range open {
		h := &open[i]
		if !c.reviewed(h) {
			continue
		}
		// A card with an open loop (its own, or one converted before its
		// handoff was named on it) or a closed loop of this handoff has its
		// loop.
		l, err := c.db.CurrentReviewLoop(ctx, h.TaskID)
		if err != nil || (l != nil && (l.State == store.ReviewLoopOpenState || (l.HandoffID == h.HandoffID && l.MainInstanceID == h.MainInstanceID))) {
			continue
		}
		if err := c.openLoop(ctx, h, true); err != nil {
			log.Printf("review passes: opening the loop of handoff %s: %v", h.HandoffID, err)
		}
	}
}

// StopReview ends a card's review (the engine's StopReview: the reviewer
// stops, a pass in flight is discarded) and cancels the review-loop run its
// loop belongs to: Recall, Drop, Leave, unpair and an allowed Return. The
// run's end closes the loop and lets go of its pull requests. A run that
// ended already (approved) changes nothing.
func (c *HandoffReviewCaller) StopReview(ctx context.Context, taskID string) {
	c.ReviewPassService.StopReview(ctx, taskID)
	l, err := c.db.CurrentReviewLoop(ctx, taskID)
	if err != nil || !handoffBound(l) || l.State != store.ReviewLoopOpenState {
		return
	}
	c.cancelRun(ctx, l, "its handoff ended")
}

// cancelRun cancels the review-loop run of a handoff's loop.
func (c *HandoffReviewCaller) cancelRun(ctx context.Context, l *store.ReviewLoop, why string) {
	if c.opts.Runs == nil {
		return
	}
	if _, err := c.opts.Runs.Cancel(ctx, l.RunID, why); err != nil {
		log.Printf("review passes: cancelling review run %s of %s: %v", l.RunID, l.HostTask, err)
	}
}

// sweepLoops goes through the open loops of handoffs. A review-loop run's
// whose handoff has closed, is gone or is on its way back has its run
// cancelled: the sweep behind StopReview, for an end in a process that
// could not cancel the run. One its handoff owns is adopted by a review-loop
// run while its handoff is open, reviewed and not on its way back
// (adoptLoop: at the holder's start, the loops an upgrade found), and is
// closed, ended, once its handoff has closed or is gone, freeing its pull
// requests. Nothing else moves a loop a handoff owns.
func (c *HandoffReviewCaller) sweepLoops(ctx context.Context) {
	if c.opts.Runs == nil {
		return
	}
	loops, err := c.db.ListOpenReviewLoops(ctx)
	if err != nil {
		return
	}
	for i := range loops {
		l := &loops[i]
		if l.HandoffID == "" || (l.OwnerKind != store.ReviewLoopOwnerHandoff && !handoffBound(l)) {
			continue
		}
		h, err := c.db.OpenHandoffForTask(ctx, l.HostTask)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			continue
		}
		ended := err != nil || h.HandoffID != l.HandoffID || (l.MainInstanceID != "" && h.MainInstanceID != l.MainInstanceID)
		switch {
		case handoffBound(l):
			if ended || h.State == store.HandoffReturning {
				c.ReviewPassService.StopReview(ctx, l.HostTask)
				c.cancelRun(ctx, l, "its handoff ended")
			}
		case ended:
			if _, _, err := c.db.CloseReviewLoop(ctx, l.ID, ReviewOutcomeEnded, c.now()); err != nil {
				log.Printf("review passes: closing loop %s of an ended handoff: %v", l.ID, err)
			}
		case h.State != store.HandoffReturning && c.applies(h):
			if err := c.adoptLoop(ctx, h, l); err != nil {
				log.Printf("review passes: adopting loop %s of handoff %s: %v", l.ID, l.HandoffID, err)
			}
		}
	}
}

// waitingToOpenOn: a handoff's loop opens at once, with no pull request.
func (c *HandoffReviewCaller) waitingToOpenOn(context.Context, string, int, string) *ReviewLoopActiveError {
	return nil
}

// reviewed says an open handoff's loop moves on: review passes apply and
// its agent has started.
func (c *HandoffReviewCaller) reviewed(h *store.Handoff) bool {
	return c.applies(h) && h.State != store.HandoffPending && h.State != store.HandoffFailed && h.State != store.HandoffReturning
}

// Loops are the open handoffs whose review passes are on and whose agent
// has started, each with the open loop of its review-loop run.
func (c *HandoffReviewCaller) Loops(ctx context.Context) ([]ReviewLoop, error) {
	open, err := c.db.ListOpenHandoffs(ctx)
	if err != nil {
		log.Printf("review passes: %v", err)
		return nil, err
	}
	var out []ReviewLoop
	for i := range open {
		h := &open[i]
		if !c.reviewed(h) {
			continue
		}
		rec, err := c.db.CurrentReviewLoop(ctx, h.TaskID)
		if err != nil || !c.movesOn(rec) {
			continue
		}
		out = append(out, handoffLoop(h, rec))
	}
	return out, nil
}

// movesOn says a handoff's loop record moves on here: an open one a
// review-loop run owns under the handoff binding. A run's loop that closed
// (approved, or its run ended) is history; a card with no record yet waits
// for its run, and one its handoff owns for adoption (sweepLoops).
func (c *HandoffReviewCaller) movesOn(rec *store.ReviewLoop) bool {
	return handoffBound(rec) && rec.State == store.ReviewLoopOpenState
}

// Loop is the loop of a card's open handoff: its review-loop run's.
func (c *HandoffReviewCaller) Loop(ctx context.Context, taskID string) (ReviewLoop, bool) {
	h, err := c.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return ReviewLoop{}, false
	}
	rec, err := c.db.CurrentReviewLoop(ctx, taskID)
	if err != nil || !handoffBound(rec) {
		return ReviewLoop{}, false
	}
	return handoffLoop(h, rec), true
}

// stack is the card's job clone, its local stack (a Staccato graph's
// branches, else the branch checked out), and the stack of open pull
// requests GitHub's base→head chain gives from those branches and the
// handoff's branch.
func (c *HandoffReviewCaller) stack(ctx context.Context, l ReviewLoop) (string, ReviewStack, error) {
	dir, branches, err := c.ReviewPassService.opts.Checkout.Branches(ctx, l.TaskID, l.Repo)
	if err != nil {
		return "", ReviewStack{}, &ReviewCheckoutError{Err: err}
	}
	seeds := append([]string(nil), branches...)
	if h, err := c.db.OpenHandoffForTask(ctx, l.TaskID); err == nil && h.Branch != "" {
		seeds = append(seeds, h.Branch)
	}
	stack, err := FindReviewStack(ctx, c.ReviewPassService.opts.GitHub, l.Repo, seeds, branches)
	if err == nil && len(stack.PRs) > 0 {
		// The card tracks the stack, so its summary row reports every
		// branch and the main finds every PR (pushedBranches).
		refs := make([]store.BranchRef, 0, len(stack.PRs))
		for _, pr := range stack.PRs {
			refs = append(refs, store.BranchRef{Repo: l.Repo, Branch: pr.Branch})
		}
		if _, err := c.db.TrackTaskBranches(ctx, l.TaskID, refs, BranchSourceReview); err != nil {
			log.Printf("review passes: recording the stack of %s: %v", l.TaskID, err)
		}
	}
	return dir, stack, err
}

// Checkout is the card's job clone and the open pull requests of its stack
// on GitHub. A layer the clone has no branch of is fetched for review
// (Fetched), so it is reviewed at its pinned head and not counted as a
// mismatch.
func (c *HandoffReviewCaller) Checkout(ctx context.Context, l ReviewLoop) (string, []ReviewGitHubPR, error) {
	dir, stack, err := c.stack(ctx, l)
	if err != nil {
		return "", nil, err
	}
	prs := stack.PRs
	for i, pr := range prs {
		if tip, err := c.ReviewPassService.opts.Checkout.Tip(ctx, dir, pr.Branch); err == nil && tip != "" {
			continue
		}
		if c.opts.FetchPR != nil {
			if err := c.opts.FetchPR(ctx, l.Repo, pr.Number); err != nil {
				return "", nil, err
			}
		}
		prs[i].Fetched = true
	}
	return dir, prs, nil
}

// OpenPRs are the open pull requests of the card's stack.
func (c *HandoffReviewCaller) OpenPRs(ctx context.Context, l ReviewLoop) ([]ReviewGitHubPR, error) {
	_, stack, err := c.stack(ctx, l)
	return stack.PRs, err
}

// Pushed says whether a branch of the card's clone moved past the head the
// last pass pinned, or a pull request was added to the stack.
func (c *HandoffReviewCaller) Pushed(ctx context.Context, l ReviewLoop, prs []store.HandoffReviewPR) (bool, error) {
	dir, stack, err := c.stack(ctx, l)
	if err != nil {
		return false, err
	}
	pinned := map[int]bool{}
	for _, pr := range prs {
		pinned[pr.Number] = true
	}
	for _, pr := range stack.PRs {
		if !pinned[pr.Number] {
			return true, nil // a pull request added to the stack
		}
	}
	for _, pr := range prs {
		tip, err := c.ReviewPassService.opts.Checkout.Tip(ctx, dir, pr.Branch)
		if err == nil && tip != "" && tip != pr.HeadSHA {
			return true, nil
		}
	}
	return false, nil
}

// columns are the card's columns: its loop's binding's, else this Legato's.
func (c *HandoffReviewCaller) columns(ctx context.Context, taskID string) reviewHandoffColumns {
	raw := ""
	if rec, err := c.db.CurrentReviewLoop(ctx, taskID); err == nil && rec != nil {
		raw = rec.HandoffColumns
	}
	return handoffColumns(raw, c.config())
}

// AuthorDone says whether the card is in the Done column.
func (c *HandoffReviewCaller) AuthorDone(ctx context.Context, l ReviewLoop) bool {
	task, err := c.db.GetTask(ctx, l.TaskID)
	if err != nil {
		return false
	}
	return strings.EqualFold(task.Status, c.columns(ctx, l.TaskID).Done)
}

// Stage moves the card: to the review column for a pass, the work column
// for feedback, Done for a clean pass approved at GitHub's heads (Confirm)
// or when nothing is left to review.
func (c *HandoffReviewCaller) Stage(ctx context.Context, l ReviewLoop, stage string) {
	if c.opts.Board == nil {
		return
	}
	cols := c.columns(ctx, l.TaskID)
	column := map[string]string{ReviewStagePass: cols.Review, ReviewStageAuthor: cols.Work, ReviewStageClean: cols.Done,
		ReviewStageNone: cols.Done}[stage]
	if column == "" {
		return
	}
	if task, err := c.db.GetTask(ctx, l.TaskID); stage == ReviewStageNone && err == nil && strings.EqualFold(task.Status, column) {
		return // there already: the author moved it to Done
	}
	if err := c.opts.Board.MoveCard(ctx, l.TaskID, column); err != nil {
		log.Printf("review passes: moving %s to %s: %v", l.TaskID, column, err)
	}
}

// ResumeAuthor resumes the card's agent, with everything it had.
func (c *HandoffReviewCaller) ResumeAuthor(ctx context.Context, l ReviewLoop) error {
	if c.opts.ResumeAuthor == nil {
		return ErrCannotResumeAuthor
	}
	return c.opts.ResumeAuthor(ctx, l.AuthorTaskID)
}

// Changed says the handoff changed, for its summary row.
func (c *HandoffReviewCaller) Changed(taskID string) {
	if c.opts.Bus != nil {
		c.opts.Bus.Publish(events.Event{Type: events.EventHandoffChanged, Payload: taskID, At: c.opts.Now()})
	}
}

// Subject is what the reviewer reviews.
func (c *HandoffReviewCaller) Subject() string { return "a handed-off task's pull requests" }

// The operator's actions on a handoff's loop by its ID (reviewLoopActor):
// a main's Run another pass and Message the author. A handoff's loop ends
// with its handoff (Return, Recall, Drop), so End is refused.

func (c *HandoffReviewCaller) ownsLoop(l *store.ReviewLoop) bool {
	return handoffBound(l)
}

func (c *HandoffReviewCaller) anotherPassOn(ctx context.Context, l *store.ReviewLoop) error {
	return c.anotherPass(ctx, l.HostTask)
}

func (c *HandoffReviewCaller) messageAuthorOn(ctx context.Context, l *store.ReviewLoop, text string) error {
	return c.messageAuthor(ctx, l.HostTask, text)
}

// ErrHandoffLoopEnds refuses End on a handoff's loop.
var ErrHandoffLoopEnds = errors.New("a handed-off task's review loop ends with its handoff: Return, Recall or Drop it")

func (c *HandoffReviewCaller) endOn(context.Context, *store.ReviewLoop) error {
	return ErrHandoffLoopEnds
}

// DoneAllowed says whether a handoff whose card is in Done may be done: one
// without review passes always, one with them only after a clean pass whose
// counted checks all passed or were answered by a reply (or when it had no
// pull request to review).
func (c *HandoffReviewCaller) DoneAllowed(ctx context.Context, h *store.Handoff) bool {
	if !c.applies(h) {
		return true
	}
	loop, err := c.db.GetHandoffReview(ctx, h.TaskID)
	if err != nil || loop == nil {
		return false
	}
	switch loop.Phase {
	case ReviewPhaseSkipped:
		return true
	case ReviewPhaseClean:
		// Approved only once Confirm found GitHub's heads at the clean
		// pass's pins (invariant 9).
		return confirmed(loop) && c.checksHandled(ctx, loop)
	}
	return false
}
