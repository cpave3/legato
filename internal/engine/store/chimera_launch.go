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

// latestInSlot holds a binding to the newest session of its task and slot,
// so an older row of a reused task ID is never bound.
const latestInSlot = `id = (SELECT latest.id FROM agent_sessions latest WHERE latest.task_id = agent_sessions.task_id AND latest.slot = agent_sessions.slot ORDER BY latest.started_at DESC, latest.id DESC LIMIT 1)`

// BindChimeraDiscoveryIfUnbound binds an automatic discovery (a launch, a
// restart's recovery, the link restoring a lost binding) to a running row
// that has no binding. It prevents lockfile verification racing a native mod
// binding from replacing it with a legacy endpoint, and it never binds a row
// the operator switched to its terminal (native_off), even one switched after
// the caller last read it.
func (s *Store) BindChimeraDiscoveryIfUnbound(ctx context.Context, id int, sessionID, endpoint string) (bool, error) {
	return s.bindIfUnbound(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ? WHERE id = ? AND status = 'running' AND native_off = 0 AND chimera_session_id IS NULL AND chimera_endpoint IS NULL
		AND `+latestInSlot, sessionID, endpoint, id)
}

// UpgradeChimeraDiscoveryIfUnbound is BindChimeraDiscoveryIfUnbound for the
// operator's own upgrade, which ends a switch to the terminal.
func (s *Store) UpgradeChimeraDiscoveryIfUnbound(ctx context.Context, id int, sessionID, endpoint string) (bool, error) {
	return s.bindIfUnbound(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ?, native_off = 0 WHERE id = ? AND status = 'running' AND chimera_session_id IS NULL AND chimera_endpoint IS NULL
		AND `+latestInSlot, sessionID, endpoint, id)
}

// BindNativeEndpointUnlessOff binds an automatically discovered bridge to the
// exact running row, replacing a stale binding, unless the operator switched
// the row to its terminal (native_off). It reports whether it bound.
func (s *Store) BindNativeEndpointUnlessOff(ctx context.Context, id int, sessionID, endpoint string) (bool, error) {
	return s.bindIfUnbound(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ? WHERE id = ? AND status = 'running' AND native_off = 0`, sessionID, endpoint, id)
}

func (s *Store) bindIfUnbound(ctx context.Context, query string, args ...any) (bool, error) {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// How a running agent row's native binding stands for one Chimera session.
const (
	NativeBindingBound     = "bound"     // bound to that session and endpoint
	NativeBindingUnbound   = "unbound"   // bound to nothing; it may be restored
	NativeBindingOff       = "off"       // the operator switched it to its terminal
	NativeBindingElsewhere = "elsewhere" // bound to another session or endpoint
	NativeBindingStale     = "stale"     // ended, missing, or no longer its task's newest
)

// ChimeraBindingState says how row id's binding stands for the given Chimera
// session and endpoint, in the terms BindChimeraDiscoveryIfUnbound decides
// by. It writes nothing and says nothing of another binding's endpoint.
func (s *Store) ChimeraBindingState(ctx context.Context, id int, sessionID, endpoint string) (string, error) {
	var row struct {
		Status    string  `db:"status"`
		NativeOff bool    `db:"native_off"`
		SessionID *string `db:"chimera_session_id"`
		Endpoint  *string `db:"chimera_endpoint"`
		Latest    bool    `db:"latest"`
	}
	err := s.db.GetContext(ctx, &row, `SELECT status, native_off, chimera_session_id, chimera_endpoint, `+latestInSlot+` AS latest FROM agent_sessions WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeBindingStale, nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case row.Status != "running" || !row.Latest:
		return NativeBindingStale, nil
	case row.SessionID != nil && row.Endpoint != nil && *row.SessionID == sessionID && *row.Endpoint == endpoint:
		return NativeBindingBound, nil
	case row.SessionID != nil || row.Endpoint != nil:
		return NativeBindingElsewhere, nil
	case row.NativeOff:
		return NativeBindingOff, nil
	}
	return NativeBindingUnbound, nil
}
