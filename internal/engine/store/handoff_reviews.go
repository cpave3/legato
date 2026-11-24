package store

import (
	"context"
	"database/sql"
	"errors"
)

// HandoffReview is one card's review loop on a worker.
type HandoffReview struct {
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
	TaskID      string `db:"task_id"`
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

// HandoffReviewPR is a pull request of a pass, pinned at its head.
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

// GetHandoffReview returns a card's review loop, nil, nil when it has none.
func (s *Store) GetHandoffReview(ctx context.Context, taskID string) (*HandoffReview, error) {
	var r HandoffReview
	err := s.db.GetContext(ctx, &r, `SELECT * FROM handoff_reviews WHERE task_id = ?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// ListHandoffReviews returns every review loop, oldest queued first.
func (s *Store) ListHandoffReviews(ctx context.Context) ([]HandoffReview, error) {
	var out []HandoffReview
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_reviews ORDER BY queued_at, task_id`)
	return out, err
}

// SaveHandoffReview writes a card's review loop.
func (s *Store) SaveHandoffReview(ctx context.Context, r HandoffReview) error {
	_, err := s.db.NamedExecContext(ctx, saveHandoffReviewSQL, r)
	return err
}

const saveHandoffReviewSQL = `
		INSERT INTO handoff_reviews (task_id, settings, phase, pass, mismatches, nudged_at, author_busy_since_nudge, reason, queued_at, updated_at, delivered_pass, polled_at, last_new_at, skipped_comments, checks_polled_at, checks_heads, checks_pending)
		VALUES (:task_id, :settings, :phase, :pass, :mismatches, :nudged_at, :author_busy_since_nudge, :reason, :queued_at, :updated_at, :delivered_pass, :polled_at, :last_new_at, :skipped_comments, :checks_polled_at, :checks_heads, :checks_pending)
		ON CONFLICT(task_id) DO UPDATE SET settings = excluded.settings, phase = excluded.phase, pass = excluded.pass,
			mismatches = excluded.mismatches, nudged_at = excluded.nudged_at,
			author_busy_since_nudge = excluded.author_busy_since_nudge, reason = excluded.reason,
			queued_at = excluded.queued_at, updated_at = excluded.updated_at, delivered_pass = excluded.delivered_pass,
			polled_at = excluded.polled_at, last_new_at = excluded.last_new_at, skipped_comments = excluded.skipped_comments,
			checks_polled_at = excluded.checks_polled_at, checks_heads = excluded.checks_heads, checks_pending = excluded.checks_pending`

// SaveHandoffReviewWithMetadata saves a loop and, in the same transaction,
// value at path in its task's metadata (SetTaskMetadata): the record that
// names who opened the loop is never there without the loop.
func (s *Store) SaveHandoffReviewWithMetadata(ctx context.Context, r HandoffReview, path []string, value any) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setTaskMetadata(ctx, tx, r.TaskID, path, value); err != nil {
		return err
	}
	if _, err := tx.NamedExecContext(ctx, saveHandoffReviewSQL, r); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteHandoffReview forgets a card's review loop and its passes.
func (s *Store) DeleteHandoffReview(ctx context.Context, taskID string) error {
	return withTxDB(ctx, s, func(exec func(string, ...any) error) error {
		for _, table := range []string{"handoff_review_poll_pages", "handoff_review_items", "handoff_review_findings", "handoff_review_prs", "handoff_review_passes", "handoff_reviews"} {
			if err := exec(`DELETE FROM `+table+` WHERE task_id = ?`, taskID); err != nil {
				return err
			}
		}
		return nil
	})
}

// EndHandoffReview ends a card's review loop that a pass runs on, waits to
// run, or is about to start (phase ready, waiting_for_reviewer or
// reviewing), in one transaction: the loop's phase becomes ended with
// reason, and a pass in flight (reviewing or submitted) is discarded with
// passReason. Such a loop holds no reviewer slot after. A loop in any other
// phase (the author's turn, clean, stuck) keeps it. It says whether the
// loop ended.
func (s *Store) EndHandoffReview(ctx context.Context, taskID, reason, passReason, at string) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE handoff_review_passes SET state = 'discarded', reason = ?, ended_at = ?
		WHERE task_id = ? AND state IN ('reviewing', 'submitted')`, passReason, at, taskID); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE handoff_reviews SET phase = 'ended', reason = ?, queued_at = '', updated_at = ?
		WHERE task_id = ? AND phase IN ('ready', 'waiting_for_reviewer', 'reviewing')`, reason, at, taskID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, tx.Commit()
}

// InsertHandoffReviewPass records a pass and its pinned pull requests. A
// discarded pass of the same number is replaced, the new one's Attempt one
// past it; a pass of that number in any other state is a conflict.
func (s *Store) InsertHandoffReviewPass(ctx context.Context, p HandoffReviewPass, prs []HandoffReviewPR) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var earlier HandoffReviewPass
	err = tx.GetContext(ctx, &earlier, `SELECT * FROM handoff_review_passes WHERE task_id = ? AND pass = ?`, p.TaskID, p.Pass)
	switch {
	case err == nil && earlier.State == "discarded":
		p.Attempt = earlier.Attempt + 1
		if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_review_prs WHERE task_id = ? AND pass = ?`, p.TaskID, p.Pass); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_review_passes WHERE task_id = ? AND pass = ?`, p.TaskID, p.Pass); err != nil {
			return err
		}
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, reason, execution_id, checkout, head_sha, started_at, ended_at, attempt)
		VALUES (:task_id, :pass, :state, :verdict, :summary, :reason, :execution_id, :checkout, :head_sha, :started_at, :ended_at, :attempt)`, p); err != nil {
		return err
	}
	for _, pr := range prs {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, body, url, head_sha, review_id, review_url, author, local_branch, head_repo)
			VALUES (:task_id, :pass, :repo, :number, :branch, :base, :title, :body, :url, :head_sha, :review_id, :review_url, :author, :local_branch, :head_repo)`, pr); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AnyCurrentReviewPassIn says whether a review loop in loopPhase has its
// current pass in passState.
func (s *Store) AnyCurrentReviewPassIn(ctx context.Context, loopPhase, passState string) (bool, error) {
	var found bool
	err := s.db.GetContext(ctx, &found, `SELECT EXISTS (SELECT 1 FROM handoff_reviews r
		JOIN handoff_review_passes p ON p.task_id = r.task_id AND p.pass = r.pass
		WHERE r.phase = ? AND p.state = ?)`, loopPhase, passState)
	return found, err
}

// ReviewApprovedHead says whether Legato's latest review of repo's pull
// request number at head, by any review loop whichever task it is kept on (a
// card's, a workflow run's own), approves it: a pass posted APPROVE on it, or
// kept an earlier approval. A later pass at the same head that posted a
// COMMENT undoes it; a pass not posted yet (no event) is no review.
func (s *Store) ReviewApprovedHead(ctx context.Context, repo string, number int, head string) (bool, error) {
	if head == "" {
		return false, nil
	}
	var events []string
	err := s.db.SelectContext(ctx, &events, `SELECT r.event FROM handoff_review_prs r
		JOIN handoff_review_passes p ON p.task_id = r.task_id AND p.pass = r.pass
		WHERE r.repo = ? COLLATE NOCASE AND r.number = ? AND r.head_sha = ? AND r.event != ''
		ORDER BY p.started_at DESC, r.rowid DESC LIMIT 1`, repo, number, head)
	if err != nil || len(events) == 0 {
		return false, err
	}
	return events[0] == "APPROVE" || events[0] == "kept", nil
}

// GetHandoffReviewPass returns one pass.
func (s *Store) GetHandoffReviewPass(ctx context.Context, taskID string, pass int) (*HandoffReviewPass, error) {
	var p HandoffReviewPass
	err := s.db.GetContext(ctx, &p, `SELECT * FROM handoff_review_passes WHERE task_id = ? AND pass = ?`, taskID, pass)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ListHandoffReviewPasses returns a card's passes, first first.
func (s *Store) ListHandoffReviewPasses(ctx context.Context, taskID string) ([]HandoffReviewPass, error) {
	var out []HandoffReviewPass
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_review_passes WHERE task_id = ? ORDER BY pass`, taskID)
	return out, err
}

// UpdateHandoffReviewPass writes a pass's state, verdict, summary, reason,
// execution and end.
func (s *Store) UpdateHandoffReviewPass(ctx context.Context, p HandoffReviewPass) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE handoff_review_passes SET state = :state, verdict = :verdict, summary = :summary, reason = :reason,
			execution_id = :execution_id, ended_at = :ended_at
		WHERE task_id = :task_id AND pass = :pass`, p)
	return err
}

// SetHandoffReviewPassPosting writes how a pass's post stands: when it was
// first tried and how many tries ended with GitHub's answer unknown. Both
// are written only here.
func (s *Store) SetHandoffReviewPassPosting(ctx context.Context, taskID string, pass int, startedAt string, attempts int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_passes SET posting_started_at = ?, post_attempts = ? WHERE task_id = ? AND pass = ?`,
		startedAt, attempts, taskID, pass)
	return err
}

// DiscardHandoffReviewPass sets a pass's result aside in one transaction:
// its state, reason and end, and none of its findings or rulings stand.
// Its findings go, and the earlier findings it ruled on are open again with
// no ruling, as the next attempt at the pass finds them.
func (s *Store) DiscardHandoffReviewPass(ctx context.Context, p HandoffReviewPass) error {
	return withTxDB(ctx, s, func(exec func(string, ...any) error) error {
		if err := exec(`UPDATE handoff_review_passes SET state = 'discarded', reason = ?, ended_at = ? WHERE task_id = ? AND pass = ?`,
			p.Reason, p.EndedAt, p.TaskID, p.Pass); err != nil {
			return err
		}
		if err := exec(`DELETE FROM handoff_review_findings WHERE task_id = ? AND pass = ?`, p.TaskID, p.Pass); err != nil {
			return err
		}
		return exec(`UPDATE handoff_review_findings SET status = 'open', ruled_pass = 0, ruling = '' WHERE task_id = ? AND ruled_pass = ?`, p.TaskID, p.Pass)
	})
}

// ListHandoffReviewPRs returns a pass's pull requests in stack order.
func (s *Store) ListHandoffReviewPRs(ctx context.Context, taskID string, pass int) ([]HandoffReviewPR, error) {
	var out []HandoffReviewPR
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_review_prs WHERE task_id = ? AND pass = ? ORDER BY rowid`, taskID, pass)
	return out, err
}

// SetHandoffReviewPRPosted records the review posted on a pass's pull request.
func (s *Store) SetHandoffReviewPRPosted(ctx context.Context, taskID string, pass, number int, reviewID int64, url, event string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_prs SET review_id = ?, review_url = ?, event = ? WHERE task_id = ? AND pass = ? AND number = ?`,
		reviewID, url, event, taskID, pass, number)
	return err
}

// ReplaceHandoffReviewFindings sets a pass's findings, dropping any recorded for
// it before (a submission refused, then made again).
func (s *Store) ReplaceHandoffReviewFindings(ctx context.Context, taskID string, pass int, findings []HandoffReviewFinding) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_review_findings WHERE task_id = ? AND pass = ?`, taskID, pass); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO handoff_review_findings (id, task_id, pass, pr, severity, title, body, path, line, start_line, side, comment_id, status, ruled_pass, ruling)
			VALUES (:id, :task_id, :pass, :pr, :severity, :title, :body, :path, :line, :start_line, :side, :comment_id, :status, :ruled_pass, :ruling)`, f); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListHandoffReviewFindings returns a card's findings, oldest pass first.
func (s *Store) ListHandoffReviewFindings(ctx context.Context, taskID string) ([]HandoffReviewFinding, error) {
	var out []HandoffReviewFinding
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_review_findings WHERE task_id = ? ORDER BY pass, rowid`, taskID)
	return out, err
}

// SetHandoffReviewFindingComment records the inline comment that opened a
// finding's thread.
func (s *Store) SetHandoffReviewFindingComment(ctx context.Context, taskID, id string, commentID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_findings SET comment_id = ? WHERE task_id = ? AND id = ?`, commentID, taskID, id)
	return err
}

// RuleHandoffReviewFinding records a later pass's ruling on a finding.
func (s *Store) RuleHandoffReviewFinding(ctx context.Context, taskID, id, status string, pass int, ruling string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_findings SET status = ?, ruled_pass = ?, ruling = ? WHERE task_id = ? AND id = ?`,
		status, pass, ruling, taskID, id)
	return err
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

// HandoffReviewItem is one thing the author of a reviewed handoff must
// handle (a finding, a counted comment, a failing check), and its answer.
// It is keyed by its loop (TaskID) and ID.
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

// UpsertHandoffReviewItem writes an item: a new one, or one raised again
// (its answer cleared, its version moved on). An item is its loop's: the
// same ID in another loop (the same pull request number and check name in
// another repository) is another item.
func (s *Store) UpsertHandoffReviewItem(ctx context.Context, item HandoffReviewItem) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO handoff_review_items (id, task_id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body,
			thread_comment_id, source_id, answer, answer_note, answered_at, posted, closed, created_at, delivered)
		VALUES (:id, :task_id, :kind, :pass, :repo, :pr, :path, :line, :url, :version, :outdated, :author, :title, :body,
			:thread_comment_id, :source_id, :answer, :answer_note, :answered_at, :posted, :closed, :created_at, :delivered)
		ON CONFLICT(task_id, id) DO UPDATE SET kind = excluded.kind, pass = excluded.pass, path = excluded.path, line = excluded.line,
			url = excluded.url, version = excluded.version, outdated = excluded.outdated, author = excluded.author,
			title = excluded.title, body = excluded.body, thread_comment_id = excluded.thread_comment_id,
			answer = excluded.answer, answer_note = excluded.answer_note, answered_at = excluded.answered_at,
			posted = excluded.posted, closed = excluded.closed, delivered = excluded.delivered`, item)
	return err
}

// GetHandoffReviewItem returns one item.
func (s *Store) GetHandoffReviewItem(ctx context.Context, taskID, id string) (*HandoffReviewItem, error) {
	var item HandoffReviewItem
	err := s.db.GetContext(ctx, &item, `SELECT * FROM handoff_review_items WHERE task_id = ? AND id = ?`, taskID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

// ListHandoffReviewItems returns a card's items, oldest first.
func (s *Store) ListHandoffReviewItems(ctx context.Context, taskID string) ([]HandoffReviewItem, error) {
	var out []HandoffReviewItem
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_review_items WHERE task_id = ? ORDER BY created_at, rowid`, taskID)
	return out, err
}

// AnswerHandoffReviewItem records the author's answer to an item, with a
// new answer marker.
func (s *Store) AnswerHandoffReviewItem(ctx context.Context, taskID, id, answer, note, at string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE handoff_review_items SET answer = ?, answer_note = ?, answered_at = ?, posted = 0,
		answer_marker = lower(hex(randomblob(8)))
		WHERE task_id = ? AND id = ? AND closed = 0`, answer, note, at, taskID, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkHandoffReviewItemPosted records that an item's answer is on GitHub.
func (s *Store) MarkHandoffReviewItemPosted(ctx context.Context, taskID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_items SET posted = 1 WHERE task_id = ? AND id = ?`, taskID, id)
	return err
}

// MarkHandoffReviewItemsDelivered records that the author was told of
// items.
func (s *Store) MarkHandoffReviewItemsDelivered(ctx context.Context, taskID string, ids []string) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE handoff_review_items SET delivered = 1 WHERE task_id = ? AND id = ?`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

// HandoffReviewPollPage returns a list page polled before, false when none.
func (s *Store) HandoffReviewPollPage(ctx context.Context, taskID string, pr int, list string, page int) (etag, body string, ok bool) {
	var row struct {
		ETag string `db:"etag"`
		Body string `db:"body"`
	}
	if err := s.db.GetContext(ctx, &row, `SELECT etag, body FROM handoff_review_poll_pages WHERE task_id = ? AND pr = ? AND list = ? AND page = ?`, taskID, pr, list, page); err != nil {
		return "", "", false
	}
	return row.ETag, row.Body, true
}

// SaveHandoffReviewPollPage keeps a list page and its ETag.
func (s *Store) SaveHandoffReviewPollPage(ctx context.Context, taskID string, pr int, list string, page int, etag, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO handoff_review_poll_pages (task_id, pr, list, page, etag, body) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, pr, list, page) DO UPDATE SET etag = excluded.etag, body = excluded.body`, taskID, pr, list, page, etag, body)
	return err
}
