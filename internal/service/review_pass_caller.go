package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewLoop is one review loop as the review-pass engine moves it on: the
// pull requests of one task's branches, reviewed in that task's reviewer
// slot and recorded in the ledger under that task. A caller says which
// loops there are; the engine knows nothing else of where they came from.
type ReviewLoop struct {
	// TaskID keys the loop: its ledger rows, its reviewer slot, its job
	// clone.
	TaskID string
	// Repo is the repository (owner/name) of its pull requests.
	Repo string
	// AuthorTaskID is the task whose main-slot agent is the author, told
	// of each round and resumed when it has exited.
	AuthorTaskID string
	// CountComments says whether counted comments (from people and bots)
	// are items the author must handle, and Done waits for its answers;
	// CountChecks whether counted checks are: a failing one is an item on
	// its pull request, which is not approved while it fails. Handoff
	// loops count both, agent-authored run loops checks only, and run
	// loops a person authors neither (findings only, LEG-255).
	CountComments bool
	CountChecks   bool
	// PR is the pull request the loop reviews when its caller knows it
	// before the first pass (a run's); 0 otherwise.
	PR int
	// PRs are every pull request the loop reviews when its caller knows
	// them before the first pass (a run's stack); nil otherwise.
	PRs []int
	// Name and Link say whose loop it is, for a refusal to start another
	// on the same pull request ("run wr-1", "/workflows/runs/wr-1").
	Name, Link string
	// Pending says a loop whose phase reads finished (clean) is not: its
	// caller has yet to end it (a run confirming its head with GitHub).
	Pending bool
	// Owner is who the loop writes to GitHub for: the run's step attempt,
	// or the handoff. Each write checks it still is (mayWrite).
	Owner store.HandoffReviewOwner

	caller ReviewCaller
}

// Review stages a caller is told of.
const (
	// ReviewStagePass: a pass started.
	ReviewStagePass = "pass"
	// ReviewStageAuthor: the author has feedback to work on.
	ReviewStageAuthor = "author"
	// ReviewStageClean: a clean pass approved.
	ReviewStageClean = "clean"
	// ReviewStageNone: no open pull request was left to review.
	ReviewStageNone = "none"
)

// ReviewCaller is who starts review loops and what they mean to it. The
// handoff caller (HandoffReviewCaller) runs a loop for each reviewed
// make-a-change handoff.
type ReviewCaller interface {
	// Loops are the caller's loops that move on now.
	Loops(ctx context.Context) ([]ReviewLoop, error)
	// Loop is the caller's loop of a task; false when it has none (or the
	// loop no longer moves on).
	Loop(ctx context.Context, taskID string) (ReviewLoop, bool)
	// Checkout is the folder a pass reviews and the open pull requests it
	// reviews there, each with Branch the folder's local branch at its
	// head; none when there is nothing to review.
	Checkout(ctx context.Context, loop ReviewLoop) (dir string, prs []ReviewGitHubPR, err error)
	// OpenPRs are the loop's open pull requests on GitHub now, without
	// touching a checkout (for polling comments and checks).
	OpenPRs(ctx context.Context, loop ReviewLoop) ([]ReviewGitHubPR, error)
	// Pushed says whether the author pushed past the heads a pass
	// pinned (or added a pull request).
	Pushed(ctx context.Context, loop ReviewLoop, pinned []store.HandoffReviewPR) (bool, error)
	// AuthorDone says whether the author has finished a round: its work
	// is ready for the next pass.
	AuthorDone(ctx context.Context, loop ReviewLoop) bool
	// Stage tells the caller the loop entered a stage (ReviewStage*).
	Stage(ctx context.Context, loop ReviewLoop, stage string)
	// ResumeAuthor brings back an author that is not running;
	// ErrCannotResumeAuthor when this caller cannot.
	ResumeAuthor(ctx context.Context, loop ReviewLoop) error
	// Changed is told a loop's phase changed; taskID may be another
	// caller's.
	Changed(taskID string)
	// Subject is the reviewer brief's opening: what the pull requests
	// are ("a handed-off task's pull requests").
	Subject() string
}

// ErrCannotResumeAuthor is a caller that cannot resume a loop's author.
var ErrCannotResumeAuthor = errors.New("the author is not running, and this worker cannot resume it")

// AddCaller registers a caller whose loops the engine moves on.
func (s *ReviewPassService) AddCaller(caller ReviewCaller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callers = append(s.callers, caller)
}

// reviewOpener is a caller whose loops start as steps waiting to open, which
// only the engine's tick opens (ReviewRunCaller).
type reviewOpener interface {
	OpenWaiting(ctx context.Context)
	// waitingToOpenOn is a step waiting to open on the pull request,
	// other than the task except's, which holds it as a loop would.
	waitingToOpenOn(ctx context.Context, repo string, number int, except string) *ReviewLoopActiveError
}

// openWaiting opens every caller's steps waiting to open.
func (s *ReviewPassService) openWaiting(ctx context.Context) {
	s.mu.Lock()
	callers := slices.Clone(s.callers)
	s.mu.Unlock()
	for _, caller := range callers {
		if opener, ok := caller.(reviewOpener); ok {
			opener.OpenWaiting(ctx)
		}
	}
}

// loops are every caller's loops that move on now.
func (s *ReviewPassService) loops(ctx context.Context) []ReviewLoop {
	var out []ReviewLoop
	for _, caller := range s.callers {
		loops, err := caller.Loops(ctx)
		if err != nil {
			continue
		}
		for _, l := range loops {
			l.caller = caller
			out = append(out, l)
		}
	}
	return out
}

// loop is the loop of a task, from whichever caller has it.
func (s *ReviewPassService) loop(ctx context.Context, taskID string) (ReviewLoop, error) {
	for _, caller := range s.callers {
		if l, ok := caller.Loop(ctx, taskID); ok {
			l.caller = caller
			return l, nil
		}
	}
	return ReviewLoop{}, errors.New("no review loop runs on " + taskID)
}

// resumeAuthor resumes a loop's author through its caller.
func (l ReviewLoop) resumeAuthor(ctx context.Context) error {
	if l.caller == nil {
		return ErrCannotResumeAuthor
	}
	return l.caller.ResumeAuthor(ctx, l)
}

func (l ReviewLoop) stage(ctx context.Context, stage string) {
	if l.caller != nil {
		l.caller.Stage(ctx, l, stage)
	}
}

// ReviewCheckoutError is a checkout that could not be read, which makes the
// loop stuck; any other error from Checkout is tried again next tick.
type ReviewCheckoutError struct{ Err error }

func (e *ReviewCheckoutError) Error() string { return e.Err.Error() }
func (e *ReviewCheckoutError) Unwrap() error { return e.Err }

// ReviewLoopActiveError refuses a review loop on a pull request that has an
// active one: one review loop per pull request, whoever started it.
type ReviewLoopActiveError struct {
	Repo       string
	PR         int
	Name, Link string
}

func (e *ReviewLoopActiveError) Error() string {
	return fmt.Sprintf("pull request #%d of %s already has an active review loop: %s (%s)", e.PR, e.Repo, e.Name, e.Link)
}

// ActiveLoopOn is the active loop on a pull request, among every caller's:
// one that has not ended, approved, or found nothing to review. Nil when
// there is none.
func (s *ReviewPassService) ActiveLoopOn(ctx context.Context, repo string, number int) *ReviewLoopActiveError {
	return s.activeLoopOn(ctx, repo, number, "")
}

// activeLoopOn is ActiveLoopOn, leaving out the loop of except.
func (s *ReviewPassService) activeLoopOn(ctx context.Context, repo string, number int, except string) *ReviewLoopActiveError {
	for _, l := range s.loops(ctx) {
		if l.TaskID == except || !strings.EqualFold(l.Repo, repo) {
			continue
		}
		loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
		if err != nil {
			continue
		}
		if loop != nil && !l.Pending && (loop.Phase == ReviewPhaseClean || loop.Phase == ReviewPhaseSkipped || loop.Phase == ReviewPhaseEnded) {
			continue
		}
		on := l.PR == number || slices.Contains(l.PRs, number)
		if !on && loop != nil {
			prs, _ := s.db.ListHandoffReviewPRs(ctx, l.TaskID, loop.Pass)
			for _, pr := range prs {
				on = on || pr.Number == number
			}
		}
		if on {
			return &ReviewLoopActiveError{Repo: repo, PR: number, Name: l.Name, Link: l.Link}
		}
	}
	for _, caller := range s.callers {
		if opener, ok := caller.(reviewOpener); ok {
			if active := opener.waitingToOpenOn(ctx, repo, number, except); active != nil {
				return active
			}
		}
	}
	return nil
}
