package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// The review engine's view of the loop record (review_loops.go), keyed by
// the task that hosts each loop (host_task): a handed-off card, or a
// workflow run's own task. A task's loop is its open one, else its newest
// (closed ones are history). Nothing here reads or writes handoff_review*:
// those tables are left from before the conversion (migration 148) until
// they are dropped. See docs/ai/database.md § Loop record › The engine's
// view.

// HandoffReview is one task's review loop, as the engine reads and writes
// it: the loop record's phase, counters and settings.
type HandoffReview struct {
	// LoopID is the loop record's ID, and MarkerKey its marker_key, which
	// names what the loop leaves on GitHub; neither is written.
	LoopID               string `db:"-"`
	MarkerKey            string `db:"-"`
	TaskID               string `db:"task_id"`
	Settings             string `db:"settings"`
	Phase                string `db:"phase"`
	Pass                 int    `db:"pass"`
	Mismatches           int    `db:"mismatches"`
	NudgedAt             string `db:"nudged_at"`
	AuthorBusySinceNudge bool   `db:"author_busy_since_nudge"`
	Reason               string `db:"reason"`
	QueuedAt             string `db:"queued_at"`
	UpdatedAt            string `db:"updated_at"`
	// DeliveredPass is the last pass whose feedback the author was told of.
	DeliveredPass int `db:"delivered_pass"`
	// PolledAt is when the pull requests were last polled for comments,
	// or when the next poll may be (after a rate limit); LastNewAt when a
	// new counted comment last came in; SkippedComments how many comments
	// do not count.
	PolledAt        string `db:"polled_at"`
	LastNewAt       string `db:"last_new_at"`
	SkippedComments int    `db:"skipped_comments"`
	// ChecksPolledAt is when the counted checks were last read, on
	// ChecksHeads (each pull request's "number:head", in order), with
	// ChecksPending of them not finished.
	ChecksPolledAt string `db:"checks_polled_at"`
	ChecksHeads    string `db:"checks_heads"`
	ChecksPending  int    `db:"checks_pending"`
}

// HandoffReviewPass is one review pass.
type HandoffReviewPass struct {
	TaskID string `db:"task_id"`
	// MarkerKey is its loop's marker_key, which its markers start with; it
	// is not written.
	MarkerKey   string `db:"-"`
	Pass        int    `db:"pass"`
	State       string `db:"state"`
	Verdict     string `db:"verdict"`
	Summary     string `db:"summary"`
	Reason      string `db:"reason"`
	ExecutionID string `db:"execution_id"`
	Checkout    string `db:"checkout"`
	HeadSHA     string `db:"head_sha"`
	StartedAt   string `db:"started_at"`
	EndedAt     string `db:"ended_at"`
	// Attempt is how many times this pass's number was pinned before: a
	// pass discarded because a head moved is pinned again under the same
	// number (InsertHandoffReviewPass).
	Attempt int `db:"attempt"`
	// PostingStartedAt is when the pass's post was first tried, and
	// PostAttempts how many tries ended with GitHub's answer unknown (a
	// 5xx, a timeout): the pass stays submitted and is tried again by its
	// markers, up to a limit (LEG-561).
	PostingStartedAt string `db:"posting_started_at"`
	PostAttempts     int    `db:"post_attempts"`
}

// HandoffReviewPR is a pull request of a pass, pinned at its head (one of
// the pass's pins).
type HandoffReviewPR struct {
	TaskID    string `db:"task_id"`
	Pass      int    `db:"pass"`
	Repo      string `db:"repo"`
	Number    int    `db:"number"`
	Branch    string `db:"branch"`
	Base      string `db:"base"`
	Title     string `db:"title"`
	Body      string `db:"body"`
	URL       string `db:"url"`
	HeadSHA   string `db:"head_sha"`
	ReviewID  int64  `db:"review_id"`
	ReviewURL string `db:"review_url"`
	// Author is the pull request's author on GitHub (the author App).
	Author string `db:"author"`
	// Event is the pass's review of it: APPROVE or COMMENT as posted,
	// kept when an earlier approval stands; "" before it is posted.
	Event string `db:"event"`
	// LocalBranch is the branch of the reviewer's checkout the head was
	// put on when it is not Branch; "" otherwise.
	LocalBranch string `db:"local_branch"`
	// HeadRepo is the repository (owner/name) the head branch is in, when
	// read; a fork's differs from Repo.
	HeadRepo string `db:"head_repo"`
}

// HandoffReviewFinding is one finding of a pass, with its ruling by a later
// one, keyed by its loop (TaskID) and ID.
type HandoffReviewFinding struct {
	ID        string `db:"id"`
	TaskID    string `db:"task_id"`
	Pass      int    `db:"pass"`
	PR        int    `db:"pr"`
	Severity  string `db:"severity"`
	Title     string `db:"title"`
	Body      string `db:"body"`
	Path      string `db:"path"`
	Line      int    `db:"line"`
	StartLine int    `db:"start_line"`
	Side      string `db:"side"`
	CommentID int64  `db:"comment_id"`
	Status    string `db:"status"`
	RuledPass int    `db:"ruled_pass"`
	Ruling    string `db:"ruling"`
}

// HandoffReviewItem is one thing the author of a reviewed loop must handle
// (a finding, a counted comment, a failing check), and its answer. It is
// keyed by its loop (TaskID) and ID.
type HandoffReviewItem struct {
	ID              string `db:"id" json:"id"`
	TaskID          string `db:"task_id" json:"-"`
	Kind            string `db:"kind" json:"kind"`
	Pass            int    `db:"pass" json:"pass"`
	Repo            string `db:"repo" json:"repo,omitempty"`
	PR              int    `db:"pr" json:"pr,omitempty"`
	Path            string `db:"path" json:"path,omitempty"`
	Line            int    `db:"line" json:"line,omitempty"`
	URL             string `db:"url" json:"url,omitempty"`
	Version         int    `db:"version" json:"version"`
	Outdated        bool   `db:"outdated" json:"outdated,omitempty"`
	Author          string `db:"author" json:"author,omitempty"`
	Title           string `db:"title" json:"title,omitempty"`
	Body            string `db:"body" json:"body,omitempty"`
	ThreadCommentID int64  `db:"thread_comment_id" json:"-"`
	SourceID        string `db:"source_id" json:"-"`
	Answer          string `db:"answer" json:"answer,omitempty"`
	AnswerNote      string `db:"answer_note" json:"answer_note,omitempty"`
	AnsweredAt      string `db:"answered_at" json:"answered_at,omitempty"`
	Posted          bool   `db:"posted" json:"posted,omitempty"`
	// AnswerMarker names this answer on GitHub: a new one with each answer,
	// so an answer GitHub took is found, not posted again.
	AnswerMarker string `db:"answer_marker" json:"-"`
	Closed       bool   `db:"closed" json:"-"`
	CreatedAt    string `db:"created_at" json:"-"`
	// Delivered says the author was told of this version of the item.
	Delivered bool `db:"delivered" json:"-"`
}

// currentReviewLoop is a task's loop: its open one, else its newest; nil
// when it has none.
func currentReviewLoop(ctx context.Context, q sqlx.QueryerContext, taskID string) (*ReviewLoop, error) {
	var l ReviewLoop
	err := sqlx.GetContext(ctx, q, &l, `SELECT * FROM review_loops WHERE host_task = ?
		ORDER BY state = 'open' DESC, created_at DESC, rowid DESC LIMIT 1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CurrentReviewLoop returns the loop a task hosts: its open one, else its
// newest; nil when it hosts none.
func (s *Store) CurrentReviewLoop(ctx context.Context, hostTask string) (*ReviewLoop, error) {
	return currentReviewLoop(ctx, s.db, hostTask)
}

// UpdateHostedReviewLoop runs change as one compare-and-swap transition of
// the loop hostTask hosts (UpdateReviewLoop), read in the same transaction:
// ErrNotFound when it hosts none, ErrReviewLoopClosed when its newest is
// closed. A transition that loses to another writer is tried again on the
// loop as it then stands.
func (s *Store) UpdateHostedReviewLoop(ctx context.Context, hostTask string, change func(*ReviewLoopTx) error) (*ReviewLoop, error) {
	var out *ReviewLoop
	err := s.reviewTransition(ctx, hostTask, false, func(t *ReviewLoopTx) error {
		if err := change(t); err != nil {
			return err
		}
		out = t.Loop
		return nil
	})
	if err != nil {
		return nil, err
	}
	return getReviewLoop(ctx, s.db, out.ID)
}

// reviewTransition is one compare-and-swap transition of a task's loop,
// found in the transaction. With create, a task with no loop at all gets
// one first (newHostedReviewLoop). A lost race (ErrConflict) is tried
// again, a few times.
func (s *Store) reviewTransition(ctx context.Context, taskID string, create bool, change func(*ReviewLoopTx) error) error {
	var err error
	for try := 0; try < 5; try++ {
		err = withTx(ctx, s.db, func(tx *sqlx.Tx) error {
			l, err := hostedLoop(ctx, tx, taskID, create)
			if err != nil {
				return err
			}
			if l.State != ReviewLoopOpenState {
				return ErrReviewLoopClosed
			}
			read := *l
			rt := &ReviewLoopTx{Loop: l, ctx: ctx, tx: tx}
			if err := change(rt); err != nil {
				return err
			}
			return writeReviewLoop(ctx, tx, read, *rt.Loop)
		})
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return err
}

// reviewWrite runs write in one transaction on the rows of a task's loop
// (passes, findings, items), leaving the loop itself as it is. With
// create, a task with no loop at all gets one first.
func (s *Store) reviewWrite(ctx context.Context, taskID string, create bool, write func(tx *sqlx.Tx, loop *ReviewLoop) error) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		l, err := hostedLoop(ctx, tx, taskID, create)
		if err != nil {
			return err
		}
		return write(tx, l)
	})
}

// hostedLoop is a task's loop in a transaction, made when create and it has
// none; ErrNotFound otherwise.
func hostedLoop(ctx context.Context, tx *sqlx.Tx, taskID string, create bool) (*ReviewLoop, error) {
	l, err := currentReviewLoop(ctx, tx, taskID)
	if err != nil || l != nil {
		return l, err
	}
	if !create {
		return nil, ErrNotFound
	}
	return newHostedReviewLoop(ctx, tx, taskID)
}

// newHostedReviewLoop opens a loop on a task that has none: a handed-off
// card's (owned by its handoff, the open one else the newest), with no
// pull requests and no claims yet. A workflow run's loop is opened with
// OpenReviewLoop instead. Its markers start with its own ID, as a new run
// loop's do, so a later loop on the same card never finds this one's
// reviews for its own (LEG-605 review).
func newHostedReviewLoop(ctx context.Context, tx *sqlx.Tx, taskID string) (*ReviewLoop, error) {
	var h struct {
		MainInstanceID string `db:"main_instance_id"`
		HandoffID      string `db:"handoff_id"`
		Repo           string `db:"repo"`
	}
	err := tx.GetContext(ctx, &h, `SELECT main_instance_id, handoff_id, repo FROM handoffs WHERE task_id = ?
		ORDER BY closed_at IS NULL DESC, created_at DESC, rowid DESC LIMIT 1`, taskID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := reviewLoopNow()
	id := NewReviewLoopID()
	l := ReviewLoop{ID: id, Rev: 1, OwnerKind: ReviewLoopOwnerHandoff, HandoffID: h.HandoffID,
		MainInstanceID: h.MainInstanceID, HostTask: taskID, Repo: h.Repo, State: ReviewLoopOpenState,
		Replies: "[]", MarkerKey: id, CreatedAt: now, UpdatedAt: now}
	if h.HandoffID != "" {
		l.AuthorTask, l.LinkedTask = taskID, taskID
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_loops (`+reviewLoopColumns+`) VALUES (`+reviewLoopValues+`)`, l); err != nil {
		return nil, err
	}
	return &l, nil
}

// reviewSettings are a loop's settings as the engine keeps them (JSON).
type reviewSettings struct {
	Enabled      bool   `json:"enabled"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	Tier         string `json:"tier,omitempty"`
	Skill        string `json:"skill,omitempty"`
	MaxPasses    int    `json:"max_passes"`
	SkillManaged bool   `json:"skill_managed,omitempty"`
}

// loopSettingsJSON is a loop's settings columns as the engine's JSON: ""
// when none was set (the engine then uses its defaults).
func loopSettingsJSON(l *ReviewLoop) string {
	if !l.PassesOff && l.Reviewer == "" && l.ReviewerModel == "" && l.ReviewerTier == "" && l.Skill == "" && l.MaxPasses == 0 && !l.SkillManaged {
		return ""
	}
	data, _ := json.Marshal(reviewSettings{Enabled: !l.PassesOff, Agent: l.Reviewer, Model: l.ReviewerModel, Tier: l.ReviewerTier,
		Skill: l.Skill, MaxPasses: l.MaxPasses, SkillManaged: l.SkillManaged})
	return string(data)
}

// setLoopSettings writes the engine's settings JSON to a loop's columns;
// "" or "{}" is none set.
func setLoopSettings(l *ReviewLoop, settings string) {
	v := reviewSettings{Enabled: true}
	if settings != "" {
		_ = json.Unmarshal([]byte(settings), &v)
	}
	l.PassesOff, l.Reviewer, l.ReviewerModel, l.ReviewerTier = !v.Enabled, v.Agent, v.Model, v.Tier
	l.Skill, l.MaxPasses, l.SkillManaged = v.Skill, v.MaxPasses, v.SkillManaged
}

func handoffReviewOf(l *ReviewLoop) *HandoffReview {
	return &HandoffReview{LoopID: l.ID, MarkerKey: l.MarkerKey, TaskID: l.HostTask, Settings: loopSettingsJSON(l), Phase: l.Phase, Pass: l.Pass,
		Mismatches: l.Mismatches, NudgedAt: l.NudgedAt, AuthorBusySinceNudge: l.AuthorBusySinceNudge, Reason: l.Reason,
		QueuedAt: l.QueuedAt, UpdatedAt: l.UpdatedAt, DeliveredPass: l.DeliveredPass, PolledAt: l.PolledAt,
		LastNewAt: l.LastNewAt, SkippedComments: l.SkippedComments, ChecksPolledAt: l.ChecksPolledAt,
		ChecksHeads: l.ChecksHeads, ChecksPending: l.ChecksPending}
}

// applyHandoffReview writes what the engine keeps of a loop onto its record.
func applyHandoffReview(l *ReviewLoop, r HandoffReview) {
	setLoopSettings(l, r.Settings)
	l.Phase, l.Pass, l.Mismatches, l.NudgedAt = r.Phase, r.Pass, r.Mismatches, r.NudgedAt
	l.AuthorBusySinceNudge, l.Reason, l.QueuedAt, l.UpdatedAt = r.AuthorBusySinceNudge, r.Reason, r.QueuedAt, r.UpdatedAt
	l.DeliveredPass, l.PolledAt, l.LastNewAt, l.SkippedComments = r.DeliveredPass, r.PolledAt, r.LastNewAt, r.SkippedComments
	l.ChecksPolledAt, l.ChecksHeads, l.ChecksPending = r.ChecksPolledAt, r.ChecksHeads, r.ChecksPending
}

// GetHandoffReview returns a task's review loop, nil, nil when it has none.
func (s *Store) GetHandoffReview(ctx context.Context, taskID string) (*HandoffReview, error) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil || l == nil {
		return nil, err
	}
	return handoffReviewOf(l), nil
}

// ListHandoffReviews returns every open review loop, oldest queued first.
func (s *Store) ListHandoffReviews(ctx context.Context) ([]HandoffReview, error) {
	var loops []ReviewLoop
	if err := s.db.SelectContext(ctx, &loops, `SELECT * FROM review_loops WHERE state = 'open' ORDER BY queued_at, host_task, created_at`); err != nil {
		return nil, err
	}
	out := make([]HandoffReview, 0, len(loops))
	for i := range loops {
		out = append(out, *handoffReviewOf(&loops[i]))
	}
	return out, nil
}

// SaveHandoffReview writes a task's review loop: its open loop, or a new
// one when the task has none (newHostedReviewLoop). A task whose newest
// loop is closed is ErrReviewLoopClosed.
func (s *Store) SaveHandoffReview(ctx context.Context, r HandoffReview) error {
	return s.reviewTransition(ctx, r.TaskID, true, func(t *ReviewLoopTx) error {
		applyHandoffReview(t.Loop, r)
		return nil
	})
}

// DeleteHandoffReview forgets every loop a task hosts, with their passes.
func (s *Store) DeleteHandoffReview(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM review_loops WHERE host_task = ?`, taskID)
	return err
}

// HandoffReviewOwner is who a task's review loop writes to GitHub for: the
// workflow step attempt that opened it (AttemptID), or the handoff the card
// is (MainInstanceID and HandoffID).
type HandoffReviewOwner struct {
	AttemptID      string
	MainInstanceID string
	HandoffID      string
}

// ErrReviewPassNotSubmitted is a pass whose review may no longer be posted:
// it was set aside (discarded) or ended since it was submitted.
var ErrReviewPassNotSubmitted = errors.New("the review pass is no longer waiting to be posted")

// CheckHandoffReviewWrite is CheckReviewLoopWrite on the loop a task hosts
// (ErrReviewLoopClosed when it hosts none), its owner a step attempt (of the
// loop's run) or a handoff.
func (s *Store) CheckHandoffReviewWrite(ctx context.Context, taskID string, pass int, owner HandoffReviewOwner) error {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil {
		return err
	}
	if l == nil {
		return ErrReviewLoopClosed
	}
	o := ReviewLoopOwner{Kind: ReviewLoopOwnerHandoff, HandoffID: owner.HandoffID, MainInstanceID: owner.MainInstanceID}
	if owner.AttemptID != "" {
		o = ReviewLoopOwner{Kind: ReviewLoopOwnerRun, RunID: l.RunID, AttemptID: owner.AttemptID}
	}
	return s.CheckReviewLoopWrite(ctx, l.ID, pass, o)
}

// CheckReviewLoopWrite is the re-check before each GitHub write of a loop
// (spec LEG-559 D11), in one transaction: the loop is open and has not
// ended (ErrReviewLoopClosed); pass, when not 0, is still submitted
// (ErrReviewPassNotSubmitted); and owner still owns it
// (ErrReviewLoopNotOwned): the loop names it (CheckReviewLoopOwner; a
// handoff loop made before its handoff names none), and its step attempt
// still waits in a run not ended, or its handoff is still open on this
// worker and not returning. Nil allows the write. What happens between this
// check and the write is not covered.
func (s *Store) CheckReviewLoopWrite(ctx context.Context, loopID string, pass int, owner ReviewLoopOwner) error {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	l, err := getReviewLoop(ctx, tx, loopID)
	if errors.Is(err, ErrNotFound) {
		return ErrReviewLoopClosed
	}
	if err != nil {
		return err
	}
	if l.State != ReviewLoopOpenState || l.Phase == "ended" {
		return ErrReviewLoopClosed
	}
	if pass > 0 {
		var state string
		switch err := tx.GetContext(ctx, &state, `SELECT state FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, l.ID, pass); {
		case errors.Is(err, sql.ErrNoRows):
			return ErrReviewPassNotSubmitted
		case err != nil:
			return err
		case state != ReviewLoopPassSubmitted && state != ReviewLoopPassPosting:
			return ErrReviewPassNotSubmitted
		}
	}
	named := owner
	if owner.Kind == ReviewLoopOwnerHandoff && l.HandoffID == "" {
		named.HandoffID, named.MainInstanceID = "", ""
	}
	if !named.owns(l) {
		return ErrReviewLoopNotOwned
	}
	var live int
	switch owner.Kind {
	case ReviewLoopOwnerRun:
		err = tx.GetContext(ctx, &live, `SELECT COUNT(*) FROM workflow_step_attempts a JOIN workflow_runs r ON r.id = a.run_id
			WHERE a.id = ? AND a.task_id = ? AND a.status = 'waiting' AND r.status IN ('running', 'waiting')`, l.AttemptID, l.HostTask)
	case ReviewLoopOwnerHandoff:
		err = tx.GetContext(ctx, &live, `SELECT COUNT(*) FROM handoffs
			WHERE main_instance_id = ? AND handoff_id = ? AND task_id = ? AND closed_at IS NULL AND state IN (?, ?)`,
			owner.MainInstanceID, owner.HandoffID, l.HostTask, HandoffInProgress, HandoffDone)
	}
	if err != nil {
		return err
	}
	if live == 0 {
		return ErrReviewLoopNotOwned
	}
	return nil
}

// EndHandoffReview ends a task's review loop that a pass runs on, waits to
// run, or is about to start (phase ready, waiting_for_reviewer or
// reviewing), in one transaction: the loop's phase becomes ended with
// reason, and a pass in flight (reviewing or submitted) is discarded with
// passReason. Such a loop holds no reviewer slot after. A loop in any other
// phase (the author's turn, clean, stuck) keeps it, and a closed loop is
// left as it is. It says whether the loop ended.
func (s *Store) EndHandoffReview(ctx context.Context, taskID, reason, passReason, at string) (bool, error) {
	ended := false
	err := s.reviewTransition(ctx, taskID, false, func(t *ReviewLoopTx) error {
		if _, err := t.tx.ExecContext(ctx, `UPDATE review_loop_passes SET state = 'discarded', reason = ?, ended_at = ?
			WHERE loop_id = ? AND state IN `+reviewLoopInFlight, passReason, at, t.Loop.ID); err != nil {
			return err
		}
		switch t.Loop.Phase {
		case "ready", "waiting_for_reviewer", "reviewing":
			t.Loop.Phase, t.Loop.Reason, t.Loop.QueuedAt, t.Loop.UpdatedAt = "ended", reason, "", at
			ended = true
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrReviewLoopClosed) {
		return false, nil
	}
	return ended, err
}

// CloseHostedReviewLoop closes the loop a task hosts in one transaction
// (as CloseReviewLoop), its phase first set to phase with reason when phase
// is not "". A task with no open loop changes nothing.
func (s *Store) CloseHostedReviewLoop(ctx context.Context, taskID, phase, reason, outcome, at string) (bool, error) {
	if at == "" {
		at = reviewLoopNow()
	}
	closed := false
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		l, err := currentReviewLoop(ctx, tx, taskID)
		if err != nil || l == nil || l.State != ReviewLoopOpenState {
			return err
		}
		if phase != "" {
			next := *l
			next.Phase, next.Reason, next.QueuedAt, next.UpdatedAt = phase, reason, "", at
			if err := writeReviewLoop(ctx, tx, *l, next); err != nil {
				return err
			}
		}
		closed = true
		return closeReviewLoopTx(ctx, tx, l.ID, outcome, at)
	})
	if err != nil {
		return false, err
	}
	return closed, nil
}

func passOf(l *ReviewLoop, p ReviewLoopPass) HandoffReviewPass {
	return HandoffReviewPass{TaskID: l.HostTask, MarkerKey: l.MarkerKey, Pass: p.Pass, State: p.State, Verdict: p.Verdict, Summary: p.Summary, Reason: p.Reason,
		ExecutionID: p.ExecutionID, Checkout: p.Checkout, HeadSHA: p.HeadSHA, StartedAt: p.StartedAt, EndedAt: p.EndedAt,
		Attempt: p.Attempt, PostingStartedAt: p.PostingStartedAt, PostAttempts: p.Attempts}
}

func pinsOf(prs []HandoffReviewPR) ReviewLoopPins {
	pins := ReviewLoopPins{}
	for _, pr := range prs {
		pins = append(pins, ReviewLoopPin{Number: pr.Number, Repo: pr.Repo, Branch: pr.Branch, Base: pr.Base, Title: pr.Title,
			Body: pr.Body, URL: pr.URL, HeadSHA: pr.HeadSHA, Author: pr.Author, LocalBranch: pr.LocalBranch, HeadRepo: pr.HeadRepo,
			ReviewID: pr.ReviewID, ReviewURL: pr.ReviewURL, Event: pr.Event})
	}
	return pins
}

// InsertHandoffReviewPass records a pass and its pinned pull requests. A
// discarded pass of the same number is replaced, the new one's Attempt one
// past it; a pass of that number in any other state is a conflict.
func (s *Store) InsertHandoffReviewPass(ctx context.Context, p HandoffReviewPass, prs []HandoffReviewPR) error {
	return s.reviewWrite(ctx, p.TaskID, true, func(tx *sqlx.Tx, l *ReviewLoop) error {
		return insertReviewLoopPass(ctx, tx, l, p, prs)
	})
}

// StartHandoffReviewPass records a pass with its pinned pull requests
// (as InsertHandoffReviewPass) and its loop, which now names it, in one
// transition: both are written or neither.
func (s *Store) StartHandoffReviewPass(ctx context.Context, p HandoffReviewPass, prs []HandoffReviewPR, loop HandoffReview) error {
	return s.reviewTransition(ctx, p.TaskID, true, func(t *ReviewLoopTx) error {
		if err := insertReviewLoopPass(ctx, t.tx, t.Loop, p, prs); err != nil {
			return err
		}
		applyHandoffReview(t.Loop, loop)
		return nil
	})
}

func insertReviewLoopPass(ctx context.Context, tx *sqlx.Tx, l *ReviewLoop, p HandoffReviewPass, prs []HandoffReviewPR) error {
	var earlier ReviewLoopPass
	err := tx.GetContext(ctx, &earlier, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, l.ID, p.Pass)
	switch {
	case err == nil && earlier.State == ReviewLoopPassDiscarded:
		p.Attempt = earlier.Attempt + 1
		if _, err := tx.ExecContext(ctx, `DELETE FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, l.ID, p.Pass); err != nil {
			return err
		}
	case err == nil:
		return fmt.Errorf("%w: pass %d of %s is %s", ErrConflict, p.Pass, l.HostTask, earlier.State)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	record := ReviewLoopPass{LoopID: l.ID, Pass: p.Pass, StepPath: l.StepPath, State: p.State, Verdict: p.Verdict, Summary: p.Summary,
		Reason: p.Reason, ExecutionID: p.ExecutionID, Checkout: p.Checkout, HeadSHA: p.HeadSHA, Pins: pinsOf(prs),
		PostingStartedAt: p.PostingStartedAt, Attempts: p.PostAttempts, Attempt: p.Attempt, StartedAt: p.StartedAt, EndedAt: p.EndedAt}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_loop_passes (`+reviewLoopPassColumns+`) VALUES (`+reviewLoopPassValues+`)`, record); err != nil {
		return err
	}
	for _, pr := range prs {
		// A layer the loop holds keeps the head its last pass pinned.
		if _, err := tx.ExecContext(ctx, `UPDATE review_loop_prs SET pinned_head = ? WHERE loop_id = ? AND number = ?`, pr.HeadSHA, l.ID, pr.Number); err != nil {
			return err
		}
	}
	return nil
}

// AnyCurrentReviewPassIn says whether an open review loop in loopPhase has
// its current pass in passState.
func (s *Store) AnyCurrentReviewPassIn(ctx context.Context, loopPhase, passState string) (bool, error) {
	var found bool
	err := s.db.GetContext(ctx, &found, `SELECT EXISTS (SELECT 1 FROM review_loops l
		JOIN review_loop_passes p ON p.loop_id = l.id AND p.pass = l.pass
		WHERE l.state = 'open' AND l.phase = ? AND p.state = ?)`, loopPhase, passState)
	return found, err
}

// ReviewApprovedHead says whether Legato's latest review of repo's pull
// request number at head, by any review loop whichever task hosts it (a
// card's, a workflow run's own), approves it: a pass posted APPROVE on it, or
// kept an earlier approval. A later pass at the same head that posted a
// COMMENT undoes it; a pass not posted yet (no event) is no review.
func (s *Store) ReviewApprovedHead(ctx context.Context, repo string, number int, head string) (bool, error) {
	if head == "" {
		return false, nil
	}
	var events []string
	err := s.db.SelectContext(ctx, &events, `SELECT json_extract(j.value, '$.event') FROM review_loop_passes p
		JOIN review_loops l ON l.id = p.loop_id, json_each(p.pins) j
		WHERE COALESCE(NULLIF(json_extract(j.value, '$.repo'), ''), l.repo) = ? COLLATE NOCASE
			AND json_extract(j.value, '$.number') = ? AND json_extract(j.value, '$.head_sha') = ?
			AND COALESCE(json_extract(j.value, '$.event'), '') != ''
		ORDER BY p.started_at DESC, p.rowid DESC, CAST(j.key AS INTEGER) DESC LIMIT 1`, repo, number, head)
	if err != nil || len(events) == 0 {
		return false, err
	}
	return events[0] == "APPROVE" || events[0] == "kept", nil
}

// getReviewLoopPassOf is a pass of a task's loop, ErrNotFound when either is
// missing.
func getReviewLoopPassOf(ctx context.Context, q sqlx.QueryerContext, taskID string, pass int) (*ReviewLoop, *ReviewLoopPass, error) {
	l, err := currentReviewLoop(ctx, q, taskID)
	if err != nil {
		return nil, nil, err
	}
	if l == nil {
		return nil, nil, ErrNotFound
	}
	var p ReviewLoopPass
	err = sqlx.GetContext(ctx, q, &p, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, l.ID, pass)
	if errors.Is(err, sql.ErrNoRows) {
		return l, nil, ErrNotFound
	}
	if err != nil {
		return l, nil, err
	}
	return l, &p, nil
}

// GetHandoffReviewPass returns one pass.
func (s *Store) GetHandoffReviewPass(ctx context.Context, taskID string, pass int) (*HandoffReviewPass, error) {
	l, p, err := getReviewLoopPassOf(ctx, s.db, taskID, pass)
	if err != nil {
		return nil, err
	}
	out := passOf(l, *p)
	return &out, nil
}

// ListHandoffReviewPasses returns a task's loop's passes, first first.
func (s *Store) ListHandoffReviewPasses(ctx context.Context, taskID string) ([]HandoffReviewPass, error) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil || l == nil {
		return nil, err
	}
	passes, err := s.ListReviewLoopPasses(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	out := make([]HandoffReviewPass, 0, len(passes))
	for _, p := range passes {
		out = append(out, passOf(l, p))
	}
	return out, nil
}

// UpdateHandoffReviewPass writes a pass's state, verdict, summary, reason,
// execution and end.
func (s *Store) UpdateHandoffReviewPass(ctx context.Context, p HandoffReviewPass) error {
	return s.reviewWrite(ctx, p.TaskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		return updateReviewLoopPass(ctx, tx, l.ID, p)
	})
}

// UpdateHandoffReviewPassWithLoop writes a pass (as UpdateHandoffReviewPass)
// and its loop in one transition: both are written or neither.
func (s *Store) UpdateHandoffReviewPassWithLoop(ctx context.Context, p HandoffReviewPass, loop HandoffReview) error {
	return s.reviewTransition(ctx, p.TaskID, false, func(t *ReviewLoopTx) error {
		if err := updateReviewLoopPass(ctx, t.tx, t.Loop.ID, p); err != nil {
			return err
		}
		applyHandoffReview(t.Loop, loop)
		return nil
	})
}

func updateReviewLoopPass(ctx context.Context, tx *sqlx.Tx, loopID string, p HandoffReviewPass) error {
	_, err := tx.ExecContext(ctx, `UPDATE review_loop_passes SET state = ?, verdict = ?, summary = ?, reason = ?,
		execution_id = ?, ended_at = ? WHERE loop_id = ? AND pass = ?`,
		p.State, p.Verdict, p.Summary, p.Reason, p.ExecutionID, p.EndedAt, loopID, p.Pass)
	return err
}

// SetHandoffReviewPassPosting writes how a pass's post stands: when it was
// first tried and how many tries ended with GitHub's answer unknown. Both
// are written only here.
func (s *Store) SetHandoffReviewPassPosting(ctx context.Context, taskID string, pass int, startedAt string, attempts int) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		_, err := tx.ExecContext(ctx, `UPDATE review_loop_passes SET posting_started_at = ?, attempts = ? WHERE loop_id = ? AND pass = ?`,
			startedAt, attempts, l.ID, pass)
		return err
	})
}

// HandoffReviewRuling is a pass's ruling on an earlier finding.
type HandoffReviewRuling struct {
	Finding string
	Status  string
	Ruling  string
}

// SubmitHandoffReviewPass records a reviewer's result in one transaction:
// the pass's findings (any recorded for it before dropped), its rulings on
// earlier findings, and the pass itself (as UpdateHandoffReviewPass).
func (s *Store) SubmitHandoffReviewPass(ctx context.Context, p HandoffReviewPass, findings []HandoffReviewFinding, rulings []HandoffReviewRuling) error {
	return s.reviewWrite(ctx, p.TaskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		if err := replaceReviewLoopFindings(ctx, tx, l.ID, p.Pass, findings); err != nil {
			return err
		}
		for _, r := range rulings {
			if _, err := tx.ExecContext(ctx, `UPDATE review_loop_findings SET status = ?, ruled_pass = ?, ruling = ? WHERE loop_id = ? AND id = ?`,
				r.Status, p.Pass, r.Ruling, l.ID, r.Finding); err != nil {
				return err
			}
		}
		return updateReviewLoopPass(ctx, tx, l.ID, p)
	})
}

// FinishHandoffReviewPass records a posted pass in one transition: the
// items it raises for the author (as UpsertHandoffReviewItem), the pass (as
// UpdateHandoffReviewPass) and its loop, moved on. All are written or none.
func (s *Store) FinishHandoffReviewPass(ctx context.Context, p HandoffReviewPass, items []HandoffReviewItem, loop HandoffReview) error {
	return s.reviewTransition(ctx, p.TaskID, false, func(t *ReviewLoopTx) error {
		for _, item := range items {
			if err := upsertReviewLoopItem(ctx, t.tx, loopItemOf(t.Loop.ID, item)); err != nil {
				return err
			}
		}
		if err := updateReviewLoopPass(ctx, t.tx, t.Loop.ID, p); err != nil {
			return err
		}
		applyHandoffReview(t.Loop, loop)
		return nil
	})
}

// DiscardHandoffReviewPass sets a pass's result aside and writes its loop in
// one transition: the pass's state, reason and end, and none of its
// findings or rulings stand (its findings go, and the earlier findings it
// ruled on are open again with no ruling, as the next attempt at the pass
// finds them). Either all of it is written or none.
func (s *Store) DiscardHandoffReviewPass(ctx context.Context, p HandoffReviewPass, loop HandoffReview) error {
	return s.reviewTransition(ctx, p.TaskID, false, func(t *ReviewLoopTx) error {
		if err := discardReviewLoopPass(ctx, t.tx, t.Loop.ID, p.Pass, p.Reason, p.EndedAt); err != nil {
			return err
		}
		applyHandoffReview(t.Loop, loop)
		return nil
	})
}

func discardReviewLoopPass(ctx context.Context, tx *sqlx.Tx, loopID string, pass int, reason, at string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE review_loop_passes SET state = 'discarded', reason = ?, ended_at = ? WHERE loop_id = ? AND pass = ?`,
		reason, at, loopID, pass); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_loop_findings WHERE loop_id = ? AND pass = ?`, loopID, pass); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE review_loop_findings SET status = 'open', ruled_pass = 0, ruling = '' WHERE loop_id = ? AND ruled_pass = ?`, loopID, pass)
	return err
}

// HoldHandoffReviewForRetry keeps a review loop whose workflow step failed
// for a Retry to resume, in one transition. A pass in flight (reviewing or
// submitted) is discarded with passReason, as a moved head discards one,
// and the loop's pass count steps back over a discarded current pass, so
// the next pass pinned takes the number after the last that counted. A
// loop that held or waited for a reviewer, or that a cancel ended, is ready
// for that pass; one ended after a pass that asked for changes waits for
// its author again. A loop in any other phase (the author's turn, clean,
// stuck) keeps it, and every pass, finding, ruling and item stays. It says
// whether the loop changed; a task with no open loop is false.
func (s *Store) HoldHandoffReviewForRetry(ctx context.Context, taskID, passReason, at string) (bool, error) {
	changed := false
	err := s.reviewTransition(ctx, taskID, false, func(t *ReviewLoopTx) error {
		changed = false
		loop := t.Loop
		before := *loop
		var inFlight []int
		if err := t.tx.SelectContext(ctx, &inFlight, `SELECT pass FROM review_loop_passes WHERE loop_id = ? AND state IN `+reviewLoopInFlight+` ORDER BY pass`, loop.ID); err != nil {
			return err
		}
		for _, pass := range inFlight {
			if err := discardReviewLoopPass(ctx, t.tx, loop.ID, pass, passReason, at); err != nil {
				return err
			}
		}
		var current ReviewLoopPass
		err := t.tx.GetContext(ctx, &current, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, loop.ID, loop.Pass)
		if err == nil && current.State == ReviewLoopPassDiscarded && loop.Pass > 0 {
			loop.Pass--
			err = t.tx.GetContext(ctx, &current, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, loop.ID, loop.Pass)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		lastAskedForChanges := err == nil && current.State == ReviewLoopPassPosted && current.Verdict == "changes"
		switch loop.Phase {
		case "ready", "waiting_for_reviewer", "reviewing":
			loop.Phase = "ready"
		case "ended":
			loop.Phase = "ready"
			if lastAskedForChanges {
				loop.Phase = "waiting_for_author"
			}
		}
		if loop.Phase != before.Phase {
			loop.Reason, loop.QueuedAt, loop.Mismatches, loop.NudgedAt, loop.AuthorBusySinceNudge = "", "", 0, "", false
		}
		if *loop == before && len(inFlight) == 0 {
			return errUnchanged
		}
		changed = true
		loop.UpdatedAt = at
		return nil
	})
	if errors.Is(err, errUnchanged) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrReviewLoopClosed) {
		return false, nil
	}
	return changed, err
}

// errUnchanged rolls back a transition that has nothing to write.
var errUnchanged = errors.New("unchanged")

// ListHandoffReviewPRs returns a pass's pull requests in stack order: what
// it pinned.
func (s *Store) ListHandoffReviewPRs(ctx context.Context, taskID string, pass int) ([]HandoffReviewPR, error) {
	l, p, err := getReviewLoopPassOf(ctx, s.db, taskID, pass)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []HandoffReviewPR
	for _, pin := range p.Pins {
		repo := pin.Repo
		if repo == "" {
			repo = l.Repo
		}
		out = append(out, HandoffReviewPR{TaskID: l.HostTask, Pass: p.Pass, Repo: repo, Number: pin.Number, Branch: pin.Branch,
			Base: pin.Base, Title: pin.Title, Body: pin.Body, URL: pin.URL, HeadSHA: pin.HeadSHA, ReviewID: pin.ReviewID,
			ReviewURL: pin.ReviewURL, Author: pin.Author, Event: pin.Event, LocalBranch: pin.LocalBranch, HeadRepo: pin.HeadRepo})
	}
	return out, nil
}

// SetHandoffReviewPRPosted records the review posted on a pass's pull
// request: on its pin, and on the layer when the loop holds one.
func (s *Store) SetHandoffReviewPRPosted(ctx context.Context, taskID string, pass, number int, reviewID int64, url, event string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		var p ReviewLoopPass
		err := tx.GetContext(ctx, &p, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, l.ID, pass)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		for i := range p.Pins {
			if p.Pins[i].Number == number {
				p.Pins[i].ReviewID, p.Pins[i].ReviewURL, p.Pins[i].Event = reviewID, url, event
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE review_loop_passes SET pins = ? WHERE loop_id = ? AND pass = ?`, p.Pins, l.ID, pass); err != nil {
			return err
		}
		if reviewID == 0 && url == "" {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE review_loop_prs SET review_id = ?, review_url = ? WHERE loop_id = ? AND number = ?`, reviewID, url, l.ID, number)
		return err
	})
}

func findingOf(l *ReviewLoop, f ReviewLoopFinding) HandoffReviewFinding {
	return HandoffReviewFinding{ID: f.ID, TaskID: l.HostTask, Pass: f.Pass, PR: f.PR, Severity: f.Severity, Title: f.Title, Body: f.Body,
		Path: f.Path, Line: f.Line, StartLine: f.StartLine, Side: f.Side, CommentID: f.CommentID, Status: f.Status,
		RuledPass: f.RuledPass, Ruling: f.Ruling}
}

// ReplaceHandoffReviewFindings sets a pass's findings, dropping any recorded for
// it before (a submission refused, then made again).
func (s *Store) ReplaceHandoffReviewFindings(ctx context.Context, taskID string, pass int, findings []HandoffReviewFinding) error {
	return s.reviewWrite(ctx, taskID, true, func(tx *sqlx.Tx, l *ReviewLoop) error {
		return replaceReviewLoopFindings(ctx, tx, l.ID, pass, findings)
	})
}

func replaceReviewLoopFindings(ctx context.Context, tx *sqlx.Tx, loopID string, pass int, findings []HandoffReviewFinding) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_loop_findings WHERE loop_id = ? AND pass = ?`, loopID, pass); err != nil {
		return err
	}
	for _, f := range findings {
		record := ReviewLoopFinding{LoopID: loopID, ID: f.ID, Pass: f.Pass, PR: f.PR, Severity: f.Severity, Title: f.Title, Body: f.Body,
			Path: f.Path, Line: f.Line, StartLine: f.StartLine, Side: f.Side, CommentID: f.CommentID, Status: f.Status,
			RuledPass: f.RuledPass, Ruling: f.Ruling}
		if record.Status == "" {
			record.Status = "open"
		}
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_loop_findings (`+reviewLoopFindingColumns+`)
			VALUES (`+reviewLoopFindingValues+`)`, record); err != nil {
			return err
		}
	}
	return nil
}

// ListHandoffReviewFindings returns a task's loop's findings, oldest pass
// first.
func (s *Store) ListHandoffReviewFindings(ctx context.Context, taskID string) ([]HandoffReviewFinding, error) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil || l == nil {
		return nil, err
	}
	findings, err := s.ListReviewLoopFindings(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	var out []HandoffReviewFinding
	for _, f := range findings {
		out = append(out, findingOf(l, f))
	}
	return out, nil
}

// SetHandoffReviewFindingComment records the inline comment that opened a
// finding's thread.
func (s *Store) SetHandoffReviewFindingComment(ctx context.Context, taskID, id string, commentID int64) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		_, err := tx.ExecContext(ctx, `UPDATE review_loop_findings SET comment_id = ? WHERE loop_id = ? AND id = ?`, commentID, l.ID, id)
		return err
	})
}

// RuleHandoffReviewFinding records a later pass's ruling on a finding.
func (s *Store) RuleHandoffReviewFinding(ctx context.Context, taskID, id, status string, pass int, ruling string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		_, err := tx.ExecContext(ctx, `UPDATE review_loop_findings SET status = ?, ruled_pass = ?, ruling = ? WHERE loop_id = ? AND id = ?`,
			status, pass, ruling, l.ID, id)
		return err
	})
}

func loopItemOf(loopID string, item HandoffReviewItem) ReviewLoopItem {
	return ReviewLoopItem{LoopID: loopID, ID: item.ID, Kind: item.Kind, Pass: item.Pass, Repo: item.Repo, PR: item.PR, Path: item.Path,
		Line: item.Line, URL: item.URL, Version: item.Version, Outdated: item.Outdated, Author: item.Author, Title: item.Title,
		Body: item.Body, ThreadCommentID: item.ThreadCommentID, SourceID: item.SourceID, Answer: item.Answer,
		AnswerNote: item.AnswerNote, AnsweredAt: item.AnsweredAt, AnswerMarker: item.AnswerMarker, Posted: item.Posted,
		Closed: item.Closed, Delivered: item.Delivered, CreatedAt: item.CreatedAt}
}

func itemOf(l *ReviewLoop, item ReviewLoopItem) HandoffReviewItem {
	return HandoffReviewItem{ID: item.ID, TaskID: l.HostTask, Kind: item.Kind, Pass: item.Pass, Repo: item.Repo, PR: item.PR,
		Path: item.Path, Line: item.Line, URL: item.URL, Version: item.Version, Outdated: item.Outdated, Author: item.Author,
		Title: item.Title, Body: item.Body, ThreadCommentID: item.ThreadCommentID, SourceID: item.SourceID, Answer: item.Answer,
		AnswerNote: item.AnswerNote, AnsweredAt: item.AnsweredAt, Posted: item.Posted, AnswerMarker: item.AnswerMarker,
		Closed: item.Closed, CreatedAt: item.CreatedAt, Delivered: item.Delivered}
}

// UpsertHandoffReviewItem writes an item: a new one, or one raised again
// (its answer cleared, its version moved on). An item is its loop's: the
// same ID in another loop (the same pull request number and check name in
// another repository) is another item.
func (s *Store) UpsertHandoffReviewItem(ctx context.Context, item HandoffReviewItem) error {
	return s.reviewWrite(ctx, item.TaskID, true, func(tx *sqlx.Tx, l *ReviewLoop) error {
		record := loopItemOf(l.ID, item)
		if record.AnswerMarker == "" {
			// An item written whole keeps the marker its answer had.
			var marker string
			if err := tx.GetContext(ctx, &marker, `SELECT answer_marker FROM review_loop_items WHERE loop_id = ? AND id = ?`, l.ID, item.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			record.AnswerMarker = marker
		}
		if record.CreatedAt == "" {
			record.CreatedAt = reviewLoopNow()
		}
		return upsertReviewLoopItem(ctx, tx, record)
	})
}

// GetHandoffReviewItem returns one item.
func (s *Store) GetHandoffReviewItem(ctx context.Context, taskID, id string) (*HandoffReviewItem, error) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, ErrNotFound
	}
	item, err := s.GetReviewLoopItem(ctx, l.ID, id)
	if err != nil {
		return nil, err
	}
	out := itemOf(l, *item)
	return &out, nil
}

// ListHandoffReviewItems returns a task's loop's items, oldest first.
func (s *Store) ListHandoffReviewItems(ctx context.Context, taskID string) ([]HandoffReviewItem, error) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil || l == nil {
		return nil, err
	}
	items, err := s.ListReviewLoopItems(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	var out []HandoffReviewItem
	for _, item := range items {
		out = append(out, itemOf(l, item))
	}
	return out, nil
}

// AnswerHandoffReviewItem records the author's answer to an item, with a
// new answer marker.
func (s *Store) AnswerHandoffReviewItem(ctx context.Context, taskID, id, answer, note, at string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		result, err := tx.ExecContext(ctx, `UPDATE review_loop_items SET answer = ?, answer_note = ?, answered_at = ?, posted = 0,
			answer_marker = lower(hex(randomblob(8)))
			WHERE loop_id = ? AND id = ? AND closed = 0`, answer, note, at, l.ID, id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// MarkHandoffReviewItemPosted records that an item's answer is on GitHub.
func (s *Store) MarkHandoffReviewItemPosted(ctx context.Context, taskID, id string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		_, err := tx.ExecContext(ctx, `UPDATE review_loop_items SET posted = 1 WHERE loop_id = ? AND id = ?`, l.ID, id)
		return err
	})
}

// MarkHandoffReviewItemsDelivered records that the author was told of
// items.
func (s *Store) MarkHandoffReviewItemsDelivered(ctx context.Context, taskID string, ids []string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE review_loop_items SET delivered = 1 WHERE loop_id = ? AND id = ?`, l.ID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// HandoffReviewPollPage returns a list page polled before, false when none.
func (s *Store) HandoffReviewPollPage(ctx context.Context, taskID string, pr int, list string, page int) (etag, body string, ok bool) {
	l, err := currentReviewLoop(ctx, s.db, taskID)
	if err != nil || l == nil {
		return "", "", false
	}
	return s.ReviewLoopPollPage(ctx, l.ID, pr, list, page)
}

// SaveHandoffReviewPollPage keeps a list page and its ETag.
func (s *Store) SaveHandoffReviewPollPage(ctx context.Context, taskID string, pr int, list string, page int, etag, body string) error {
	return s.reviewWrite(ctx, taskID, false, func(tx *sqlx.Tx, l *ReviewLoop) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO review_loop_poll_pages (loop_id, pr, list, page, etag, body) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(loop_id, pr, list, page) DO UPDATE SET etag = excluded.etag, body = excluded.body`, l.ID, pr, list, page, etag, body)
		return err
	})
}

// withTxDB runs statements in one transaction.
func withTxDB(ctx context.Context, s *Store, f func(exec func(string, ...any) error) error) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(func(q string, args ...any) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRunReviewLoopsAuthoredBy returns the workflow runs' loops whose agent
// author is authorTask's, newest first.
func (s *Store) ListRunReviewLoopsAuthoredBy(ctx context.Context, authorTask string) ([]ReviewLoop, error) {
	var out []ReviewLoop
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loops WHERE owner_kind = 'run' AND agent_author = 1 AND author_task = ?
		ORDER BY state = 'open' DESC, created_at DESC, rowid DESC`, authorTask)
	return out, err
}

// CloseReviewLoopsOfClosedHandoffs closes the open loops owned by a handoff
// that has closed, or is gone, here (CloseReviewLoop: claims released, a
// pass in flight discarded, the phase kept), outcome ended, as the
// conversion closes them. It says how many it closed.
func (s *Store) CloseReviewLoopsOfClosedHandoffs(ctx context.Context, at string) (int, error) {
	var ids []string
	if err := s.db.SelectContext(ctx, &ids, `SELECT l.id FROM review_loops l
		WHERE l.state = 'open' AND l.owner_kind = 'handoff' AND l.handoff_id != ''
		AND NOT EXISTS (SELECT 1 FROM handoffs h WHERE h.handoff_id = l.handoff_id AND h.task_id = l.host_task
			AND (l.main_instance_id = '' OR h.main_instance_id = l.main_instance_id) AND h.closed_at IS NULL)
		ORDER BY l.created_at, l.id`); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		_, closed, err := s.CloseReviewLoop(ctx, id, "ended", at)
		if err != nil {
			return n, err
		}
		if closed {
			n++
		}
	}
	return n, nil
}
