package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrExecutionTerminal = errors.New("execution is terminal")

const executionTimestamp = "2006-01-02 15:04:05.999999999"

func formatExecutionTime(value time.Time) string {
	return value.UTC().Format(executionTimestamp)
}

func (s *Store) CreateExecution(ctx context.Context, execution Execution) error {
	if execution.ID == "" || execution.StartedAt.IsZero() {
		return errors.New("execution id and start time are required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO executions (
			id, kind, status, task_id, execution_task_id, task_title,
			workflow_run_id, workflow_attempt_id, swarm_id, swarm_subtask_id,
			agent_session_id, agent_kind, role, working_dir, provider_session_id,
			started_at, ended_at
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''),
			NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)`,
		execution.ID, execution.Kind, execution.Status, execution.TaskID,
		execution.ExecutionTaskID, execution.TaskTitle, execution.WorkflowRunID,
		execution.WorkflowAttemptID, execution.SwarmID, execution.SwarmSubtaskID,
		execution.AgentSessionID, execution.AgentKind, execution.Role,
		execution.WorkingDir, execution.ProviderSessionID,
		formatExecutionTime(execution.StartedAt), nullableExecutionTime(execution.EndedAt),
	)
	return err
}

func nullableExecutionTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatExecutionTime(*value)
}

func (s *Store) GetExecution(ctx context.Context, id string) (*Execution, error) {
	var execution Execution
	if err := s.db.GetContext(ctx, &execution, `
		SELECT id, kind, status, COALESCE(task_id, '') AS task_id,
			COALESCE(execution_task_id, '') AS execution_task_id, task_title,
			COALESCE(workflow_run_id, '') AS workflow_run_id,
			COALESCE(workflow_attempt_id, '') AS workflow_attempt_id,
			COALESCE(swarm_id, '') AS swarm_id,
			COALESCE(swarm_subtask_id, '') AS swarm_subtask_id,
			agent_session_id, agent_kind, role, working_dir, provider_session_id,
			started_at, ended_at
		FROM executions WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &execution, nil
}

func (s *Store) LinkExecutionAgentSession(ctx context.Context, executionID string, sessionID int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE executions SET agent_session_id = ?
		WHERE id = ? AND status = 'running'`, sessionID, executionID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		if _, getErr := s.GetExecution(ctx, executionID); getErr != nil {
			return getErr
		}
		return ErrExecutionTerminal
	}
	return nil
}

func (s *Store) RecordExecutionActivity(ctx context.Context, executionID, state, workingDir string) error {
	return s.RecordExecutionActivityAt(ctx, executionID, state, workingDir, time.Now().UTC())
}

func (s *Store) RecordExecutionActivityAt(ctx context.Context, executionID, state, workingDir string, at time.Time) error {
	if state != "" && state != ActivityWorking && state != ActivityWaiting {
		return fmt.Errorf("invalid execution activity %q", state)
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.GetContext(ctx, &status, `SELECT status FROM executions WHERE id = ?`, executionID); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != ExecutionRunning {
		return ErrExecutionTerminal
	}

	var current ExecutionActivity
	err = tx.GetContext(ctx, &current, `
		SELECT id, execution_id, state, started_at, ended_at, COALESCE(working_dir, '') AS working_dir
		FROM execution_activity WHERE execution_id = ? AND ended_at IS NULL`, executionID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && current.State == state {
		return tx.Commit()
	}
	stamp := formatExecutionTime(at)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE execution_activity SET ended_at = ? WHERE id = ?`, stamp, current.ID); err != nil {
			return err
		}
	}
	if state != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO execution_activity (execution_id, state, started_at, working_dir)
			VALUES (?, ?, ?, NULLIF(?, ''))`, executionID, state, stamp, workingDir); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) TerminalizeExecution(ctx context.Context, executionID, status string) error {
	return s.TerminalizeExecutionAt(ctx, executionID, status, time.Now().UTC())
}

func (s *Store) TerminalizeExecutionAt(ctx context.Context, executionID, status string, at time.Time) error {
	if status == ExecutionRunning || (status != ExecutionSucceeded && status != ExecutionFailed && status != ExecutionCancelled && status != ExecutionLost) {
		return fmt.Errorf("invalid terminal execution status %q", status)
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current string
	if err := tx.GetContext(ctx, &current, `SELECT status FROM executions WHERE id = ?`, executionID); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if current != ExecutionRunning {
		return tx.Commit()
	}
	stamp := formatExecutionTime(at)
	if _, err := tx.ExecContext(ctx, `UPDATE execution_activity SET ended_at = ? WHERE execution_id = ? AND ended_at IS NULL`, stamp, executionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE executions SET status = ?, ended_at = ? WHERE id = ? AND status = 'running'`, status, stamp, executionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetExecutionDurations(ctx context.Context, executionID string) (map[string]time.Duration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.state, CAST(ROUND(SUM(MAX(0,
			(julianday(MIN(COALESCE(a.ended_at, e.ended_at, datetime('now')), COALESCE(e.ended_at, datetime('now')))) - julianday(a.started_at)) * 86400
		))) AS INTEGER)
		FROM execution_activity a
		JOIN executions e ON e.id = a.execution_id
		WHERE a.execution_id = ?
		GROUP BY a.state`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]time.Duration)
	for rows.Next() {
		var state string
		var seconds int64
		if err := rows.Scan(&state, &seconds); err != nil {
			return nil, err
		}
		result[state] = time.Duration(seconds) * time.Second
	}
	return result, rows.Err()
}

func (s *Store) ActivityEpoch(ctx context.Context) (time.Time, error) {
	var epoch time.Time
	err := s.db.GetContext(ctx, &epoch, `SELECT activity_epoch FROM reporting_metadata WHERE id = 1`)
	return epoch, err
}
