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
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO review_tours (id, task_id, name) VALUES (?, ?, ?)", id, taskID, name); err != nil {
		return nil, err
	}
	passID := id + "-p1"
	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO review_passes (id, tour_id, number) VALUES (?, ?, 1)", passID, id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO review_pass_plans (pass_id, plan_id, revision_id, markdown)
		SELECT ?, p.id, r.id, r.markdown
		FROM plans p JOIN plan_revisions r ON r.plan_id = p.id AND r.revision = p.latest_revision
		WHERE p.task_id = ? AND p.status IN ('approved', 'completed')
		ORDER BY COALESCE(p.completed_at, p.approved_at, p.updated_at) DESC, p.rowid DESC LIMIT 1`, passID, taskID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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

func (s *Store) ListReviewPasses(ctx context.Context, tourID string) ([]ReviewPass, error) {
	var passes []ReviewPass
	err := s.db.SelectContext(ctx, &passes, "SELECT * FROM review_passes WHERE tour_id = ? ORDER BY number", tourID)
	return passes, err
}

func (s *Store) GetActiveReviewPass(ctx context.Context, tourID string) (*ReviewPass, error) {
	var pass ReviewPass
	err := s.db.GetContext(ctx, &pass, "SELECT * FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1", tourID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &pass, err
}

func (s *Store) ListReviewStepsByPass(ctx context.Context, passID string) ([]ReviewStep, error) {
	var steps []ReviewStep
	err := s.db.SelectContext(ctx, &steps, `
		SELECT * FROM review_steps WHERE pass_id = ?
		ORDER BY (order_hint IS NULL), order_hint ASC, seq ASC`, passID)
	return steps, err
}

func (s *Store) ListReviewMessagesByPass(ctx context.Context, passID string) ([]ReviewMessage, error) {
	var messages []ReviewMessage
	err := s.db.SelectContext(ctx, &messages, "SELECT * FROM review_transcript WHERE pass_id = ? ORDER BY id", passID)
	return messages, err
}

func (s *Store) ListReviewHunkNotesByPass(ctx context.Context, passID string) ([]ReviewHunkNote, error) {
	var notes []ReviewHunkNote
	err := s.db.SelectContext(ctx, &notes, "SELECT * FROM review_hunk_notes WHERE pass_id = ? ORDER BY created_at, id", passID)
	return notes, err
}

func (s *Store) InsertReviewFinding(ctx context.Context, finding ReviewFinding) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO review_findings (id, task_id, tour_id, pass_id, step_id, file_path,
			hunk_anchor, line_start, line_end, body, status)
		VALUES (:id, :task_id, :tour_id, :pass_id, :step_id, :file_path,
			:hunk_anchor, :line_start, :line_end, :body, :status)`, finding)
	return err
}

func (s *Store) ListReviewFindingsByPass(ctx context.Context, passID string) ([]ReviewFinding, error) {
	var findings []ReviewFinding
	err := s.db.SelectContext(ctx, &findings, "SELECT * FROM review_findings WHERE pass_id = ? ORDER BY created_at, id", passID)
	return findings, err
}

func (s *Store) InsertReviewPlanRequest(ctx context.Context, request ReviewPlanRequest, findingIDs []string) (*ReviewPlanRequest, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int
	query := `SELECT COUNT(*) FROM review_findings WHERE pass_id = ? AND status = 'open' AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(findingIDs)), ",") + `)`
	args := []any{request.PassID}
	for _, id := range findingIDs {
		args = append(args, id)
	}
	if err := tx.GetContext(ctx, &count, query, args...); err != nil {
		return nil, err
	}
	if count != len(findingIDs) {
		return nil, fmt.Errorf("selected findings must be open and belong to the active review pass")
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_plan_requests (id, task_id, tour_id, pass_id)
		VALUES (:id, :task_id, :tour_id, :pass_id)`, request); err != nil {
		return nil, err
	}
	for _, findingID := range findingIDs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO review_plan_request_findings (request_id, finding_id) VALUES (?, ?)", request.ID, findingID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	requests, err := s.ListReviewPlanRequestsByPass(ctx, request.PassID)
	if err != nil {
		return nil, err
	}
	return &requests[len(requests)-1], nil
}

func (s *Store) MarkReviewPlanRequestDelivered(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE review_plan_requests SET delivered_at = datetime('now') WHERE id = ?", id)
	return err
}

func (s *Store) ListReviewPlanRequestsByPass(ctx context.Context, passID string) ([]ReviewPlanRequest, error) {
	var requests []ReviewPlanRequest
	if err := s.db.SelectContext(ctx, &requests, "SELECT * FROM review_plan_requests WHERE pass_id = ? ORDER BY created_at, id", passID); err != nil {
		return nil, err
	}
	for i := range requests {
		if err := s.db.SelectContext(ctx, &requests[i].FindingIDs, "SELECT finding_id FROM review_plan_request_findings WHERE request_id = ? ORDER BY finding_id", requests[i].ID); err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *Store) GetReviewPassPlan(ctx context.Context, passID string) (*ReviewPassPlan, error) {
	var plan ReviewPassPlan
	err := s.db.GetContext(ctx, &plan, `
		SELECT pp.pass_id, pp.plan_id, pp.revision_id, r.revision, p.title, pp.markdown, pp.created_at
		FROM review_pass_plans pp
		JOIN plans p ON p.id = pp.plan_id
		JOIN plan_revisions r ON r.id = pp.revision_id
		WHERE pp.pass_id = ?`, passID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &plan, err
}

func (s *Store) UpdateReviewPass(ctx context.Context, id string, mutate func(*ReviewPass)) (*ReviewPass, error) {
	var pass ReviewPass
	if err := s.db.GetContext(ctx, &pass, "SELECT * FROM review_passes WHERE id = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	mutate(&pass)
	if _, err := s.db.NamedExecContext(ctx, `UPDATE review_passes SET status = :status, summary = :summary,
		guidance = :guidance, head_sha = :head_sha, ready_at = :ready_at, reviewed_at = :reviewed_at,
		updated_at = datetime('now') WHERE id = :id`, pass); err != nil {
		return nil, err
	}
	return s.GetActiveReviewPass(ctx, pass.TourID)
}

func (s *Store) AdvanceReviewPass(ctx context.Context, tourID, guidance string) (*ReviewPass, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current ReviewPass
	if err := tx.GetContext(ctx, &current, "SELECT * FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1", tourID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE review_passes SET status = 'superseded', updated_at = datetime('now') WHERE id = ?", current.ID); err != nil {
		return nil, err
	}
	next := ReviewPass{ID: fmt.Sprintf("%s-p%d", tourID, current.Number+1), TourID: tourID, Number: current.Number + 1, Status: "capturing", Guidance: guidance}
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO review_passes (id, tour_id, number, status, guidance) VALUES (:id, :tour_id, :number, :status, :guidance)`, next); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO review_pass_plans (pass_id, plan_id, revision_id, markdown)
		SELECT ?, p.id, r.id, r.markdown
		FROM review_tours t JOIN plans p ON p.task_id = t.task_id
		JOIN plan_revisions r ON r.plan_id = p.id AND r.revision = p.latest_revision
		WHERE t.id = ? AND p.status IN ('approved', 'completed')
		ORDER BY COALESCE(p.completed_at, p.approved_at, p.updated_at) DESC, p.rowid DESC LIMIT 1`, next.ID, tourID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_tours SET status = 'capturing', summary = '', head_sha = '',
		last_reviewed_sha = '', ready_at = NULL, updated_at = datetime('now') WHERE id = ?`, tourID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetActiveReviewPass(ctx, tourID)
}

// InsertReviewStep adds a step to a tour. Commit steps are deduped per pass
// on their SHA; the returned bool reports whether a row was actually inserted.
func (s *Store) InsertReviewStep(ctx context.Context, step ReviewStep) (bool, error) {
	res, err := s.db.NamedExecContext(ctx, `
		INSERT OR IGNORE INTO review_steps (id, task_id, tour_id, pass_id, kind, commit_sha, files,
			title, narration, risk, order_hint, seq, subtask_id, dirty_fingerprint)
		VALUES (:id, :task_id, :tour_id,
			COALESCE(NULLIF(:pass_id, ''), (SELECT id FROM review_passes WHERE tour_id = :tour_id ORDER BY number DESC LIMIT 1), ''),
			:kind, :commit_sha, :files, :title, :narration, :risk, :order_hint, :seq, :subtask_id, :dirty_fingerprint)`, step)
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
		  AND (pass_id = COALESCE((SELECT id FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1), '') OR pass_id = '')
		ORDER BY (order_hint IS NULL), order_hint ASC, seq ASC`, tourID, tourID)
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
			"WHERE task_id = ? AND kind = 'commit' AND orphaned_at IS NULL AND "+
			"(pass_id = COALESCE((SELECT rp.id FROM review_passes rp JOIN review_tours rt ON rt.id = rp.tour_id WHERE rt.task_id = review_steps.task_id ORDER BY rp.number DESC LIMIT 1), '') OR pass_id = '') "+
			"AND commit_sha NOT IN (%s)",
		taskID, liveSHAs)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	query, args = buildInClause(
		"UPDATE review_steps SET orphaned_at = NULL, updated_at = datetime('now') "+
			"WHERE task_id = ? AND kind = 'commit' AND orphaned_at IS NOT NULL AND "+
			"(pass_id = COALESCE((SELECT rp.id FROM review_passes rp JOIN review_tours rt ON rt.id = rp.tour_id WHERE rt.task_id = review_steps.task_id ORDER BY rp.number DESC LIMIT 1), '') OR pass_id = '') "+
			"AND commit_sha IN (%s)",
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
	if step.PassID == "" {
		if err := tx.GetContext(ctx, &step.PassID, "SELECT id FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1", step.TourID); err != nil {
			return err
		}
	}
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO review_steps (id, task_id, tour_id, pass_id, kind, commit_sha, files, title,
			narration, risk, order_hint, seq, subtask_id, dirty_fingerprint)
		VALUES (:id, :task_id, :tour_id, :pass_id, :kind, :commit_sha, :files, :title,
			:narration, :risk, :order_hint, :seq, :subtask_id, :dirty_fingerprint)`, step); err != nil {
		return err
	}
	for _, hunk := range hunks {
		if hunk.PassID == "" {
			hunk.PassID = step.PassID
		}
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO review_chapter_hunks (id, task_id, tour_id, pass_id, step_id, file_path, hunk_anchor, seq, generated)
			VALUES (:id, :task_id, :tour_id, :pass_id, :step_id, :file_path, :hunk_anchor, :seq, :generated)`, hunk); err != nil {
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
	for _, query := range []string{
		"DELETE FROM review_chapter_hunks WHERE step_id = ?",
		"DELETE FROM review_hunk_notes WHERE step_id = ?",
		"DELETE FROM review_transcript WHERE step_id = ?",
	} {
		if _, err = tx.ExecContext(ctx, query, stepID); err != nil {
			return err
		}
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
		INSERT INTO review_hunk_notes (id, task_id, tour_id, pass_id, step_id, file_path, hunk_anchor,
			line_start, line_end, line_anchor, body, updated_at)
		VALUES (:id, :task_id, :tour_id,
			COALESCE(NULLIF(:pass_id, ''), (SELECT id FROM review_passes WHERE tour_id = :tour_id ORDER BY number DESC LIMIT 1), ''),
			:step_id, :file_path, :hunk_anchor, :line_start, :line_end, :line_anchor, :body, datetime('now'))`, note)
	return err
}

// GetReviewHunkNoteByPrefix resolves a note within a tour by ID prefix.
func (s *Store) GetReviewHunkNoteByPrefix(ctx context.Context, tourID, prefix string) (*ReviewHunkNote, error) {
	var notes []ReviewHunkNote
	if err := s.db.SelectContext(ctx, &notes,
		"SELECT * FROM review_hunk_notes WHERE tour_id = ? AND id LIKE ? || '%' LIMIT 2", tourID, prefix); err != nil {
		return nil, err
	}
	switch len(notes) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &notes[0], nil
	default:
		return nil, fmt.Errorf("annotation id prefix %q is ambiguous", prefix)
	}
}

// UpdateReviewHunkNoteBody replaces an annotation's narration.
func (s *Store) UpdateReviewHunkNoteBody(ctx context.Context, id, body string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE review_hunk_notes SET body = ?, updated_at = datetime('now') WHERE id = ?", body, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteReviewHunkNote removes one durable annotation.
func (s *Store) DeleteReviewHunkNote(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM review_hunk_notes WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListReviewHunkNotes returns a tour's hunk notes oldest first.
func (s *Store) ListReviewHunkNotes(ctx context.Context, tourID string) ([]ReviewHunkNote, error) {
	var notes []ReviewHunkNote
	err := s.db.SelectContext(ctx, &notes, `
		SELECT * FROM review_hunk_notes WHERE tour_id = ?
		  AND (pass_id = COALESCE((SELECT id FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1), '') OR pass_id = '')
		ORDER BY created_at ASC, id ASC`, tourID, tourID)
	return notes, err
}

// InsertReviewMessage appends a Q&A transcript entry. delivered records
// whether the message actually reached the agent's pane (questions sent while
// the agent session is dead are stored with a NULL delivered_at).
func (s *Store) InsertReviewMessage(ctx context.Context, m ReviewMessage, delivered bool) (*ReviewMessage, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO review_transcript (task_id, tour_id, pass_id, step_id, kind, author, body, delivered_at)
		VALUES (?, ?, COALESCE(NULLIF(?, ''), (SELECT id FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1), ''),
			?, ?, ?, ?, CASE WHEN ? THEN datetime('now') ELSE NULL END)`,
		m.TaskID, m.TourID, m.PassID, m.TourID, m.StepID, m.Kind, m.Author, m.Body, delivered)
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
	err := s.db.SelectContext(ctx, &msgs, `SELECT * FROM review_transcript WHERE tour_id = ?
		AND (pass_id = COALESCE((SELECT id FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1), '') OR pass_id = '')
		ORDER BY id ASC`, tourID, tourID)
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
		"DELETE FROM plan_review_origins WHERE review_pass_id IN (SELECT id FROM review_passes WHERE tour_id = ?)",
		"DELETE FROM review_plan_request_findings WHERE request_id IN (SELECT id FROM review_plan_requests WHERE tour_id = ?)",
		"DELETE FROM review_plan_requests WHERE tour_id = ?",
		"DELETE FROM review_findings WHERE tour_id = ?",
		"DELETE FROM review_chapter_hunks WHERE tour_id = ?",
		"DELETE FROM review_hunk_notes WHERE tour_id = ?",
		"DELETE FROM review_transcript WHERE tour_id = ?",
		"DELETE FROM review_steps WHERE tour_id = ?",
		"DELETE FROM review_pass_plans WHERE pass_id IN (SELECT id FROM review_passes WHERE tour_id = ?)",
		"DELETE FROM review_passes WHERE tour_id = ?",
		"DELETE FROM review_tours WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RestartReviewTour atomically removes review artifacts and resets lifecycle
// state while retaining the repository and base SHA that define the capture.
func (s *Store) RestartReviewTour(ctx context.Context, id string) error {
	return s.RestartReviewTourWithGuidance(ctx, id, "")
}

func (s *Store) RestartReviewTourWithGuidance(ctx context.Context, id, guidance string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current ReviewPass
	if err := tx.GetContext(ctx, &current, "SELECT * FROM review_passes WHERE tour_id = ? ORDER BY number DESC LIMIT 1", id); err != nil {
		return err
	}
	for _, query := range []string{
		"DELETE FROM plan_review_origins WHERE review_pass_id = ?",
		"DELETE FROM review_plan_request_findings WHERE request_id IN (SELECT id FROM review_plan_requests WHERE pass_id = ?)",
		"DELETE FROM review_plan_requests WHERE pass_id = ?",
		"DELETE FROM review_findings WHERE pass_id = ?",
		"DELETE FROM review_chapter_hunks WHERE pass_id = ?",
		"DELETE FROM review_hunk_notes WHERE pass_id = ?",
		"DELETE FROM review_transcript WHERE pass_id = ?",
		"DELETE FROM review_steps WHERE pass_id = ?",
		"DELETE FROM review_pass_plans WHERE pass_id = ?",
		"DELETE FROM review_passes WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, query, current.ID); err != nil {
			return err
		}
	}
	replacementID := current.ID + "r"
	if _, err := tx.ExecContext(ctx, "INSERT INTO review_passes (id, tour_id, number, guidance) VALUES (?, ?, ?, ?)", replacementID, id, current.Number, guidance); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO review_pass_plans (pass_id, plan_id, revision_id, markdown)
		SELECT ?, p.id, r.id, r.markdown
		FROM review_tours t JOIN plans p ON p.task_id = t.task_id
		JOIN plan_revisions r ON r.plan_id = p.id AND r.revision = p.latest_revision
		WHERE t.id = ? AND p.status IN ('approved', 'completed')
		ORDER BY COALESCE(p.completed_at, p.approved_at, p.updated_at) DESC, p.rowid DESC LIMIT 1`, replacementID, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE review_tours SET status = 'capturing', summary = '', head_sha = '',
			last_reviewed_sha = '', ready_at = NULL, updated_at = datetime('now')
		WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
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
