package store

import (
	"context"
	"database/sql"
	"errors"
)

// SetExecutionBrokerToken records the hash of the token an execution's
// agent-side processes present to the broker.
func (s *Store) SetExecutionBrokerToken(ctx context.Context, executionID, hash string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE executions SET broker_token_hash = ? WHERE id = ?", hash, executionID)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

// ExecutionByBrokerToken returns the running execution whose broker token
// has this hash. A terminal execution's token resolves to nothing.
func (s *Store) ExecutionByBrokerToken(ctx context.Context, hash string) (*Execution, error) {
	if hash == "" {
		return nil, ErrNotFound
	}
	var id string
	err := s.db.GetContext(ctx, &id, "SELECT id FROM executions WHERE broker_token_hash = ? AND status = ?", hash, ExecutionRunning)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetExecution(ctx, id)
}

// SetExecutionReadOnly marks an execution as one that may look but not
// change: the credential broker refuses its writes.
func (s *Store) SetExecutionReadOnly(ctx context.Context, executionID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE executions SET read_only = 1 WHERE id = ?`, executionID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExecutionReadOnly says whether an execution may only look.
func (s *Store) ExecutionReadOnly(ctx context.Context, executionID string) (bool, error) {
	var readOnly bool
	err := s.db.GetContext(ctx, &readOnly, `SELECT read_only FROM executions WHERE id = ?`, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return readOnly, err
}
