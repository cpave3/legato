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
}

// HandoffReviewCaller is the review-pass engine's caller for a worker's
// make-a-change handoffs: a loop per open handoff whose review passes are on,
// keyed on the card. The card's column drives it: Done means the author is
// done, a pass moves the card to the review column, feedback to the work
// column, a clean pass to Done, and a handoff is done only after a clean
// review (DoneAllowed). Comments from people and bots and counted checks are
// items the author handles too. It is what the handoff side asks of review
// passes (HandoffReviews).
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
// unless its main turned them off for it.
func (c *HandoffReviewCaller) applies(h *store.Handoff) bool {
	if !c.config().Enabled || h.Purpose != HandoffMakeChange || h.ClosedAt != nil {
		return false
	}
	loop, err := c.db.GetHandoffReview(context.Background(), h.TaskID)
	if err != nil {
		return false
	}
	return loopSettings(loop, c.config().Defaults).Enabled
}

func handoffLoop(h *store.Handoff) ReviewLoop {
	return ReviewLoop{TaskID: h.TaskID, Repo: h.Repo, AuthorTaskID: h.TaskID, CountComments: true, CountChecks: true,
		Name: "handed-off task " + h.TaskID, Link: "/board?highlight_task=" + h.TaskID,
		Owner: store.HandoffReviewOwner{MainInstanceID: h.MainInstanceID, HandoffID: h.HandoffID}}
}

// openLoop opens the loop of a card's handoff with the engine's Open: owned
// by the handoff, hosted by the card, with no pull requests yet; each pass
// claims those it pins. A handoff that has its loop gets it back, and
// another handoff of the same card gets one of its own.
func (c *HandoffReviewCaller) openLoop(ctx context.Context, h *store.Handoff) (*store.ReviewLoop, error) {
	loop, _, err := c.Open(ctx, store.HandoffReviewLoopOpen(h.TaskID, store.HandoffOfCard{
		MainInstanceID: h.MainInstanceID, HandoffID: h.HandoffID, Repo: h.Repo}))
	return loop, err
}

// SetSettings opens the loop of a card's handoff (openLoop) and keeps its
// review settings (a main's override, or the worker's defaults) on it.
func (c *HandoffReviewCaller) SetSettings(ctx context.Context, taskID string, settings ReviewPassSettings) error {
	if h, err := c.db.OpenHandoffForTask(ctx, taskID); err == nil {
		if _, err := c.openLoop(ctx, h); err != nil {
			return err
		}
	}
	return c.ReviewPassService.SetSettings(ctx, taskID, settings)
}

// OpenWaiting opens the loop of each reviewed handoff whose card has none
// of its own yet, in the holder's tick.
func (c *HandoffReviewCaller) OpenWaiting(ctx context.Context) {
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
		if _, err := c.openLoop(ctx, h); err != nil {
			log.Printf("review passes: opening the loop of handoff %s: %v", h.HandoffID, err)
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
// has started.
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
		out = append(out, handoffLoop(h))
	}
	return out, nil
}

// Loop is the loop of a card's open handoff.
func (c *HandoffReviewCaller) Loop(ctx context.Context, taskID string) (ReviewLoop, bool) {
	h, err := c.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return ReviewLoop{}, false
	}
	return handoffLoop(h), true
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

// AuthorDone says whether the card is in the Done column.
func (c *HandoffReviewCaller) AuthorDone(ctx context.Context, l ReviewLoop) bool {
	task, err := c.db.GetTask(ctx, l.TaskID)
	if err != nil {
		return false
	}
	return strings.EqualFold(task.Status, c.config().DoneColumn)
}

// Stage moves the card: to the review column for a pass, the work column
// for feedback, Done for a clean pass approved at GitHub's heads (Confirm)
// or when nothing is left to review.
func (c *HandoffReviewCaller) Stage(ctx context.Context, l ReviewLoop, stage string) {
	if c.opts.Board == nil {
		return
	}
	cfg := c.config()
	column := map[string]string{ReviewStagePass: cfg.ReviewColumn, ReviewStageAuthor: cfg.WorkColumn, ReviewStageClean: cfg.DoneColumn,
		ReviewStageNone: cfg.DoneColumn}[stage]
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
	return l.OwnerKind == store.ReviewLoopOwnerHandoff
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
