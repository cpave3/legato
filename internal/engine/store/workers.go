package store

import (
	"context"
	"database/sql"
	"errors"
)

// Worker is a worker instance a main paired with, by the worker's instance
// ID. The token is not here: TokenSecret names it in the sealed secret store.
type Worker struct {
	InstanceID     string  `db:"instance_id"`
	Address        string  `db:"address"`
	Name           string  `db:"name"`
	MainName       string  `db:"main_name"`
	CACertificate  string  `db:"ca_certificate"`
	TokenSecret    string  `db:"token_secret"`
	Sandbox        bool    `db:"sandbox"`
	HandoffVersion int     `db:"handoff_version"`
	PairedAt       string  `db:"paired_at"`
	LastPullAt     *string `db:"last_pull_at"`
}

// SaveWorker stores a worker, replacing the one of its instance ID: pairing
// the same worker again keeps one record. LastPullAt is kept.
func (s *Store) SaveWorker(ctx context.Context, w Worker) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workers (instance_id, address, name, main_name, ca_certificate, token_secret, sandbox, handoff_version, paired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET address = excluded.address, name = excluded.name, main_name = excluded.main_name,
			ca_certificate = excluded.ca_certificate, token_secret = excluded.token_secret, sandbox = excluded.sandbox,
			handoff_version = excluded.handoff_version, paired_at = excluded.paired_at`,
		w.InstanceID, w.Address, w.Name, w.MainName, w.CACertificate, w.TokenSecret, w.Sandbox, w.HandoffVersion, w.PairedAt)
	return err
}

// ListWorkers lists the paired workers, in the order they were paired.
func (s *Store) ListWorkers(ctx context.Context) ([]Worker, error) {
	var out []Worker
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM workers ORDER BY paired_at, rowid`)
	return out, err
}

// GetWorker is the worker of an instance ID, or ErrNotFound.
func (s *Store) GetWorker(ctx context.Context, instanceID string) (*Worker, error) {
	var w Worker
	err := s.db.GetContext(ctx, &w, `SELECT * FROM workers WHERE instance_id = ?`, instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}
