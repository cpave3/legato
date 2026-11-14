package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
)

// SetChimeraLaunchSecret keeps launch authentication outside public session records.
func (s *Store) SetChimeraLaunchSecret(ctx context.Context, id int, secret, path string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chimera_launch_secrets (agent_session_id, secret, file_path) VALUES (?, ?, ?)`, id, secret, path)
	return err
}

// GetChimeraLaunchSecret returns private authentication for discovery and recovery.
// Sessions launched before authentication was introduced have empty values.
func (s *Store) GetChimeraLaunchSecret(ctx context.Context, id int) (string, string, error) {
	var record struct {
		Secret string `db:"secret"`
		Path   string `db:"file_path"`
	}
	err := s.db.GetContext(ctx, &record, `SELECT secret, file_path FROM chimera_launch_secrets WHERE agent_session_id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return record.Secret, record.Path, err
}

// RemoveChimeraLaunchSecretFile removes the bootstrap file after binding. The
// stored secret remains available for rediscovery if the native endpoint dies.
func (s *Store) RemoveChimeraLaunchSecretFile(ctx context.Context, id int) error {
	_, path, err := s.GetChimeraLaunchSecret(ctx, id)
	if err != nil || path == "" {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// BindChimeraDiscoveryIfUnbound prevents lockfile verification racing a native
// mod binding from replacing it with a legacy endpoint.
func (s *Store) BindChimeraDiscoveryIfUnbound(ctx context.Context, id int, sessionID, endpoint string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ?, native_off = 0 WHERE id = ? AND status = 'running' AND chimera_session_id IS NULL AND chimera_endpoint IS NULL
		AND id = (SELECT latest.id FROM agent_sessions latest WHERE latest.task_id = agent_sessions.task_id AND latest.slot = agent_sessions.slot ORDER BY latest.started_at DESC, latest.id DESC LIMIT 1)`, sessionID, endpoint, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
