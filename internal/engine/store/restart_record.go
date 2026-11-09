package store

import (
	"context"
	"database/sql"
	"errors"
)

// Restart record states.
const (
	RestartPending    = "pending"
	RestartApplied    = "applied"
	RestartRolledBack = "rolled_back"
)

// RestartRecord is a restart of Legato that can be rolled back. Settings is
// JSON: the database settings it altered, each with its old value.
type RestartRecord struct {
	ID           string         `db:"id"`
	Reason       string         `db:"reason"`
	RequestedBy  string         `db:"requested_by"`
	Version      string         `db:"version"`
	ConfigPath   string         `db:"config_path"`
	ConfigBackup string         `db:"config_backup"`
	Settings     string         `db:"settings"`
	State        string         `db:"state"`
	Failure      string         `db:"failure"`
	CreatedAt    string         `db:"created_at"`
	FinishedAt   sql.NullString `db:"finished_at"`
}

// InsertRestartRecord keeps a pending restart.
func (s *Store) InsertRestartRecord(ctx context.Context, r RestartRecord) error {
	if r.Settings == "" {
		r.Settings = "[]"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO restart_records (id, reason, requested_by, version, config_path, config_backup, settings, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`, r.ID, r.Reason, r.RequestedBy, r.Version, r.ConfigPath, r.ConfigBackup, r.Settings)
	return err
}

// GetRestartRecord reads one restart; ErrNotFound when there is none.
func (s *Store) GetRestartRecord(ctx context.Context, id string) (RestartRecord, error) {
	var r RestartRecord
	err := s.db.GetContext(ctx, &r, `SELECT * FROM restart_records WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// PendingRestartRecord is the newest pending restart, if any.
func (s *Store) PendingRestartRecord(ctx context.Context) (*RestartRecord, error) {
	var r RestartRecord
	err := s.db.GetContext(ctx, &r, `SELECT * FROM restart_records WHERE state = 'pending' ORDER BY created_at DESC, id DESC LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// FinishRestartRecord ends a pending restart as applied or rolled back. It
// reports false when the record was no longer pending.
func (s *Store) FinishRestartRecord(ctx context.Context, id, state, failure string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE restart_records SET state = ?, failure = ?, finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND state = 'pending'`, state, failure, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListRestartRecords lists restarts, newest first, at most limit.
func (s *Store) ListRestartRecords(ctx context.Context, limit int) ([]RestartRecord, error) {
	records := []RestartRecord{}
	err := s.db.SelectContext(ctx, &records, `SELECT * FROM restart_records ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	return records, err
}
