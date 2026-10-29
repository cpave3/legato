package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CredentialPlaceholder is what a placeholder key stands for.
type CredentialPlaceholder struct {
	ExecutionID string `db:"execution_id"`
	TaskID      string `db:"task_id"`
	Credential  string `db:"credential"`
}

// CreateCredentialPlaceholder records the hash of a placeholder key made for
// an execution and a credential. The placeholder itself is never stored.
func (s *Store) CreateCredentialPlaceholder(ctx context.Context, hash, executionID, taskID, credential string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO credential_placeholders (hash, execution_id, task_id, credential) VALUES (?, ?, ?, ?)`,
		hash, executionID, taskID, credential)
	return err
}

// ResolveCredentialPlaceholder returns what a placeholder hash stands for,
// while it is not revoked and its execution is running. ErrNotFound otherwise.
func (s *Store) ResolveCredentialPlaceholder(ctx context.Context, hash string) (*CredentialPlaceholder, error) {
	var p CredentialPlaceholder
	err := s.db.GetContext(ctx, &p, `
		SELECT p.execution_id, p.task_id, p.credential FROM credential_placeholders p
		JOIN executions e ON e.id = p.execution_id
		WHERE p.hash = ? AND p.revoked_at IS NULL AND e.status = ?`, hash, ExecutionRunning)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// RevokeCredentialPlaceholders ends every placeholder of an execution.
func (s *Store) RevokeCredentialPlaceholders(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE credential_placeholders SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE execution_id = ? AND revoked_at IS NULL`, executionID)
	return err
}

const meterTime = "2006-01-02T15:04:05.000Z"

// AddCredentialMeter records an amount a channel measured of one use.
func (s *Store) AddCredentialMeter(ctx context.Context, credential, meter string, amount int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO credential_meter_totals (credential, meter, amount, at) VALUES (?, ?, ?, ?)`,
		credential, meter, amount, at.UTC().Format(meterTime))
	return err
}

// CredentialMeterTotal adds up a credential's meter since a time.
func (s *Store) CredentialMeterTotal(ctx context.Context, credential, meter string, since time.Time) (int64, error) {
	var total int64
	err := s.db.GetContext(ctx, &total, `SELECT COALESCE(SUM(amount), 0) FROM credential_meter_totals WHERE credential = ? AND meter = ? AND at >= ?`,
		credential, meter, since.UTC().Format(meterTime))
	return total, err
}

// ListRunningExecutions returns every execution that is running.
func (s *Store) ListRunningExecutions(ctx context.Context) ([]Execution, error) {
	executions := []Execution{}
	err := s.db.SelectContext(ctx, &executions, `
		SELECT id, kind, status, COALESCE(task_id, '') AS task_id,
			COALESCE(execution_task_id, '') AS execution_task_id, task_title,
			COALESCE(workflow_run_id, '') AS workflow_run_id,
			COALESCE(workflow_attempt_id, '') AS workflow_attempt_id,
			COALESCE(swarm_id, '') AS swarm_id,
			COALESCE(swarm_subtask_id, '') AS swarm_subtask_id,
			agent_session_id, agent_kind, role, working_dir, provider_session_id,
			started_at, ended_at
		FROM executions WHERE status = ? ORDER BY started_at, id`, ExecutionRunning)
	return executions, err
}
