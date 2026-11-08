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

// ReviewPassConfig is a worker's review settings (config review_passes).
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

// ReviewGitHubPR is an open pull request of a handoff's branch.
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
}

// ReviewGitHub is what a review pass asks of GitHub: ReviewPublisher, as
// the reviewer App.
type ReviewGitHub interface {
	// PullRequests are the open pull requests whose heads are branches, in
	// the order of branches.
	PullRequests(ctx context.Context, repo string, branches []string) ([]ReviewGitHubPR, error)
	PostReview(ctx context.Context, post ReviewPost) (PostedReview, error)
	PostIssueComment(ctx context.Context, who GitHubIdentity, repo string, number int, body string) (int64, error)
	PollComments(ctx context.Context, repo string, number int, pages PollPages) (CommentPoll, error)
	Reply(ctx context.Context, who GitHubIdentity, repo string, number int, commentID int64, body string) error
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

// ReviewPassOptions configure the review loop of a worker.
type ReviewPassOptions struct {
	Config func() ReviewPassConfig
	Agents ReviewAgents
	Board  interface {
		MoveCard(ctx context.Context, id, column string) error
	}
	GitHub   ReviewGitHub
	Checkout ReviewCheckout
	Bus      *events.Bus
	Now      func() time.Time
	// Run runs posting in the background; the caller's goroutine when nil.
	Run func(func())
	// Overlay says whether the reviewer's checkout gets a throwaway layer
	// (CheckoutOverlayProber); nil means it does not.
	Overlay func() bool
	// Advert is this worker's review passes as /api/meta says them, which
	// a main's override is checked against; nil refuses every override.
	Advert func() *ReviewPassesAdvert
	// ResumeAuthor brings back an author that is not running
	// (HandoffWorkerService.ResumeAuthor); nil cannot.
	ResumeAuthor func(ctx context.Context, taskID string) error
}

// ReviewPassService runs review passes on a worker's make-a-change
// handoffs: it notices the author finished, pins the pull requests' heads,
// starts a reviewer in the card's reviewer slot, takes its submit_review,
// posts it to GitHub as the reviewer App, and tells the loop what comes
// next. Tick drives it; the runtime calls it on card events and on a timer.
type ReviewPassService struct {
	db   *store.Store
	opts ReviewPassOptions
	mu   sync.Mutex // one Tick or submission at a time
}

// NewReviewPassService makes the review loop of a worker.
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

// applies says whether a handoff runs review passes: a make-a-change one,
// unless its main turned them off for it.
func (s *ReviewPassService) applies(h *store.Handoff) bool {
	if !s.config().Enabled || h.Purpose != HandoffMakeChange || h.ClosedAt != nil {
		return false
	}
	loop, err := s.db.GetHandoffReview(context.Background(), h.TaskID)
	if err != nil {
		return false
	}
	return loopSettings(loop, s.config().Defaults).Enabled
}

// DoneAllowed says whether a handoff whose card is in Done may be done: one
// without review passes always, one with them only after a clean pass whose
// counted checks all passed or were answered by a reply (or when it had no
// pull request to review).
func (s *ReviewPassService) DoneAllowed(ctx context.Context, h *store.Handoff) bool {
	if !s.applies(h) {
		return true
	}
	loop, err := s.db.GetHandoffReview(ctx, h.TaskID)
	if err != nil || loop == nil {
		return false
	}
	switch loop.Phase {
	case ReviewPhaseSkipped:
		return true
	case ReviewPhaseClean:
		return s.checksHandled(ctx, loop)
	}
	return false
}

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

// Tick moves every open handoff's review loop on: a finished author's card
// starts a pass, a reviewer past its time or gone is unable, and queued
// passes start as reviewers free up.
func (s *ReviewPassService) Tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config().Enabled {
		return
	}
	_ = s.opts.Agents.ReconcileSessions(ctx)
	open, err := s.db.ListOpenHandoffs(ctx)
	if err != nil {
		log.Printf("review passes: %v", err)
		return
	}
	for i := range open {
		h := &open[i]
		if !s.applies(h) || h.State == store.HandoffPending || h.State == store.HandoffFailed || h.State == store.HandoffReturning {
			continue
		}
		s.postAnswers(ctx, h)
		s.pollComments(ctx, h)
		s.pollChecks(ctx, h)
		if err := s.step(ctx, h); err != nil {
			log.Printf("review passes: %s: %v", h.TaskID, err)
		}
	}
	s.startQueued(ctx)
}

// step moves one handoff's loop on.
func (s *ReviewPassService) step(ctx context.Context, h *store.Handoff) error {
	cfg := s.config()
	loop, err := s.db.GetHandoffReview(ctx, h.TaskID)
	if err != nil {
		return err
	}
	if loop == nil {
		loop = &store.HandoffReview{TaskID: h.TaskID, Settings: "{}"}
	}
	task, err := s.db.GetTask(ctx, h.TaskID)
	if err != nil {
		return err
	}
	inDone := strings.EqualFold(task.Status, cfg.DoneColumn)
	switch loop.Phase {
	case ReviewPhaseAuthor:
		if err := s.deliverComments(ctx, h, loop); err != nil {
			return err
		}
		if !inDone || RefuseUnansweredReview(ctx, s.db, h.TaskID) != nil {
			return nil
		}
		loop.Phase = ReviewPhaseReady
		return s.ready(ctx, h, loop)
	case ReviewPhaseReady:
		return s.ready(ctx, h, loop)
	case ReviewPhaseReviewing:
		return s.watchReviewer(ctx, h, loop)
	case ReviewPhaseWaitingForAuthor:
		if err := s.deliverComments(ctx, h, loop); err != nil {
			return err
		}
		return s.feedback(ctx, h, loop, inDone)
	case ReviewPhaseClean:
		return s.checksReopen(ctx, h, loop)
	}
	return nil
}

// authorIdle says whether the author is between turns: idle, or not
// running.
func (s *ReviewPassService) authorIdle(ctx context.Context, taskID string) bool {
	session, err := s.opts.Agents.GetAgentSession(ctx, taskID)
	if err != nil || session == nil {
		return true
	}
	return session.Activity == ""
}

// ready starts a pass when the author is idle and its checkout is what
// GitHub has: each pull request's branch at its head, the tree clean.
func (s *ReviewPassService) ready(ctx context.Context, h *store.Handoff, loop *store.HandoffReview) error {
	if loop.Mismatches > 0 && !loop.AuthorBusySinceNudge {
		// The author was told to push or discard; wait until it has
		// worked on it, or for a while when it does not answer.
		if !s.authorIdle(ctx, h.TaskID) {
			loop.AuthorBusySinceNudge = true
			return s.save(ctx, loop)
		}
		if nudged, err := time.Parse(time.RFC3339, loop.NudgedAt); err == nil && s.opts.Now().Sub(nudged) < 10*time.Minute {
			return s.save(ctx, loop)
		}
	}
	if !s.authorIdle(ctx, h.TaskID) {
		return s.save(ctx, loop)
	}
	dir, branches, err := s.opts.Checkout.Branches(ctx, h.TaskID, h.Repo)
	if err != nil {
		return s.stuck(ctx, h, loop, "Legato could not read the author's checkout: "+err.Error())
	}
	prs, err := s.opts.GitHub.PullRequests(ctx, h.Repo, branches)
	if err != nil {
		return s.save(ctx, loop) // GitHub may answer next time
	}
	if len(prs) == 0 {
		// Nothing to review: the handoff is done as without review passes.
		loop.Phase = ReviewPhaseSkipped
		return s.save(ctx, loop)
	}
	var mismatch []string
	for _, pr := range prs {
		tip, err := s.opts.Checkout.Tip(ctx, dir, pr.Branch)
		if err != nil || tip != pr.HeadSHA {
			mismatch = append(mismatch, fmt.Sprintf("%s is at %s here and %s on GitHub (#%d)", pr.Branch, short(tip), short(pr.HeadSHA), pr.Number))
		}
	}
	if _, clean, err := s.opts.Checkout.State(ctx, dir); err != nil || !clean {
		mismatch = append(mismatch, "the working tree has changes that are not committed")
	}
	if len(mismatch) > 0 {
		return s.mismatch(ctx, h, loop, mismatch)
	}
	loop.Mismatches, loop.AuthorBusySinceNudge, loop.NudgedAt = 0, false, ""
	settings := loopSettings(loop, s.config().Defaults)
	if loop.Pass >= settings.MaxPasses && settings.MaxPasses > 0 {
		reason := fmt.Sprintf("the review reached its limit of %d passes", settings.MaxPasses)
		if findings, err := s.db.ListHandoffReviewFindings(ctx, h.TaskID); err == nil && notConverging(findings, loop.Pass) {
			reason = fmt.Sprintf("passes %d and %d each found new blocking problems in the author's fixes, so the review stopped before pass %d for you to decide", loop.Pass-1, loop.Pass, loop.Pass+1)
		}
		return s.stuck(ctx, h, loop, reason)
	}
	if s.running(ctx) >= s.config().MaxConcurrent {
		if loop.Phase != ReviewPhaseWaitingForReviewer {
			loop.Phase, loop.QueuedAt = ReviewPhaseWaitingForReviewer, s.now()
			return s.saveChanged(ctx, loop)
		}
		return nil
	}
	return s.startPass(ctx, h, loop, dir, prs)
}

// mismatch tells the author once to push or discard; a second miss is
// stuck.
func (s *ReviewPassService) mismatch(ctx context.Context, h *store.Handoff, loop *store.HandoffReview, why []string) error {
	if loop.Mismatches > 0 {
		return s.stuck(ctx, h, loop, "the checkout still does not match GitHub after the author was told: "+strings.Join(why, "; "))
	}
	message := ReviewTag + " Push or discard before review: " + strings.Join(why, "; ") + ". Then end your turn; the review starts when your branches on GitHub match your checkout."
	if err := s.opts.Agents.QueueMessage(ctx, h.TaskID, message); err != nil {
		return s.stuck(ctx, h, loop, "the author could not be told to push before review: "+err.Error())
	}
	loop.Phase, loop.Mismatches, loop.NudgedAt, loop.AuthorBusySinceNudge = ReviewPhaseReady, 1, s.now(), false
	return s.saveChanged(ctx, loop)
}

// running counts the reviewers at work on this worker.
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
		h, err := s.db.OpenHandoffForTask(ctx, loop.TaskID)
		if err != nil {
			continue
		}
		loop.Phase = ReviewPhaseReady
		if err := s.ready(ctx, h, loop); err != nil {
			log.Printf("review passes: %s: %v", loop.TaskID, err)
		}
	}
}

// startPass pins the heads, moves the card to the review column, and starts
// a fresh reviewer in the card's reviewer slot.
func (s *ReviewPassService) startPass(ctx context.Context, h *store.Handoff, loop *store.HandoffReview, dir string, prs []ReviewGitHubPR) error {
	cfg := s.config()
	settings := loopSettings(loop, cfg.Defaults)
	pass := loop.Pass + 1
	head, _, err := s.opts.Checkout.State(ctx, dir)
	if err != nil {
		return s.stuck(ctx, h, loop, "Legato could not read the author's checkout: "+err.Error())
	}
	record := store.HandoffReviewPass{TaskID: h.TaskID, Pass: pass, State: ReviewPassReviewing, Checkout: dir, HeadSHA: head, StartedAt: s.now()}
	var pinned []store.HandoffReviewPR
	for _, pr := range prs {
		if err := s.opts.Checkout.Pin(ctx, dir, pinRef(h.TaskID, pass, pr.Number), pr.HeadSHA); err != nil {
			return s.stuck(ctx, h, loop, "Legato could not pin the head of #"+fmt.Sprint(pr.Number)+": "+err.Error())
		}
		pinned = append(pinned, store.HandoffReviewPR{TaskID: h.TaskID, Pass: pass, Repo: h.Repo, Number: pr.Number, Branch: pr.Branch,
			Base: pr.Base, Title: pr.Title, Body: pr.Body, URL: pr.URL, HeadSHA: pr.HeadSHA, Author: pr.Author})
	}
	if err := s.db.InsertHandoffReviewPass(ctx, record, pinned); err != nil {
		return err
	}
	loop.Pass, loop.Phase, loop.QueuedAt, loop.Reason = pass, ReviewPhaseReviewing, "", ""
	if loop.ChecksHeads != checkHeads(pinned) {
		// The checks of the heads this pass reviews, read now.
		s.readChecks(ctx, h, loop, pinned)
	}
	if err := s.saveChanged(ctx, loop); err != nil {
		return err
	}
	if err := s.opts.Board.MoveCard(ctx, h.TaskID, cfg.ReviewColumn); err != nil {
		log.Printf("review passes: moving %s to %s: %v", h.TaskID, cfg.ReviewColumn, err)
	}
	brief, err := s.brief(ctx, h, pass, pinned, settings)
	if err != nil {
		return s.unable(ctx, h, loop, record, "Legato could not write the reviewer's brief: "+err.Error())
	}
	opts := AgentSpawnOptions{AgentKind: settings.Agent, Model: settings.Model, Tier: settings.Tier, Slot: SlotReviewer,
		WorkingDir: dir, Assignment: brief, Unattended: true, ReadOnlyCheckout: true, ThrowawayCheckout: true,
		ExecutionKind: store.ExecutionInteractive}
	if !s.resumeReviewer(ctx, h.TaskID, pass, settings, opts) {
		if err := s.opts.Agents.SpawnAgent(ctx, h.TaskID, 0, 0, opts); err != nil {
			return s.unable(ctx, h, loop, record, "the reviewer could not start: "+err.Error())
		}
	}
	if session, err := s.opts.Agents.GetAgentSessionInSlot(ctx, h.TaskID, SlotReviewer); err == nil {
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
	if err := s.opts.Agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		log.Printf("review passes: %s: resuming the reviewer for pass %d, starting a fresh one: %v", taskID, pass, err)
		return false
	}
	return true
}

// watchReviewer makes a reviewer that ran past its time, or ended without
// submitting, unable.
func (s *ReviewPassService) watchReviewer(ctx context.Context, h *store.Handoff, loop *store.HandoffReview) error {
	pass, err := s.db.GetHandoffReviewPass(ctx, h.TaskID, loop.Pass)
	if err != nil {
		return err
	}
	if pass.State == ReviewPassSubmitted {
		return s.post(ctx, h.TaskID) // a post that failed, tried again
	}
	if pass.State != ReviewPassReviewing {
		return nil
	}
	started, _ := time.Parse(time.RFC3339, pass.StartedAt)
	if limit := s.config().PassTimeout; limit > 0 && s.opts.Now().Sub(started) > limit {
		return s.unable(ctx, h, loop, *pass, fmt.Sprintf("the reviewer ran past %s", limit))
	}
	if session, err := s.opts.Agents.GetAgentSessionInSlot(ctx, h.TaskID, SlotReviewer); err != nil || session == nil {
		return s.unable(ctx, h, loop, *pass, "the reviewer exited without submitting")
	}
	return nil
}

// unable ends a pass with nothing posted and makes the review stuck.
func (s *ReviewPassService) unable(ctx context.Context, h *store.Handoff, loop *store.HandoffReview, pass store.HandoffReviewPass, reason string) error {
	_ = s.opts.Agents.KillAgentInSlot(ctx, h.TaskID, SlotReviewer)
	pass.State, pass.Reason, pass.EndedAt = ReviewPassUnable, reason, s.now()
	if err := s.db.UpdateHandoffReviewPass(ctx, pass); err != nil {
		return err
	}
	return s.stuck(ctx, h, loop, reason)
}

// stuck stops the loop for the person: the main shows it as needs you.
func (s *ReviewPassService) stuck(ctx context.Context, h *store.Handoff, loop *store.HandoffReview, reason string) error {
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
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventHandoffChanged, Payload: loop.TaskID, At: s.opts.Now()})
	}
	return nil
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
// card's running reviewer.
var ErrNotTheReviewer = errors.New("only the card's reviewer submits a review")
