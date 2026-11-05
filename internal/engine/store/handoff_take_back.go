package store

import (
	"context"
	"database/sql"
	"errors"
)

// Taking an away task back without its results, Drop and Leave. On a main,
// a task taken back closes its away task and leaves a Drop owed, and a
// removed worker is kept hidden (leaving) until it is told to unpair this
// main or its time runs out. On a worker, Drop closes a handoff and
// archives its card, and unpairing a main closes its handoffs and leaves
// their cards. See docs/ai/integrations.md § Take back, Drop and Leave.

// TakeBackAwayTask closes an open away task without the worker's results,
// adds note to its task and leaves a Drop owed, in one transaction. It says
// whether it took it back: false when the task was not away, and nothing
// changes.
func (s *Store) TakeBackAwayTask(ctx context.Context, handoffID string, note TaskNote, at string) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	result, err := tx.ExecContext(ctx, `UPDATE away_tasks SET closed_at = ?, updated_at = ?, taken_back_at = ?, drop_owed = 1
		WHERE handoff_id = ? AND closed_at IS NULL AND state = 'away'`, at, at, at, handoffID)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_notes (id, task_id, body, source, remote_id)
		SELECT ?, task_id, ?, ?, ? FROM away_tasks WHERE handoff_id = ?`, note.ID, note.Body, note.Source, note.RemoteID, handoffID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ListDropOwed lists the tasks taken back from a worker ("" for every
// worker) that it has not yet confirmed dropping, oldest first.
func (s *Store) ListDropOwed(ctx context.Context, workerInstanceID string) ([]AwayTask, error) {
	var out []AwayTask
	err := s.db.SelectContext(ctx, &out, awayTaskSelect+` WHERE (? = '' OR a.worker_instance_id = ?) AND a.drop_owed = 1 ORDER BY a.closed_at, a.rowid`,
		workerInstanceID, workerInstanceID)
	return out, err
}

// ClearDropOwed records that the worker dropped a task taken back.
func (s *Store) ClearDropOwed(ctx context.Context, handoffID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE away_tasks SET drop_owed = 0 WHERE handoff_id = ?`, handoffID)
	return err
}

// SetWorkerLeaving hides a removed worker until until, keeping it only to
// send the calls owed it.
func (s *Store) SetWorkerLeaving(ctx context.Context, instanceID, until string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workers SET leaving_until = ? WHERE instance_id = ?`, until, instanceID)
	return err
}

// DeleteWorker forgets a worker, and every call owed it, in one
// transaction. Forgetting one that is not there does nothing.
func (s *Store) DeleteWorker(ctx context.Context, instanceID string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	if _, err := tx.ExecContext(ctx, `DELETE FROM workers WHERE instance_id = ?`, instanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE away_tasks SET drop_owed = 0, archive_owed = 0 WHERE worker_instance_id = ? AND closed_at IS NOT NULL`, instanceID); err != nil {
		return err
	}
	return tx.Commit()
}

// CloseHandoffWithNote closes a main's open handoff and adds note to its
// card, archiving the card when archive is set, in one transaction: a Drop
// (archive) or an unpaired main (not). It says whether it closed it now:
// false when it was closed already, and nothing changes. ErrNotFound when
// there is no such handoff.
func (s *Store) CloseHandoffWithNote(ctx context.Context, mainInstanceID, handoffID string, note TaskNote, archive bool, at string) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	var taskID string
	err = tx.GetContext(ctx, &taskID, `SELECT task_id FROM handoffs WHERE main_instance_id = ? AND handoff_id = ?`, mainInstanceID, handoffID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	closed, err := closeHandoffTx(ctx, tx, mainInstanceID, handoffID, taskID, note, archive, at)
	if err != nil || !closed {
		return false, err
	}
	return true, tx.Commit()
}

// CloseMainHandoffs closes every open handoff of a main and adds the note
// it is given to each card, leaving the cards as they are, in one
// transaction. It returns the handoffs it closed.
func (s *Store) CloseMainHandoffs(ctx context.Context, mainInstanceID string, note func(Handoff) TaskNote, at string) ([]Handoff, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	var open []Handoff
	if err := tx.SelectContext(ctx, &open, `SELECT * FROM handoffs WHERE main_instance_id = ? AND closed_at IS NULL ORDER BY created_at, rowid`, mainInstanceID); err != nil {
		return nil, err
	}
	for _, h := range open {
		if _, err := closeHandoffTx(ctx, tx, mainInstanceID, h.HandoffID, h.TaskID, note(h), false, at); err != nil {
			return nil, err
		}
	}
	return open, tx.Commit()
}

func closeHandoffTx(ctx context.Context, tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, mainInstanceID, handoffID, taskID string, note TaskNote, archive bool, at string) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE handoffs SET closed_at = ?, updated_at = ?
		WHERE main_instance_id = ? AND handoff_id = ? AND closed_at IS NULL`, at, at, mainInstanceID, handoffID)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_notes (id, task_id, body, source, remote_id) VALUES (?, ?, ?, ?, ?)`,
		note.ID, taskID, note.Body, note.Source, note.RemoteID); err != nil {
		return false, err
	}
	if archive {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET archived_at = datetime('now') WHERE id = ? AND archived_at IS NULL`, taskID); err != nil {
			return false, err
		}
	}
	return true, nil
}
