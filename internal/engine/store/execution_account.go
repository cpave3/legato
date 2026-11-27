package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jmoiron/sqlx"
)

// AgentAccountIdentity freezes local configuration identity, not credentials.
// ConfigDir is the normalized lexical identity Claude starts with; ResolvedDir
// detects symlink retargeting. An unset default config variable stays unset.
type AgentAccountIdentity struct {
	Provider     string `json:"provider"`
	Account      string `json:"account"`
	ConfigDir    string `json:"config_dir"`
	ResolvedDir  string `json:"resolved_dir"`
	ConfigEnvSet bool   `json:"config_env_set"`
}

// PutExecutionAccount records an execution's identity once, before launch.
func (s *Store) PutExecutionAccount(ctx context.Context, executionID string, identity AgentAccountIdentity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO execution_accounts(execution_id,identity_json) VALUES (?,?)`, executionID, string(data))
	return err
}

// ExecutionAccount is nil for a legacy or non-Claude execution.
func (s *Store) ExecutionAccount(ctx context.Context, executionID string) (*AgentAccountIdentity, error) {
	return executionAccount(ctx, s.db, executionID)
}

func executionAccount(ctx context.Context, q sqlx.QueryerContext, executionID string) (*AgentAccountIdentity, error) {
	var data string
	err := sqlx.GetContext(ctx, q, &data, `SELECT identity_json FROM execution_accounts WHERE execution_id=?`, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var identity AgentAccountIdentity
	if err := json.Unmarshal([]byte(data), &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}
