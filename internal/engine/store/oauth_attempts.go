package store

import (
	"context"
	"database/sql"
	"errors"
)

// OAuthAttempt contains delivery and activation authority, never credentials.
// The state and receiver capability are represented only by hashes.
type OAuthAttempt struct {
	ID           string `db:"id"`
	Connection   string `db:"connection"`
	Generation   int64  `db:"generation"`
	Owner        string `db:"owner"`
	InstanceID   string `db:"instance_id"`
	StateHash    string `db:"state_hash"`
	ReceiverHash string `db:"receiver_hash"`
	Status       string `db:"status"`
	ErrorCode    string `db:"error_code"`
	ExpiresAt    int64  `db:"expires_at"`
	CreatedAt    int64  `db:"created_at"`
	UpdatedAt    int64  `db:"updated_at"`
}

func (s *Store) BeginOAuthAttempt(ctx context.Context, a OAuthAttempt, now int64) (OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return OAuthAttempt{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_connections(connection) VALUES(?) ON CONFLICT(connection) DO NOTHING`, a.Connection); err != nil {
		return OAuthAttempt{}, err
	}
	if err = tx.GetContext(ctx, &a.Generation, `SELECT generation FROM oauth_connections WHERE connection=?`, a.Connection); err != nil {
		return OAuthAttempt{}, err
	}
	a.Status, a.CreatedAt, a.UpdatedAt = "pending", now, now
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_attempts(id,connection,generation,owner,instance_id,state_hash,receiver_hash,status,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Connection, a.Generation, a.Owner, a.InstanceID, a.StateHash, a.ReceiverHash, a.Status, a.ExpiresAt, now, now)
	if err != nil {
		return OAuthAttempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return OAuthAttempt{}, err
	}
	return a, nil
}

func (s *Store) GetOAuthAttempt(ctx context.Context, id string) (OAuthAttempt, error) {
	var a OAuthAttempt
	err := s.db.GetContext(ctx, &a, `SELECT * FROM oauth_attempts WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthAttempt{}, ErrNotFound
	}
	return a, err
}

func (s *Store) ActiveOAuthAttempt(ctx context.Context, connection string) (OAuthAttempt, error) {
	var a OAuthAttempt
	err := s.db.GetContext(ctx, &a, `SELECT a.* FROM oauth_attempts a JOIN oauth_connections c ON c.active_attempt=a.id WHERE c.connection=? AND a.status='authorized'`, connection)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthAttempt{}, ErrNotFound
	}
	return a, err
}

// ClaimOAuthAttempt is the single exchange gate, shared by web processes.
func (s *Store) ClaimOAuthAttempt(ctx context.Context, id string, now int64) error {
	return s.transitionOAuthAttempt(ctx, id, "pending", "exchanging", now)
}

func (s *Store) StageOAuthAttempt(ctx context.Context, id string, now int64) error {
	return s.transitionOAuthAttempt(ctx, id, "exchanging", "awaiting_confirmation", now)
}

func (s *Store) transitionOAuthAttempt(ctx context.Context, id, from, to string, now int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status=?,updated_at=? WHERE id=? AND status=? AND expires_at>? AND generation=(SELECT generation FROM oauth_connections WHERE connection=oauth_attempts.connection)`, to, now, id, from, now)
	return oauthTransitionResult(result, err)
}

func oauthTransitionResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// ActivateOAuthAttempt requires a successful exchange and the same operator.
// Advancing the generation also invalidates every competing unfinished flow.
func (s *Store) ActivateOAuthAttempt(ctx context.Context, id, owner string, now int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var a OAuthAttempt
	err = tx.GetContext(ctx, &a, `SELECT * FROM oauth_attempts WHERE id=? AND owner=? AND status='awaiting_confirmation' AND expires_at>?`, id, owner, now)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE oauth_connections SET generation=generation+1,active_attempt=? WHERE connection=? AND generation=?`, id, a.Connection, a.Generation)
	if err = oauthTransitionResult(result, err); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='superseded',updated_at=? WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation','authorized')`, now, a.Connection); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='authorized',updated_at=? WHERE id=?`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// InvalidateOAuthConnection fences pending exchanges and confirmation before
// callers revoke tokens or remove sealed records. Its cleanup snapshot shares
// that transaction, so attempts begun after the fence are not accidentally removed.
func (s *Store) InvalidateOAuthConnection(ctx context.Context, connection string, now int64) ([]OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_connections(connection) VALUES(?) ON CONFLICT(connection) DO NOTHING`, connection); err != nil {
		return nil, err
	}
	var attempts []OAuthAttempt
	if err := tx.SelectContext(ctx, &attempts, `SELECT * FROM oauth_attempts WHERE connection=? ORDER BY created_at,id`, connection); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_connections SET generation=generation+1,active_attempt='' WHERE connection=?`, connection); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='cancelled',updated_at=? WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation','authorized')`, now, connection); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempts, nil
}

// EndOAuthAttempt terminalizes only unfinished work. It cannot revoke an
// authorization that another process already confirmed.
func (s *Store) EndOAuthAttempt(ctx context.Context, id, owner, status, code string, now int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status=?,error_code=?,updated_at=? WHERE id=? AND owner=? AND status IN ('pending','exchanging','awaiting_confirmation')`, status, code, now, id, owner)
	return oauthTransitionResult(result, err)
}

// ListOAuthAttempts identifies the sealed records to clean after invalidation.
func (s *Store) ListOAuthAttempts(ctx context.Context, connection string) ([]OAuthAttempt, error) {
	var attempts []OAuthAttempt
	err := s.db.SelectContext(ctx, &attempts, `SELECT * FROM oauth_attempts WHERE connection=? ORDER BY created_at,id`, connection)
	return attempts, err
}
