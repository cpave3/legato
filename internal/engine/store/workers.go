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
	// LastPullAt is when the main last heard from the worker on its
	// connection: a pull, a summary row or a pong.
	LastPullAt *string `db:"last_pull_at"`
	// UnreachableSince is when the connection dropped, once it has been
	// down longer than the grace (or at once when a handoff call got no
	// answer); nil while the worker is reachable.
	UnreachableSince *string `db:"unreachable_since"`
	// RefusedAt is when the worker refused this main's token: it no longer
	// accepts this main. Pairing again clears it.
	RefusedAt *string `db:"refused_at"`
}

// SaveWorker stores a worker, replacing the one of its instance ID: pairing
// the same worker again keeps one record. LastPullAt is kept.
func (s *Store) SaveWorker(ctx context.Context, w Worker) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workers (instance_id, address, name, main_name, ca_certificate, token_secret, sandbox, handoff_version, paired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET address = excluded.address, name = excluded.name, main_name = excluded.main_name,
			ca_certificate = excluded.ca_certificate, token_secret = excluded.token_secret, sandbox = excluded.sandbox,
			handoff_version = excluded.handoff_version, paired_at = excluded.paired_at, refused_at = NULL`,
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

// SetWorkerHeard records that the main heard from a worker at at: its last
// pull is then, and it is neither unreachable nor refusing this main.
func (s *Store) SetWorkerHeard(ctx context.Context, instanceID, at string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workers SET last_pull_at = ?, unreachable_since = NULL, refused_at = NULL WHERE instance_id = ?`, at, instanceID)
	return err
}

// SetWorkerUnreachable marks a worker unreachable since since, unless it is
// marked already (the first time it went down stands). It says whether
// the mark is new.
func (s *Store) SetWorkerUnreachable(ctx context.Context, instanceID, since string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE workers SET unreachable_since = ? WHERE instance_id = ? AND unreachable_since IS NULL`, since, instanceID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// SetWorkerRefused records that a worker refused this main's token at at,
// unless it is recorded already. A worker that answers is reachable. It
// says whether the record is new.
func (s *Store) SetWorkerRefused(ctx context.Context, instanceID, at string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE workers SET refused_at = COALESCE(refused_at, ?), unreachable_since = NULL
		WHERE instance_id = ? AND (refused_at IS NULL OR unreachable_since IS NOT NULL)`, at, instanceID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
