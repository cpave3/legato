package store

import (
	"context"
	"fmt"
)

// ClearTaskChimeraSession clears resumable Chimera metadata only while the
// task still references the session observed by the caller.
func (s *Store) ClearTaskChimeraSession(ctx context.Context, taskID, expectedSessionID string) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var running int
	if err := tx.GetContext(ctx, &running, `SELECT COUNT(*) FROM agent_sessions WHERE task_id = ? AND status = 'running'`, taskID); err != nil {
		return false, err
	}
	if running > 0 {
		return false, fmt.Errorf("task %s has a running agent; stop it before removing its Chimera session", taskID)
	}

	result, err := tx.ExecContext(ctx, `UPDATE tasks SET chimera_session_id = NULL, updated_at = datetime('now') WHERE id = ? AND chimera_session_id = ?`, taskID, expectedSessionID)
	if err != nil {
		return false, err
	}
	taskRows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if taskRows == 0 {
		var exists int
		if err := tx.GetContext(ctx, &exists, `SELECT COUNT(*) FROM tasks WHERE id = ?`, taskID); err != nil {
			return false, err
		}
		if exists == 0 {
			return false, ErrNotFound
		}
		return false, tx.Commit()
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = NULL, chimera_endpoint = NULL, last_event_id = NULL
		WHERE task_id = ? AND chimera_session_id = ?`, taskID, expectedSessionID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
