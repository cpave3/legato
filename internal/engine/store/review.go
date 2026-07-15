package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// EnsureReviewTour returns the tour for a task + name, creating it in the
// default 'capturing' state when absent. An empty name selects the default tour.
func (s *Store) EnsureReviewTour(ctx context.Context, taskID, name string) (*ReviewTour, error) {
	id := reviewTourID(taskID, name)
	if _, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO review_tours (id, task_id, name) VALUES (?, ?, ?)", id, taskID, name); err != nil {
		return nil, err
	}
	return s.GetReviewTour(ctx, id)
}

// GetReviewTour returns a tour by its surrogate ID, or ErrNotFound.
func (s *Store) GetReviewTour(ctx context.Context, id string) (*ReviewTour, error) {
	var rt ReviewTour
	err := s.db.GetContext(ctx, &rt, "SELECT * FROM review_tours WHERE id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

// GetDefaultReviewTour returns the tour with an empty name for a task.
func (s *Store) GetDefaultReviewTour(ctx context.Context, taskID string) (*ReviewTour, error) {
	var rt ReviewTour
	err := s.db.GetContext(ctx, &rt, "SELECT * FROM review_tours WHERE task_id = ? AND name = ''", taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

// ListReviewToursByTask returns all tours for a task, most recently updated first.
func (s *Store) ListReviewToursByTask(ctx context.Context, taskID string) ([]ReviewTour, error) {
	var tours []ReviewTour
	err := s.db.SelectContext(ctx, &tours,
		"SELECT * FROM review_tours WHERE task_id = ? ORDER BY updated_at DESC", taskID)
	return tours, err
}

// reviewTourID derives a deterministic surrogate ID from task + name.
func reviewTourID(taskID, name string) string {
	if name == "" {
		return "rt-" + taskID
	}
	return "rt-" + taskID + "-" + name
}

// InsertReviewStep adds a step to a tour. Commit steps are deduped per task
// on their SHA; the returned bool reports whether a row was actually inserted.
func (s *Store) InsertReviewStep(ctx context.Context, step ReviewStep) (bool, error) {
	res, err := s.db.NamedExecContext(ctx, `
		INSERT OR IGNORE INTO review_steps (id, task_id, tour_id, kind, commit_sha, files,
			title, narration, risk, order_hint, seq, subtask_id, dirty_fingerprint)
		VALUES (:id, :task_id, :tour_id, :kind, :commit_sha, :files,
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
func (s *Store) ListReviewSteps(ctx context.Context, tourID string) ([]ReviewStep, error) {
	var steps []ReviewStep
	err := s.db.SelectContext(ctx, &steps, `
		SELECT * FROM review_steps WHERE tour_id = ?
		ORDER BY (order_hint IS NULL), order_hint ASC, seq ASC`, tourID)
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

// InsertReviewChapter creates a chapter step and its hunk memberships atomically.
func (s *Store) InsertReviewChapter(ctx context.Context, step ReviewStep, hunks []ReviewChapterHunk) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO review_steps (id, task_id, tour_id, kind, commit_sha, files, title,
			narration, risk, order_hint, seq, subtask_id, dirty_fingerprint)
		VALUES (:id, :task_id, :tour_id, :kind, :commit_sha, :files, :title,
			:narration, :risk, :order_hint, :seq, :subtask_id, :dirty_fingerprint)`, step); err != nil {
		return err
	}
	for _, hunk := range hunks {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO review_chapter_hunks (id, task_id, tour_id, step_id, file_path, hunk_anchor, seq, generated)
			VALUES (:id, :task_id, :tour_id, :step_id, :file_path, :hunk_anchor, :seq, :generated)`, hunk); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListReviewChapterHunks returns memberships in authored reading order.
func (s *Store) ListReviewChapterHunks(ctx context.Context, stepID string) ([]ReviewChapterHunk, error) {
	var hunks []ReviewChapterHunk
	err := s.db.SelectContext(ctx, &hunks,
		"SELECT * FROM review_chapter_hunks WHERE step_id = ? ORDER BY seq ASC", stepID)
	return hunks, err
}

// DeleteReviewChapter removes a chapter and its memberships atomically.
func (s *Store) DeleteReviewChapter(ctx context.Context, stepID string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_chapter_hunks WHERE step_id = ?", stepID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_steps WHERE id = ? AND kind = 'chapter'", stepID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListReviewTours returns all tours, most recently updated first.
func (s *Store) ListReviewTours(ctx context.Context) ([]ReviewTour, error) {
	var tours []ReviewTour
	err := s.db.SelectContext(ctx, &tours,
		"SELECT * FROM review_tours ORDER BY updated_at DESC, task_id ASC")
	return tours, err
}

// InsertReviewHunkNote persists a content-anchored note. IDs are caller
// generated so the service can return the durable note identity.
func (s *Store) InsertReviewHunkNote(ctx context.Context, note ReviewHunkNote) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO review_hunk_notes (id, task_id, tour_id, step_id, file_path, hunk_anchor, body)
		VALUES (:id, :task_id, :tour_id, :step_id, :file_path, :hunk_anchor, :body)`, note)
	return err
}

// ListReviewHunkNotes returns a tour's hunk notes oldest first.
func (s *Store) ListReviewHunkNotes(ctx context.Context, tourID string) ([]ReviewHunkNote, error) {
	var notes []ReviewHunkNote
	err := s.db.SelectContext(ctx, &notes, `
		SELECT * FROM review_hunk_notes WHERE tour_id = ? ORDER BY created_at ASC, id ASC`, tourID)
	return notes, err
}

// InsertReviewMessage appends a Q&A transcript entry. delivered records
// whether the message actually reached the agent's pane (questions sent while
// the agent session is dead are stored with a NULL delivered_at).
func (s *Store) InsertReviewMessage(ctx context.Context, m ReviewMessage, delivered bool) (*ReviewMessage, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO review_transcript (task_id, tour_id, step_id, kind, author, body, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, CASE WHEN ? THEN datetime('now') ELSE NULL END)`,
		m.TaskID, m.TourID, m.StepID, m.Kind, m.Author, m.Body, delivered)
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

// ListReviewMessages returns a tour's Q&A transcript, oldest first.
func (s *Store) ListReviewMessages(ctx context.Context, tourID string) ([]ReviewMessage, error) {
	var msgs []ReviewMessage
	err := s.db.SelectContext(ctx, &msgs,
		"SELECT * FROM review_transcript WHERE tour_id = ? ORDER BY id ASC", tourID)
	return msgs, err
}

// UnreviewedReviewCounts returns, per task, the number of non-orphaned steps
// not yet marked reviewed. Drives the board badge.
func (s *Store) UnreviewedReviewCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.task_id, COUNT(*) FROM review_steps s
		WHERE s.reviewed_at IS NULL AND s.orphaned_at IS NULL
		  AND (
		    s.kind = 'chapter'
		    OR NOT EXISTS (
		      SELECT 1 FROM review_steps c
		      WHERE c.task_id = s.task_id AND c.kind = 'chapter'
		    )
		  )
		GROUP BY s.task_id`)
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

// DeleteReviewTour atomically removes every review artifact for a tour while
// leaving the task, repository, worktree, and agent records untouched.
func (s *Store) DeleteReviewTour(ctx context.Context, id string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		"DELETE FROM review_chapter_hunks WHERE tour_id = ?",
		"DELETE FROM review_hunk_notes WHERE tour_id = ?",
		"DELETE FROM review_transcript WHERE tour_id = ?",
		"DELETE FROM review_steps WHERE tour_id = ?",
		"DELETE FROM review_tours WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateReviewTour applies mutate to the tour row and persists the result.
func (s *Store) UpdateReviewTour(ctx context.Context, id string, mutate func(*ReviewTour)) (*ReviewTour, error) {
	rt, err := s.GetReviewTour(ctx, id)
	if err != nil {
		return nil, err
	}
	mutate(rt)
	_, err = s.db.NamedExecContext(ctx, `
		UPDATE review_tours SET
			status = :status, summary = :summary, base_sha = :base_sha, head_sha = :head_sha,
			repository_path = :repository_path, last_reviewed_sha = :last_reviewed_sha, ready_at = :ready_at,
			updated_at = datetime('now')
		WHERE id = :id`, rt)
	if err != nil {
		return nil, err
	}
	return s.GetReviewTour(ctx, id)
}
