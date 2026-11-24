package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Phases of a card's review loop. Ready, an internal phase, means a pass
// starts once the author is idle and its checkout matches GitHub.
const (
	ReviewPhaseAuthor             = ""
	ReviewPhaseReady              = "ready"
	ReviewPhaseWaitingForReviewer = "waiting_for_reviewer"
	ReviewPhaseReviewing          = "reviewing"
	ReviewPhaseWaitingForAuthor   = "waiting_for_author"
	ReviewPhaseClean              = "clean"
	ReviewPhaseStuck              = "stuck"
	ReviewPhaseSkipped            = "skipped"
)

// States of one pass.
const (
	ReviewPassReviewing = "reviewing"
	ReviewPassSubmitted = "submitted"
	ReviewPassPosted    = "posted"
	ReviewPassDiscarded = "discarded"
	ReviewPassUnable    = "unable"
)

// Severities and verdicts.
const (
	SeverityBlocking = "blocking"
	SeverityNit      = "nit"
	VerdictClean     = "clean"
	VerdictChanges   = "changes"
)

// ReviewTag opens every line Legato sends the author about review passes.
const ReviewTag = "[legato review pass]"

// ReviewPassConfig is an instance's review settings (config review_passes).
type ReviewPassConfig struct {
	Enabled       bool
	Defaults      ReviewPassSettings
	PassTimeout   time.Duration
	MaxConcurrent int
	ReviewColumn  string
	// DoneColumn and WorkColumn are where a clean pass puts the card and
	// where it goes while the author works on feedback.
	DoneColumn string
	WorkColumn string
	// PollInterval is how often pull requests are polled for comments,
	// QuietTime how long new comments wait for more; CountedBots are the
	// bot logins whose comments count.
	PollInterval time.Duration
	QuietTime    time.Duration
	CountedBots  []string
	// RequiredChecks count when branch protection on a pull request's
	// base names none.
	RequiredChecks []string
}

// ReviewGitHubPR is an open pull request of a loop's branch.
type ReviewGitHubPR struct {
	Number  int
	Branch  string
	Base    string
	Title   string
	Body    string
	URL     string
	HeadSHA string
	// Author is the pull request's author (the author App's login).
	Author string
	// HeadRepo is the repository (owner/name) its head branch is in; ""
	// when not read.
	HeadRepo string
	// Fetched says the author's checkout has no local branch of this
	// name: Legato fetched its head for review, and its tip is not
	// compared.
	Fetched bool
	// LocalBranch is the branch of the checkout the pull request is put
	// on when it is not Branch (a run's reviewer clone: GitHub's head
	// branch, or legato-review/pr-<n> for a fork's); "" means Branch.
	LocalBranch string
}

// checkoutBranch is the branch of the checkout that holds the pull
// request's head.
func (pr ReviewGitHubPR) checkoutBranch() string {
	if pr.LocalBranch != "" {
		return pr.LocalBranch
	}
	return pr.Branch
}

// ReviewGitHub is what a review pass asks of GitHub: ReviewPublisher, as
// the reviewer App.
type ReviewGitHub interface {
	// PullRequests are the open pull requests whose heads are branches, in
	// the order of branches.
	PullRequests(ctx context.Context, repo string, branches []string) ([]ReviewGitHubPR, error)
	// OpenPulls are the open pull requests with a head or a base branch
	// (FindReviewStack follows them).
	OpenPulls(ctx context.Context, repo string, q PullQuery) ([]ReviewGitHubPR, error)
	// PullRequest is one pull request and its state (open, closed or
	// merged): its head is read again before a pass's reviews are
	// posted.
	PullRequest(ctx context.Context, repo string, number int) (ReviewGitHubPR, string, error)
	// PullDiff is the pull request's own diff at its head (GitHub's,
	// against its base): the head's changed lines, first and last, by
	// file. nil when GitHub could not say.
	PullDiff(ctx context.Context, repo string, number int) (map[string][][2]int, error)
	PostReview(ctx context.Context, post ReviewPost) (PostedReview, error)
	// PostIssueComment and Reply post once by marker: a comment GitHub
	// already has with the marker is taken for it.
	PostIssueComment(ctx context.Context, who GitHubIdentity, repo string, number int, body string, marker CommentMarker) (int64, error)
	PollComments(ctx context.Context, repo string, number int, pages PollPages) (CommentPoll, error)
	Reply(ctx context.Context, who GitHubIdentity, repo string, number int, commentID int64, body string, marker CommentMarker) error
	ResolveThread(ctx context.Context, repo string, number int, commentID int64) error
	// Checks are the counted checks of a pull request's head.
	Checks(ctx context.Context, repo, base, head string, configured []string) ([]CheckResult, error)
}

// ReviewCheckout is the author's checkout, read on the host.
type ReviewCheckout interface {
	// Branches are the card's clone and the branches of its pull
	// requests, bottom of the stack first.
	Branches(ctx context.Context, taskID, repo string) (dir string, branches []string, err error)
	// Tip is a local branch's commit.
	Tip(ctx context.Context, dir, branch string) (string, error)
	// State is HEAD's commit and whether the working tree is clean
	// (ignored files aside).
	State(ctx context.Context, dir string) (head string, clean bool, err error)
	// Pin keeps a commit under a ref of Legato's, so a later pass can diff
	// against it after a force-push.
	Pin(ctx context.Context, dir, ref, sha string) error
	// Lines is how many lines a file has at a commit; false when it is not
	// there.
	Lines(ctx context.Context, dir, sha, path string) (int, bool, error)
	// Changed is what changed from one commit to another: the line ranges
	// of the later one, first and last, by file.
	Changed(ctx context.Context, dir, from, to string) (map[string][][2]int, error)
}

// ReviewAgents is what a review pass asks of the agent service.
type ReviewAgents interface {
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
	QueueMessage(ctx context.Context, taskID, message string) error
	ReconcileSessions(ctx context.Context) error
	SlotAgents
}

// ReviewPassOptions configure the review-pass engine.
type ReviewPassOptions struct {
	Config   func() ReviewPassConfig
	Agents   ReviewAgents
	GitHub   ReviewGitHub
	Checkout ReviewCheckout
	Bus      *events.Bus
	Now      func() time.Time
	// Run runs posting in the background; the caller's goroutine when nil.
	Run func(func())
	// Overlay says whether the reviewer's checkout gets a throwaway layer
	// (CheckoutOverlayProber); nil means it does not.
	Overlay func() bool
	// Advert is this instance's review passes as /api/meta says them,
	// which a main's override is checked against; nil refuses every
	// override.
	Advert func() *ReviewPassesAdvert
}

// ReviewPassService is the review-pass engine. For each review loop its
// callers have (ReviewCaller), it notices the author finished, pins the
// pull requests' heads, starts a reviewer in the loop task's reviewer slot,
// takes its submit_review, posts it to GitHub as the reviewer App, and
// tells the loop what comes next. It knows nothing of who started a loop.
// Tick drives it; the runtime calls it on card events and on a timer.
type ReviewPassService struct {
	db      *store.Store
	opts    ReviewPassOptions
	mu      sync.Mutex // one Tick or submission at a time
	callers []ReviewCaller

	// waitMu guards waiting and lastErr: what each ready loop waits on,
	// and the last GitHub error each logged. In memory: a restart learns
	// both again on the next tick.
	waitMu  sync.Mutex
	waiting map[string]string
	lastErr map[string]string
}

// What a ready loop waits on before its next pass starts (ReviewLoopBlock's
// WaitingOn).
const (
	// ReviewWaitingOnAuthorWorking: the author is in a turn.
	ReviewWaitingOnAuthorWorking = "author_working"
	// ReviewWaitingOnAuthorWaiting: the author waits for an answer.
	ReviewWaitingOnAuthorWaiting = "author_waiting"
	// ReviewWaitingOnAuthorPush: the author was told to push or discard.
	ReviewWaitingOnAuthorPush = "author_push"
	// ReviewWaitingOnGitHub: GitHub did not answer.
	ReviewWaitingOnGitHub = "github"
	// ReviewWaitingOnReviewerSlot: every reviewer slot is taken.
	ReviewWaitingOnReviewerSlot = "reviewer_slot"
	// ReviewWaitingOnStart: nothing; the pass starts on the next tick.
	ReviewWaitingOnStart = "start"
)

// setWaiting records what a loop waits on; "" clears it.
func (s *ReviewPassService) setWaiting(taskID, on string) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	if s.waiting == nil {
		s.waiting = map[string]string{}
	}
	if on == "" {
		delete(s.waiting, taskID)
		return
	}
	s.waiting[taskID] = on
}

func (s *ReviewPassService) waitingOn(taskID string) string {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return s.waiting[taskID]
}

// logGitHubError logs a loop's GitHub error once, until it changes or
// clears (err nil).
func (s *ReviewPassService) logGitHubError(taskID string, err error) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	if s.lastErr == nil {
		s.lastErr = map[string]string{}
	}
	if err == nil {
		delete(s.lastErr, taskID)
		return
	}
	if s.lastErr[taskID] == err.Error() {
		return
	}
	s.lastErr[taskID] = err.Error()
	log.Printf("review passes: %s: reading its pull requests from GitHub, trying again: %v", taskID, err)
}

// NewReviewPassService makes the review-pass engine. It does nothing until a
// caller (AddCaller) has a loop.
func NewReviewPassService(db *store.Store, opts ReviewPassOptions) *ReviewPassService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Run == nil {
		opts.Run = func(f func()) { f() }
	}
	return &ReviewPassService{db: db, opts: opts}
}

func (s *ReviewPassService) config() ReviewPassConfig {
	cfg := s.opts.Config()
	if cfg.DoneColumn == "" {
		cfg.DoneColumn = "Done"
	}
	if cfg.WorkColumn == "" {
		cfg.WorkColumn = "Doing"
	}
	if cfg.ReviewColumn == "" {
		cfg.ReviewColumn = "Review"
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	return cfg
}

func (s *ReviewPassService) now() string { return s.opts.Now().UTC().Format(time.RFC3339) }

// Review is a card's review loop as the main sees it.
func (s *ReviewPassService) Review(ctx context.Context, taskID string) (*store.HandoffReview, error) {
	return s.db.GetHandoffReview(ctx, taskID)
}

// settings are a loop's review settings.
func loopSettings(loop *store.HandoffReview, defaults ReviewPassSettings) ReviewPassSettings {
	settings := defaults
	if loop != nil && loop.Settings != "" && loop.Settings != "{}" {
		_ = json.Unmarshal([]byte(loop.Settings), &settings)
	}
	return settings
}

// SetSettings records the review settings of a card's handoff (a main's
// override, or the worker's defaults) before its loop starts.
func (s *ReviewPassService) SetSettings(ctx context.Context, taskID string, settings ReviewPassSettings) error {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return err
	}
	if loop == nil {
		loop = &store.HandoffReview{TaskID: taskID}
	}
	data, _ := json.Marshal(settings)
	loop.Settings, loop.UpdatedAt = string(data), s.now()
	return s.db.SaveHandoffReview(ctx, *loop)
}

func encodeJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

// Tick moves every caller's review loops on: a finished author starts a
// pass, a reviewer past its time or gone is unable, and queued passes start
// as reviewers free up.
func (s *ReviewPassService) Tick(ctx context.Context) {
	if !s.config().Enabled {
		return
	}
	// Steps any process started open here first (spec LEG-559 D9). Not
	// under the lock: opening asks GitHub and makes clones, as a start
	// did.
	s.openWaiting(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	loops := s.loops(ctx)
	if len(loops) == 0 {
		return // no caller has a loop: nothing to move on
	}
	_ = s.opts.Agents.ReconcileSessions(ctx)
	for _, l := range loops {
		s.postAnswers(ctx, l)
		if l.CountComments {
			s.pollComments(ctx, l)
		}
		if l.CountChecks {
			s.pollChecks(ctx, l)
		}
		if err := s.step(ctx, l); err != nil {
			log.Printf("review passes: %s: %v", l.TaskID, err)
		}
	}
	s.startQueued(ctx)
}

// step moves one loop on.
func (s *ReviewPassService) step(ctx context.Context, l ReviewLoop) error {
	loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
	if err != nil {
		return err
	}
	if loop == nil {
		loop = &store.HandoffReview{TaskID: l.TaskID, Settings: "{}"}
	}
	inDone := l.caller.AuthorDone(ctx, l)
	// AuthorDone may have ended the loop (a run's pull request closed):
	// the phase read before it is stale.
	if fresh, err := s.db.GetHandoffReview(ctx, l.TaskID); err == nil && fresh != nil && fresh.Phase != loop.Phase {
		return nil
	}
	switch loop.Phase {
	case ReviewPhaseAuthor:
		if err := s.deliverComments(ctx, l, loop); err != nil {
			return err
		}
		if !inDone || (l.CountComments && RefuseUnansweredReview(ctx, s.db, l.TaskID) != nil) {
			return nil
		}
		loop.Phase = ReviewPhaseReady
		return s.ready(ctx, l, loop)
	case ReviewPhaseReady:
		return s.ready(ctx, l, loop)
	case ReviewPhaseReviewing:
		return s.watchReviewer(ctx, l, loop)
	case ReviewPhaseWaitingForAuthor:
		if err := s.deliverComments(ctx, l, loop); err != nil {
			return err
		}
		return s.feedback(ctx, l, loop, inDone)
	case ReviewPhaseClean:
		if !l.CountChecks {
			return nil
		}
		if err := s.approveSettled(ctx, l, loop); err != nil {
			return err
		}
		return s.checksReopen(ctx, l, loop)
	}
	return nil
}

// authorIdle says whether the author is between turns: idle, not running,
// or with only background jobs running (which may never end; the tip and
// clean-tree check catches work they leave unpushed). A person (no author
// task) always is.
func (s *ReviewPassService) authorIdle(ctx context.Context, taskID string) bool {
	return s.authorBusy(ctx, taskID) == ""
}

// authorBusy is what the author is doing that holds a pass back:
// ReviewWaitingOnAuthorWorking, ReviewWaitingOnAuthorWaiting, or "" when it
// holds nothing back.
func (s *ReviewPassService) authorBusy(ctx context.Context, taskID string) string {
	if taskID == "" {
		return ""
	}
	session, err := s.opts.Agents.GetAgentSession(ctx, taskID)
	if err != nil || session == nil {
		return ""
	}
	switch session.Activity {
	case "working":
		return ReviewWaitingOnAuthorWorking
	case "waiting":
		return ReviewWaitingOnAuthorWaiting
	}
	return ""
}

// ready starts a pass when the author is idle and its checkout is what
// GitHub has: each pull request's branch at its head, the tree clean.
func (s *ReviewPassService) ready(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) error {
	if loop.Mismatches > 0 && !loop.AuthorBusySinceNudge {
		// The author was told to push or discard; wait until it has
		// worked on it, or for a while when it does not answer.
		s.setWaiting(l.TaskID, ReviewWaitingOnAuthorPush)
		if !s.authorIdle(ctx, l.AuthorTaskID) {
			loop.AuthorBusySinceNudge = true
			return s.save(ctx, loop)
		}
		if nudged, err := time.Parse(time.RFC3339, loop.NudgedAt); err == nil && s.opts.Now().Sub(nudged) < 10*time.Minute {
			return s.save(ctx, loop)
		}
	}
	if busy := s.authorBusy(ctx, l.AuthorTaskID); busy != "" {
		s.setWaiting(l.TaskID, busy)
		return s.save(ctx, loop)
	}
	dir, prs, err := l.caller.Checkout(ctx, l)
	var checkoutErr *ReviewCheckoutError
	if errors.As(err, &checkoutErr) {
		s.setWaiting(l.TaskID, "")
		return s.stuck(ctx, l, loop, "Legato could not read the author's checkout: "+checkoutErr.Err.Error())
	}
	if err != nil {
		s.setWaiting(l.TaskID, ReviewWaitingOnGitHub)
		s.logGitHubError(l.TaskID, err)
		return s.save(ctx, loop) // GitHub may answer next time
	}
	s.logGitHubError(l.TaskID, nil)
	s.setWaiting(l.TaskID, ReviewWaitingOnStart)
	if len(prs) == 0 {
		// Nothing to review: the loop is done as without review passes.
		loop.Phase = ReviewPhaseSkipped
		if err := s.save(ctx, loop); err != nil {
			return err
		}
		l.stage(ctx, ReviewStageNone)
		return nil
	}
	var mismatch []string
	for _, pr := range prs {
		if pr.Fetched {
			continue // no local branch to compare: reviewed at GitHub's head
		}
		tip, err := s.opts.Checkout.Tip(ctx, dir, pr.checkoutBranch())
		if err != nil || tip != pr.HeadSHA {
			mismatch = append(mismatch, fmt.Sprintf("%s is at %s here and %s on GitHub (#%d)", pr.checkoutBranch(), short(tip), short(pr.HeadSHA), pr.Number))
		}
	}
	if _, clean, err := s.opts.Checkout.State(ctx, dir); err != nil || !clean {
		mismatch = append(mismatch, "the working tree has changes that are not committed")
	}
	if len(mismatch) > 0 {
		return s.mismatch(ctx, l, loop, mismatch)
	}
	loop.Mismatches, loop.AuthorBusySinceNudge, loop.NudgedAt = 0, false, ""
	settings := loopSettings(loop, s.config().Defaults)
	if loop.Pass >= settings.MaxPasses && settings.MaxPasses > 0 {
		reason := fmt.Sprintf("the review reached its limit of %d passes", settings.MaxPasses)
		if findings, err := s.db.ListHandoffReviewFindings(ctx, l.TaskID); err == nil && notConverging(findings, loop.Pass) {
			reason = fmt.Sprintf("passes %d and %d each found new blocking problems in the author's fixes, so the review stopped before pass %d for you to decide", loop.Pass-1, loop.Pass, loop.Pass+1)
		}
		return s.stuck(ctx, l, loop, reason)
	}
	// One loop per pull request: a pass does not start on one another loop
	// reviews (a run started before a handoff's first pass, say).
	for _, pr := range prs {
		if active := s.activeLoopOn(ctx, l.Repo, pr.Number, l.TaskID); active != nil {
			return s.stuck(ctx, l, loop, fmt.Sprintf("another review loop reviews #%d: %s (%s)", pr.Number, active.Name, active.Link))
		}
	}
	if s.running(ctx) >= s.config().MaxConcurrent {
		if loop.Phase != ReviewPhaseWaitingForReviewer {
			loop.Phase, loop.QueuedAt = ReviewPhaseWaitingForReviewer, s.now()
			return s.saveChanged(ctx, loop)
		}
		return nil
	}
	return s.startPass(ctx, l, loop, dir, prs)
}

// mismatch tells the author once to push or discard; a second miss is
// stuck.
func (s *ReviewPassService) mismatch(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, why []string) error {
	if loop.Mismatches > 0 {
		return s.stuck(ctx, l, loop, "the checkout still does not match GitHub after the author was told: "+strings.Join(why, "; "))
	}
	message := ReviewTag + " Push or discard before review: " + strings.Join(why, "; ") + ". Then end your turn; the review starts when your branches on GitHub match your checkout."
	if err := s.tellAuthor(ctx, l, message); err != nil {
		return s.stuck(ctx, l, loop, "the author could not be told to push before review: "+err.Error())
	}
	loop.Phase, loop.Mismatches, loop.NudgedAt, loop.AuthorBusySinceNudge = ReviewPhaseReady, 1, s.now(), false
	return s.saveChanged(ctx, loop)
}

// running counts the reviewers at work on this instance.
func (s *ReviewPassService) running(ctx context.Context) int {
	loops, err := s.db.ListHandoffReviews(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range loops {
		if l.Phase == ReviewPhaseReviewing {
			n++
		}
	}
	return n
}

// startQueued starts the passes waiting for a reviewer, oldest first, while
// there is room.
func (s *ReviewPassService) startQueued(ctx context.Context) {
	loops, err := s.db.ListHandoffReviews(ctx)
	if err != nil {
		return
	}
	for i := range loops {
		loop := &loops[i]
		if loop.Phase != ReviewPhaseWaitingForReviewer || s.running(ctx) >= s.config().MaxConcurrent {
			continue
		}
		l, err := s.loop(ctx, loop.TaskID)
		if err != nil {
			continue
		}
		loop.Phase = ReviewPhaseReady
		if err := s.ready(ctx, l, loop); err != nil {
			log.Printf("review passes: %s: %v", loop.TaskID, err)
		}
	}
}

// startPass pins the heads, tells the caller a pass started, and starts a
// fresh reviewer in the loop task's reviewer slot.
func (s *ReviewPassService) startPass(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, dir string, prs []ReviewGitHubPR) error {
	cfg := s.config()
	settings := loopSettings(loop, cfg.Defaults)
	pass := loop.Pass + 1
	head, _, err := s.opts.Checkout.State(ctx, dir)
	if err != nil {
		return s.stuck(ctx, l, loop, "Legato could not read the author's checkout: "+err.Error())
	}
	record := store.HandoffReviewPass{TaskID: l.TaskID, Pass: pass, State: ReviewPassReviewing, Checkout: dir, HeadSHA: head, StartedAt: s.now()}
	var pinned []store.HandoffReviewPR
	for _, pr := range prs {
		if err := s.opts.Checkout.Pin(ctx, dir, pinRef(l.TaskID, pass, pr.Number), pr.HeadSHA); err != nil {
			return s.stuck(ctx, l, loop, "Legato could not pin the head of #"+fmt.Sprint(pr.Number)+": "+err.Error())
		}
		pinned = append(pinned, store.HandoffReviewPR{TaskID: l.TaskID, Pass: pass, Repo: l.Repo, Number: pr.Number, Branch: pr.Branch,
			Base: pr.Base, Title: pr.Title, Body: pr.Body, URL: pr.URL, HeadSHA: pr.HeadSHA, Author: pr.Author, LocalBranch: pr.LocalBranch, HeadRepo: pr.HeadRepo})
	}
	if err := s.db.InsertHandoffReviewPass(ctx, record, pinned); err != nil {
		return err
	}
	loop.Pass, loop.Phase, loop.QueuedAt, loop.Reason = pass, ReviewPhaseReviewing, "", ""
	s.setWaiting(l.TaskID, "")
	if l.CountChecks && loop.ChecksHeads != checkHeads(pinned) {
		// The checks of the heads this pass reviews, read now.
		s.readChecks(ctx, l, loop, pinned)
	}
	if err := s.saveChanged(ctx, loop); err != nil {
		return err
	}
	l.stage(ctx, ReviewStagePass)
	// The author's work is up for review; the reviewer reviews again, a
	// resumed one that finished the pass before included.
	publishMilestone(s.opts.Bus, events.WorkMilestonePayload{TaskID: l.AuthorTaskID,
		Stage: store.WorkStageReviewing, Reason: fmt.Sprintf("review pass %d", pass)})
	publishMilestone(s.opts.Bus, events.WorkMilestonePayload{TaskID: l.TaskID, Slot: SlotReviewer,
		Stage: store.WorkStageReviewing, Reason: fmt.Sprintf("pass %d", pass), Back: true})
	brief, err := s.brief(ctx, l, pass, pinned, settings)
	if err != nil {
		return s.unable(ctx, l, loop, record, "Legato could not write the reviewer's brief: "+err.Error())
	}
	opts := AgentSpawnOptions{AgentKind: settings.Agent, Model: settings.Model, Tier: settings.Tier, Slot: SlotReviewer,
		WorkingDir: dir, Assignment: brief, Unattended: true, ReadOnlyCheckout: true, ThrowawayCheckout: true,
		ExecutionKind: store.ExecutionInteractive}
	if !s.resumeReviewer(ctx, l.TaskID, pass, settings, opts) {
		if err := s.opts.Agents.SpawnAgent(ctx, l.TaskID, 0, 0, opts); err != nil {
			return s.unable(ctx, l, loop, record, "the reviewer could not start: "+err.Error())
		}
	}
	if session, err := s.opts.Agents.GetAgentSessionInSlot(ctx, l.TaskID, SlotReviewer); err == nil {
		record.ExecutionID = session.ExecutionID
		_ = s.db.UpdateHandoffReviewPass(ctx, record)
	}
	return nil
}

// resumeReviewer starts a later pass's reviewer in the conversation the
// reviewer slot saved, so it remembers what it read and decided. False when
// there is none or it would not resume: a fresh reviewer starts instead.
func (s *ReviewPassService) resumeReviewer(ctx context.Context, taskID string, pass int, settings ReviewPassSettings, opts AgentSpawnOptions) bool {
	if pass < 2 {
		return false
	}
	saved, err := s.opts.Agents.GetTaskResumeSessionInSlot(ctx, taskID, SlotReviewer, settings.Agent)
	if err != nil || saved == nil || saved.ID == "" {
		return false
	}
	opts.SessionPolicy, opts.ResumeSessionID = "resume", saved.ID
	// A resumed launch sends no kickoff for an assignment (and Chimera's
	// resume drops --prompt), but every tool is sent its prompt: the
	// brief is the conversation's next message.
	opts.Prompt, opts.Assignment = opts.Assignment, ""
	if err := s.opts.Agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		log.Printf("review passes: %s: resuming the reviewer for pass %d, starting a fresh one: %v", taskID, pass, err)
		return false
	}
	return true
}

// watchReviewer makes a reviewer that ran past its time, or ended without
// submitting, unable.
func (s *ReviewPassService) watchReviewer(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) error {
	pass, err := s.db.GetHandoffReviewPass(ctx, l.TaskID, loop.Pass)
	if err != nil {
		return err
	}
	if pass.State == ReviewPassSubmitted {
		return s.post(ctx, l.TaskID) // a post that failed, tried again
	}
	if pass.State != ReviewPassReviewing {
		return nil
	}
	started, _ := time.Parse(time.RFC3339, pass.StartedAt)
	if limit := s.config().PassTimeout; limit > 0 && s.opts.Now().Sub(started) > limit {
		return s.unable(ctx, l, loop, *pass, fmt.Sprintf("the reviewer ran past %s", limit))
	}
	if session, err := s.opts.Agents.GetAgentSessionInSlot(ctx, l.TaskID, SlotReviewer); err != nil || session == nil {
		return s.unable(ctx, l, loop, *pass, "the reviewer exited without submitting")
	}
	return nil
}

// unable ends a pass with nothing posted and makes the review stuck.
func (s *ReviewPassService) unable(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, pass store.HandoffReviewPass, reason string) error {
	_ = s.opts.Agents.KillAgentInSlot(ctx, l.TaskID, SlotReviewer)
	pass.State, pass.Reason, pass.EndedAt = ReviewPassUnable, reason, s.now()
	if err := s.db.UpdateHandoffReviewPass(ctx, pass); err != nil {
		return err
	}
	return s.stuck(ctx, l, loop, reason)
}

// stuck stops the loop for the person, who decides what comes next.
func (s *ReviewPassService) stuck(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, reason string) error {
	s.setWaiting(l.TaskID, "")
	loop.Phase, loop.Reason, loop.QueuedAt = ReviewPhaseStuck, reason, ""
	return s.saveChanged(ctx, loop)
}

func (s *ReviewPassService) save(ctx context.Context, loop *store.HandoffReview) error {
	loop.UpdatedAt = s.now()
	return s.db.SaveHandoffReview(ctx, *loop)
}

// saveChanged saves a loop whose phase changed, and says so.
func (s *ReviewPassService) saveChanged(ctx context.Context, loop *store.HandoffReview) error {
	if err := s.save(ctx, loop); err != nil {
		return err
	}
	for _, caller := range s.callers {
		caller.Changed(loop.TaskID)
	}
	return nil
}

// passReviewMarker is the marker of a pass's review of a pull request. A pass
// pinned again after a discard (Attempt > 0) has markers of its own, so a
// review a discarded attempt left on GitHub is never taken for its own.
func passReviewMarker(pass store.HandoffReviewPass, number int) string {
	if pass.Attempt > 0 {
		return fmt.Sprintf("%s/pass-%d.%d/pr-%d", pass.TaskID, pass.Pass, pass.Attempt, number)
	}
	return fmt.Sprintf("%s/pass-%d/pr-%d", pass.TaskID, pass.Pass, number)
}

// rulingMarker is the marker of a pass's ruling replied in a finding's
// thread. Like passReviewMarker, a pass pinned again after a discard has
// markers of its own, so a reply a discarded attempt left is never taken
// for the ruling of the attempt that replaced it.
func rulingMarker(pass store.HandoffReviewPass, findingID string) CommentMarker {
	if pass.Attempt > 0 {
		return CommentMarker{Kind: MarkerRuling, Key: fmt.Sprintf("%s/pass-%d.%d/%s", pass.TaskID, pass.Pass, pass.Attempt, findingID)}
	}
	return CommentMarker{Kind: MarkerRuling, Key: fmt.Sprintf("%s/pass-%d/%s", pass.TaskID, pass.Pass, findingID)}
}

// pinRef is the ref a pass's pinned head is kept under.
func pinRef(taskID string, pass, number int) string {
	return fmt.Sprintf("refs/legato/review/%s/%d/%d", taskID, pass, number)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "nothing"
	}
	return sha
}

// ErrNotTheReviewer refuses a submission from a session that is not the
// loop's running reviewer.
var ErrNotTheReviewer = errors.New("only the card's reviewer submits a review")

// tellAuthor queues a line for the loop's author agent; a person (no author
// task) is told on GitHub only, so there is nothing to queue.
func (s *ReviewPassService) tellAuthor(ctx context.Context, l ReviewLoop, message string) error {
	if l.AuthorTaskID == "" {
		return nil
	}
	return s.opts.Agents.QueueMessage(ctx, l.AuthorTaskID, message)
}
