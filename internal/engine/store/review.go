package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// EnsureReviewTour returns the tour for a task, creating it in the default
// 'capturing' state when absent.
func (s *Store) EnsureReviewTour(ctx context.Context, taskID string) (*ReviewTour, error) {
	if _, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO review_tours (task_id) VALUES (?)", taskID); err != nil {
		return nil, err
	}
	return s.GetReviewTour(ctx, taskID)
}

// GetReviewTour returns the tour for a task, or ErrNotFound.
func (s *Store) GetReviewTour(ctx context.Context, taskID string) (*ReviewTour, error) {
	var rt ReviewTour
	err := s.db.GetContext(ctx, &rt, "SELECT * FROM review_tours WHERE task_id = ?", taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

// InsertReviewStep adds a step to a tour. Commit steps are deduped per task
// on their SHA; the returned bool reports whether a row was actually inserted.
func (s *Store) InsertReviewStep(ctx context.Context, step ReviewStep) (bool, error) {
	res, err := s.db.NamedExecContext(ctx, `
		INSERT OR IGNORE INTO review_steps (id, task_id, kind, commit_sha, files,
			title, narration, risk, order_hint, seq, subtask_id, dirty_fingerprint)
		VALUES (:id, :task_id, :kind, :commit_sha, :files,
			:title, :narration, :risk, :order_hint, :seq, :subtask_id, :dirty_fingerprint)`, step)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// ListReviewSteps returns a tour's steps in reading order: explicit agent
// order hints first, then commit topology.
func (s *Store) ListReviewSteps(ctx context.Context, taskID string) ([]ReviewStep, error) {
	var steps []ReviewStep
	err := s.db.SelectContext(ctx, &steps, `
		SELECT * FROM review_steps WHERE task_id = ?
		ORDER BY (order_hint IS NULL), order_hint ASC, seq ASC`, taskID)
	return steps, err
}

// GetReviewStep returns a step by exact ID, or ErrNotFound.
func (s *Store) GetReviewStep(ctx context.Context, id string) (*ReviewStep, error) {
	var st ReviewStep
	err := s.db.GetContext(ctx, &st, "SELECT * FROM review_steps WHERE id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &st, err
}

// GetReviewStepByPrefix resolves a step within a task by ID prefix. Returns
// ErrNotFound when nothing matches and an error when the prefix is ambiguous.
func (s *Store) GetReviewStepByPrefix(ctx context.Context, taskID, prefix string) (*ReviewStep, error) {
	var steps []ReviewStep
	err := s.db.SelectContext(ctx, &steps,
		"SELECT * FROM review_steps WHERE task_id = ? AND id LIKE ? || '%' LIMIT 2", taskID, prefix)
	if err != nil {
		return nil, err
	}
	switch len(steps) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &steps[0], nil
	default:
		return nil, fmt.Errorf("step id prefix %q is ambiguous", prefix)
	}
}

// UpdateReviewStep applies mutate to the step row and persists the result.
func (s *Store) UpdateReviewStep(ctx context.Context, id string, mutate func(*ReviewStep)) (*ReviewStep, error) {
	st, err := s.GetReviewStep(ctx, id)
	if err != nil {
		return nil, err
	}
	mutate(st)
	_, err = s.db.NamedExecContext(ctx, `
		UPDATE review_steps SET
			files = :files, title = :title, narration = :narration, risk = :risk,
			order_hint = :order_hint, seq = :seq, subtask_id = :subtask_id,
			dirty_fingerprint = :dirty_fingerprint, reviewed_at = :reviewed_at,
			orphaned_at = :orphaned_at, updated_at = datetime('now')
		WHERE id = :id`, st)
	if err != nil {
		return nil, err
	}
	return s.GetReviewStep(ctx, id)
}

// DeleteReviewStep removes a step (used for the synthetic dirty step when the
// worktree becomes clean; commit steps are orphaned, never deleted).
func (s *Store) DeleteReviewStep(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM review_steps WHERE id = ?", id)
	return err
}

// MarkReviewStepsOrphaned reconciles commit steps against the SHAs currently
// reachable in base..HEAD: steps whose SHA left the range are flagged orphaned
// (kept — annotations and transcript stay attached), and steps whose SHA
// reappears are healed.
func (s *Store) MarkReviewStepsOrphaned(ctx context.Context, taskID string, liveSHAs []string) error {
	query, args := buildInClause(
		"UPDATE review_steps SET orphaned_at = datetime('now'), updated_at = datetime('now') "+
			"WHERE task_id = ? AND kind = 'commit' AND orphaned_at IS NULL AND commit_sha NOT IN (%s)",
		taskID, liveSHAs)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	query, args = buildInClause(
		"UPDATE review_steps SET orphaned_at = NULL, updated_at = datetime('now') "+
			"WHERE task_id = ? AND kind = 'commit' AND orphaned_at IS NOT NULL AND commit_sha IN (%s)",
		taskID, liveSHAs)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// buildInClause expands an IN (%s) placeholder list for taskID + values.
// An empty value list yields a clause that matches nothing (”) so callers
// don't need a special case.
func buildInClause(format, taskID string, values []string) (string, []any) {
	placeholders := "''"
	args := []any{taskID}
	if len(values) > 0 {
		placeholders = "?" + strings.Repeat(",?", len(values)-1)
		for _, v := range values {
			args = append(args, v)
		}
	}
	return fmt.Sprintf(format, placeholders), args
}

// ListReviewTours returns all tours, most recently updated first.
func (s *Store) ListReviewTours(ctx context.Context) ([]ReviewTour, error) {
	var tours []ReviewTour
	err := s.db.SelectContext(ctx, &tours,
		"SELECT * FROM review_tours ORDER BY updated_at DESC, task_id ASC")
	return tours, err
}

// InsertReviewMessage appends a Q&A transcript entry. delivered records
// whether the message actually reached the agent's pane (questions sent while
// the agent session is dead are stored with a NULL delivered_at).
func (s *Store) InsertReviewMessage(ctx context.Context, m ReviewMessage, delivered bool) (*ReviewMessage, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO review_transcript (task_id, step_id, kind, author, body, delivered_at)
		VALUES (?, ?, ?, ?, ?, CASE WHEN ? THEN datetime('now') ELSE NULL END)`,
		m.TaskID, m.StepID, m.Kind, m.Author, m.Body, delivered)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	var out ReviewMessage
	if err := s.db.GetContext(ctx, &out, "SELECT * FROM review_transcript WHERE id = ?", id); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListReviewMessages returns a task's Q&A transcript, oldest first.
func (s *Store) ListReviewMessages(ctx context.Context, taskID string) ([]ReviewMessage, error) {
	var msgs []ReviewMessage
	err := s.db.SelectContext(ctx, &msgs,
		"SELECT * FROM review_transcript WHERE task_id = ? ORDER BY id ASC", taskID)
	return msgs, err
}

// UnreviewedReviewCounts returns, per task, the number of non-orphaned steps
// not yet marked reviewed. Drives the board badge.
func (s *Store) UnreviewedReviewCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, COUNT(*) FROM review_steps
		WHERE reviewed_at IS NULL AND orphaned_at IS NULL
		GROUP BY task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var taskID string
		var n int
		if err := rows.Scan(&taskID, &n); err != nil {
			return nil, err
		}
		counts[taskID] = n
	}
	return counts, rows.Err()
}

// UpdateReviewTour applies mutate to the current tour row and persists the
// result, returning the updated tour.
func (s *Store) UpdateReviewTour(ctx context.Context, taskID string, mutate func(*ReviewTour)) (*ReviewTour, error) {
	rt, err := s.GetReviewTour(ctx, taskID)
	if err != nil {
		return nil, err
	}
	mutate(rt)
	_, err = s.db.NamedExecContext(ctx, `
		UPDATE review_tours SET
			status = :status, summary = :summary, base_sha = :base_sha,
			last_reviewed_sha = :last_reviewed_sha, ready_at = :ready_at,
			updated_at = datetime('now')
		WHERE task_id = :task_id`, rt)
	if err != nil {
		return nil, err
	}
	return s.GetReviewTour(ctx, taskID)
}
