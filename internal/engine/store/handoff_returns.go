package store

import (
	"context"
	"database/sql"
	"errors"
)

// Bringing an away task home: return and recall, in two steps. On a worker,
// step 1 marks the handoff returning and step 2 closes it and archives its
// card. On a main, the person's request is kept on the away task, the
// worker's results are applied in one transaction that closes it, and
// step 2 stays owed until the worker confirms it.

// HandoffReturning is a worker's handoff on its way back to its main: its
// agent stopped, its card frozen until step 2 archives it.
const HandoffReturning = "returning"

// MarkHandoffReturning marks a main's open handoff returning, whatever state
// it is in, and returns it. A closed handoff is returned as it is: step 1
// repeated after step 2 still answers. ErrNotFound when there is none.
func (s *Store) MarkHandoffReturning(ctx context.Context, mainInstanceID, handoffID, at string) (*Handoff, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE handoffs SET state = ?, updated_at = ?
		WHERE main_instance_id = ? AND handoff_id = ? AND closed_at IS NULL AND state <> ?`,
		HandoffReturning, at, mainInstanceID, handoffID, HandoffReturning); err != nil {
		return nil, err
	}
	return s.GetHandoff(ctx, mainInstanceID, handoffID)
}

// CloseHandoffAndArchive closes a main's handoff and archives its card, in
// one transaction: step 2 of a return. Repeating it changes nothing.
// ErrNotFound when there is no such handoff.
func (s *Store) CloseHandoffAndArchive(ctx context.Context, mainInstanceID, handoffID, at string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	var taskID string
	err = tx.GetContext(ctx, &taskID, `SELECT task_id FROM handoffs WHERE main_instance_id = ? AND handoff_id = ?`, mainInstanceID, handoffID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE handoffs SET closed_at = ?, updated_at = ?
		WHERE main_instance_id = ? AND handoff_id = ? AND closed_at IS NULL`, at, at, mainInstanceID, handoffID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET archived_at = datetime('now') WHERE id = ? AND archived_at IS NULL`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// AskAwayTaskReturn records that the person asked to bring an open away task
// home, at, and the state a recall was made from (empty for a return).
func (s *Store) AskAwayTaskReturn(ctx context.Context, handoffID, recalledFrom, at string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE away_tasks SET return_asked_at = ?, recalled_from = ?, updated_at = ?
		WHERE handoff_id = ? AND closed_at IS NULL`, at, recalledFrom, at, handoffID)
	return err
}

// ClearAwayTaskReturn forgets a request to bring an away task home that the
// worker refused.
func (s *Store) ClearAwayTaskReturn(ctx context.Context, handoffID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE away_tasks SET return_asked_at = NULL, recalled_from = '' WHERE handoff_id = ? AND closed_at IS NULL`, handoffID)
	return err
}

// AwayReturn is what a worker gave back for an away task, as the main
// applies it: the task's title, description, column and code context, the
// notes to add (each skipped when the task has a note of the same source
// and remote ID), and the stack document.
type AwayReturn struct {
	HandoffID   string
	TaskID      string
	Title       string
	Description string
	Status      string
	CodeContext *CodeContext
	Notes       []TaskNote
	Stack       *string
	At          string
}

// ApplyAwayTaskReturn applies a worker's results to an open away task and
// closes it, in one transaction, leaving step 2 owed (archive_owed). It
// says whether it applied them: false when the away task was closed
// already, and nothing changes.
func (s *Store) ApplyAwayTaskReturn(ctx context.Context, r AwayReturn) (bool, error) {
	codeContext, err := MarshalCodeContext(r.CodeContext)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	result, err := tx.ExecContext(ctx, `UPDATE away_tasks SET closed_at = ?, updated_at = ?, archive_owed = 1, returned_stack = ?
		WHERE handoff_id = ? AND task_id = ? AND closed_at IS NULL`, r.At, r.At, r.Stack, r.HandoffID, r.TaskID)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ?, description = ?, description_md = ?, status = ?,
			sort_order = (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM tasks WHERE status = ? AND archived_at IS NULL AND id <> ?),
			code_context = COALESCE(?, code_context), updated_at = datetime('now')
		WHERE id = ?`, r.Title, r.Description, r.Description, r.Status, r.Status, r.TaskID, codeContext, r.TaskID); err != nil {
		return false, err
	}
	for _, n := range r.Notes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_notes (id, task_id, body, source, remote_id, created_at)
			SELECT ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), datetime('now'))
			WHERE ? = '' OR NOT EXISTS (SELECT 1 FROM task_notes WHERE task_id = ? AND source = ? AND remote_id = ?)`,
			n.ID, r.TaskID, n.Body, n.Source, n.RemoteID, n.CreatedAt, n.RemoteID, r.TaskID, n.Source, n.RemoteID); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// ListArchiveOwed lists the returned tasks of a worker ("" for every
// worker) whose card it has not yet confirmed archiving, oldest first.
func (s *Store) ListArchiveOwed(ctx context.Context, workerInstanceID string) ([]AwayTask, error) {
	var out []AwayTask
	err := s.db.SelectContext(ctx, &out, awayTaskSelect+` WHERE (? = '' OR a.worker_instance_id = ?) AND a.archive_owed = 1 ORDER BY a.closed_at, a.rowid`,
		workerInstanceID, workerInstanceID)
	return out, err
}

// ClearArchiveOwed records that the worker archived a returned task's card.
func (s *Store) ClearArchiveOwed(ctx context.Context, handoffID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE away_tasks SET archive_owed = 0 WHERE handoff_id = ?`, handoffID)
	return err
}
