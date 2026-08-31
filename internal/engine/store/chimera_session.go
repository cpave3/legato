package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

func (s *Store) ReserveChimeraCleanup(ctx context.Context, taskID, workflowRunID, workflowStepID string) (string, error) {
	var sessionID string
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if err := tx.GetContext(ctx, &sessionID, `SELECT chimera_session_id FROM chimera_cleanup_reservations WHERE workflow_run_id = ? AND workflow_step_id = ?`, workflowRunID, workflowStepID); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var running int
		if err := tx.GetContext(ctx, &running, `SELECT COUNT(*) FROM agent_sessions WHERE task_id = ? AND status = 'running'`, taskID); err != nil {
			return err
		}
		if running > 0 {
			return fmt.Errorf("task %s has a running agent; stop it before removing its Chimera session", taskID)
		}
		if err := tx.GetContext(ctx, &sessionID, `SELECT COALESCE(chimera_session_id, '') FROM tasks WHERE id = ?`, taskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if sessionID == "" {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO chimera_cleanup_reservations (task_id, workflow_run_id, workflow_step_id, chimera_session_id) VALUES (?, ?, ?, ?)`, taskID, workflowRunID, workflowStepID, sessionID)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrConflict
		}
		return err
	})
	return sessionID, err
}

func (s *Store) ReleaseChimeraCleanup(ctx context.Context, taskID, workflowRunID, workflowStepID, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM chimera_cleanup_reservations WHERE task_id = ? AND workflow_run_id = ? AND workflow_step_id = ? AND chimera_session_id = ?`, taskID, workflowRunID, workflowStepID, sessionID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

// ClearTaskChimeraSession clears resumable Chimera metadata only while the
// task still references the session observed by the caller.
func (s *Store) ClearTaskChimeraSession(ctx context.Context, taskID, expectedSessionID string) (bool, error) {
	return s.clearTaskChimeraSession(ctx, taskID, expectedSessionID, "", "")
}

func (s *Store) CompleteChimeraCleanup(ctx context.Context, taskID, expectedSessionID, workflowRunID, workflowStepID string) (bool, error) {
	return s.clearTaskChimeraSession(ctx, taskID, expectedSessionID, workflowRunID, workflowStepID)
}

func (s *Store) clearTaskChimeraSession(ctx context.Context, taskID, expectedSessionID, workflowRunID, workflowStepID string) (bool, error) {
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
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET chimera_session_id = NULL, chimera_endpoint = NULL, last_event_id = NULL
		WHERE task_id = ? AND chimera_session_id = ?`, taskID, expectedSessionID)
	if err != nil {
		return false, err
	}
	agentRows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if workflowRunID != "" {
		result, err := tx.ExecContext(ctx, `DELETE FROM chimera_cleanup_reservations WHERE task_id = ? AND workflow_run_id = ? AND workflow_step_id = ? AND chimera_session_id = ?`, taskID, workflowRunID, workflowStepID, expectedSessionID)
		if err != nil {
			return false, err
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			if err != nil {
				return false, err
			}
			return false, ErrConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return taskRows > 0 || agentRows > 0, nil
}
