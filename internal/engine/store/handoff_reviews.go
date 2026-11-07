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
}

// HandoffReviewFinding is one finding of a pass, with its ruling by a later one.
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
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO handoff_reviews (task_id, settings, phase, pass, mismatches, nudged_at, author_busy_since_nudge, reason, queued_at, updated_at)
		VALUES (:task_id, :settings, :phase, :pass, :mismatches, :nudged_at, :author_busy_since_nudge, :reason, :queued_at, :updated_at)
		ON CONFLICT(task_id) DO UPDATE SET settings = excluded.settings, phase = excluded.phase, pass = excluded.pass,
			mismatches = excluded.mismatches, nudged_at = excluded.nudged_at,
			author_busy_since_nudge = excluded.author_busy_since_nudge, reason = excluded.reason,
			queued_at = excluded.queued_at, updated_at = excluded.updated_at`, r)
	return err
}

// DeleteHandoffReview forgets a card's review loop and its passes.
func (s *Store) DeleteHandoffReview(ctx context.Context, taskID string) error {
	return withTxDB(ctx, s, func(exec func(string, ...any) error) error {
		for _, table := range []string{"handoff_review_findings", "handoff_review_prs", "handoff_review_passes", "handoff_reviews"} {
			if err := exec(`DELETE FROM `+table+` WHERE task_id = ?`, taskID); err != nil {
				return err
			}
		}
		return nil
	})
}

// InsertHandoffReviewPass records a pass and its pinned pull requests.
func (s *Store) InsertHandoffReviewPass(ctx context.Context, p HandoffReviewPass, prs []HandoffReviewPR) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, reason, execution_id, checkout, head_sha, started_at, ended_at)
		VALUES (:task_id, :pass, :state, :verdict, :summary, :reason, :execution_id, :checkout, :head_sha, :started_at, :ended_at)`, p); err != nil {
		return err
	}
	for _, pr := range prs {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, body, url, head_sha, review_id, review_url)
			VALUES (:task_id, :pass, :repo, :number, :branch, :base, :title, :body, :url, :head_sha, :review_id, :review_url)`, pr); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// ListHandoffReviewPRs returns a pass's pull requests in stack order.
func (s *Store) ListHandoffReviewPRs(ctx context.Context, taskID string, pass int) ([]HandoffReviewPR, error) {
	var out []HandoffReviewPR
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM handoff_review_prs WHERE task_id = ? AND pass = ? ORDER BY rowid`, taskID, pass)
	return out, err
}

// SetHandoffReviewPRPosted records the review posted on a pass's pull request.
func (s *Store) SetHandoffReviewPRPosted(ctx context.Context, taskID string, pass, number int, reviewID int64, url string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_prs SET review_id = ?, review_url = ? WHERE task_id = ? AND pass = ? AND number = ?`,
		reviewID, url, taskID, pass, number)
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
func (s *Store) SetHandoffReviewFindingComment(ctx context.Context, id string, commentID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_findings SET comment_id = ? WHERE id = ?`, commentID, id)
	return err
}

// RuleHandoffReviewFinding records a later pass's ruling on a finding.
func (s *Store) RuleHandoffReviewFinding(ctx context.Context, id, status string, pass int, ruling string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE handoff_review_findings SET status = ?, ruled_pass = ?, ruling = ? WHERE id = ?`, status, pass, ruling, id)
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
