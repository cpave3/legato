package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
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
	Scope        string `db:"scope"`
	// ReceiverOwner and ReceiverLeaseUntil: see migration 114.
	ReceiverOwner      string `db:"receiver_owner"`
	ReceiverLeaseUntil int64  `db:"receiver_lease_until"`
	ExpiresAt          int64  `db:"expires_at"`
	CreatedAt          int64  `db:"created_at"`
	UpdatedAt          int64  `db:"updated_at"`
	// ConnectionRevision, when set, is the MCP connection revision the attempt
	// was begun against: recording it fails unless that revision is current and
	// the connection is enabled for OAuth. It is not stored on the attempt.
	ConnectionRevision int64 `db:"-"`
}

func (s *Store) BeginOAuthAttempt(ctx context.Context, a OAuthAttempt, now int64) (OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return OAuthAttempt{}, err
	}
	defer tx.Rollback()
	if a.ConnectionRevision != 0 {
		var current int
		err := tx.GetContext(ctx, &current, `SELECT COUNT(*) FROM mcp_connections WHERE name=? AND revision=? AND enabled=1 AND auth='oauth'`, a.Connection, a.ConnectionRevision)
		if err != nil {
			return OAuthAttempt{}, err
		}
		if current != 1 {
			return OAuthAttempt{}, ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_connections(connection) VALUES(?) ON CONFLICT(connection) DO NOTHING`, a.Connection); err != nil {
		return OAuthAttempt{}, err
	}
	if err = tx.GetContext(ctx, &a.Generation, `SELECT generation FROM oauth_connections WHERE connection=?`, a.Connection); err != nil {
		return OAuthAttempt{}, err
	}
	a.Status, a.CreatedAt, a.UpdatedAt = "pending", now, now
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_attempts(id,connection,generation,owner,instance_id,state_hash,receiver_hash,status,scope,receiver_owner,receiver_lease_until,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Connection, a.Generation, a.Owner, a.InstanceID, a.StateHash, a.ReceiverHash, a.Status, a.Scope, a.ReceiverOwner, a.ReceiverLeaseUntil, a.ExpiresAt, now, now)
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
	attempts, err := invalidateOAuthConnectionTx(ctx, tx, connection, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempts, nil
}

func invalidateOAuthConnectionTx(ctx context.Context, tx *sqlx.Tx, connection string, now int64) ([]OAuthAttempt, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_connections(connection) VALUES(?) ON CONFLICT(connection) DO NOTHING`, connection); err != nil {
		return nil, err
	}
	var attempts []OAuthAttempt
	if err := tx.SelectContext(ctx, &attempts, `SELECT * FROM oauth_attempts WHERE connection=? ORDER BY created_at,id`, connection); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_connections SET generation=generation+1,active_attempt='' WHERE connection=?`, connection); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='cancelled',updated_at=? WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation','authorized')`, now, connection); err != nil {
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

// AdoptOAuthAuthorization makes a, an authorization obtained outside an
// attempt (the Burnlite board's own connect flow, or one moved over from the
// board's old storage, LEG-544), the connection's active authorization: in
// one transaction it supersedes every unfinished attempt and the old
// authorization, as ActivateOAuthAttempt does, and records a as authorized.
// It returns the attempts it superseded, whose sealed records the caller
// removes. With a.ConnectionRevision set it fails with ErrConflict unless that
// revision is current and the connection is enabled for OAuth.
func (s *Store) AdoptOAuthAuthorization(ctx context.Context, a OAuthAttempt, now int64) ([]OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if a.ConnectionRevision != 0 {
		var current int
		err := tx.GetContext(ctx, &current, `SELECT COUNT(*) FROM mcp_connections WHERE name=? AND revision=? AND enabled=1 AND auth='oauth'`, a.Connection, a.ConnectionRevision)
		if err != nil {
			return nil, err
		}
		if current != 1 {
			return nil, ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_connections(connection) VALUES(?) ON CONFLICT(connection) DO NOTHING`, a.Connection); err != nil {
		return nil, err
	}
	var superseded []OAuthAttempt
	if err = tx.SelectContext(ctx, &superseded, `SELECT * FROM oauth_attempts WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation','authorized') ORDER BY created_at,id`, a.Connection); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='superseded',updated_at=? WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation','authorized')`, now, a.Connection); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, &a.Generation, `UPDATE oauth_connections SET generation=generation+1,active_attempt=? WHERE connection=? RETURNING generation`, a.ID, a.Connection); err != nil {
		return nil, err
	}
	a.Status, a.CreatedAt, a.UpdatedAt = "authorized", now, now
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_attempts(id,connection,generation,owner,instance_id,state_hash,receiver_hash,status,scope,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Connection, a.Generation, a.Owner, a.InstanceID, a.StateHash, a.ReceiverHash, a.Status, a.Scope, a.ExpiresAt, now, now)
	if err != nil {
		return nil, err
	}
	return superseded, tx.Commit()
}

// BackupTo writes a consistent copy of the database to path, which must not
// exist (SQLite's VACUUM INTO).
func (s *Store) BackupTo(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// ClaimBurnliteMigration gives holder the Burnlite migration until until,
// unless another holder's lease is live (ErrConflict).
func (s *Store) ClaimBurnliteMigration(ctx context.Context, holder string, now, until int64) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO burnlite_migration_lease(id) VALUES(1) ON CONFLICT(id) DO NOTHING`); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE burnlite_migration_lease SET holder=?,lease_until=? WHERE id=1 AND (holder='' OR holder=? OR lease_until<=?)`, holder, until, holder, now)
	return oauthTransitionResult(result, err)
}

// ReleaseBurnliteMigration ends holder's lease, if it still has it.
func (s *Store) ReleaseBurnliteMigration(ctx context.Context, holder string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE burnlite_migration_lease SET holder='',lease_until=0 WHERE id=1 AND holder=?`, holder)
	return err
}
