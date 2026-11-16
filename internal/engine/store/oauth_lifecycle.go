package store

import (
	"context"
	"database/sql"
	"errors"
)

// ClaimOAuthRefresh gives holder the right to refresh connection's tokens
// until until, if attempt is still its active authorization and no other
// holder's lease is live. ErrConflict otherwise.
func (s *Store) ClaimOAuthRefresh(ctx context.Context, connection, attempt, holder string, now, until int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE oauth_connections SET refresh_holder=?,refresh_lease_until=?
		WHERE connection=? AND active_attempt=? AND (refresh_holder='' OR refresh_holder=? OR refresh_lease_until<=?)
		AND EXISTS (SELECT 1 FROM oauth_attempts WHERE id=? AND status='authorized')`, holder, until, connection, attempt, holder, now, attempt)
	return oauthTransitionResult(result, err)
}

// ReleaseOAuthRefresh ends holder's lease, if it still has it.
func (s *Store) ReleaseOAuthRefresh(ctx context.Context, connection, holder string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_connections SET refresh_holder='',refresh_lease_until=0 WHERE connection=? AND refresh_holder=?`, connection, holder)
	return err
}

// CurrentOAuthAttempt is the attempt a connection points at, whatever its
// status (authorized, or reauth_required), or ErrNotFound.
func (s *Store) CurrentOAuthAttempt(ctx context.Context, connection string) (OAuthAttempt, error) {
	var a OAuthAttempt
	err := s.db.GetContext(ctx, &a, `SELECT a.* FROM oauth_attempts a JOIN oauth_connections c ON c.active_attempt=a.id WHERE c.connection=?`, connection)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthAttempt{}, ErrNotFound
	}
	return a, err
}

// RequireOAuthReauthorization marks the active authorization dead (its
// refresh token was refused or it expired with none): the operator must
// connect again. ErrConflict if attempt is no longer the active one.
func (s *Store) RequireOAuthReauthorization(ctx context.Context, connection, attempt, code string, now int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status='reauth_required',error_code=?,updated_at=?
		WHERE id=? AND status='authorized' AND id=(SELECT active_attempt FROM oauth_connections WHERE connection=?)`, code, now, attempt, connection)
	return oauthTransitionResult(result, err)
}

// RenewOAuthReceiverLeases extends owner's receiver leases on attempts still pending.
func (s *Store) RenewOAuthReceiverLeases(ctx context.Context, owner string, until int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET receiver_lease_until=? WHERE receiver_owner=? AND status='pending'`, until, owner)
	return err
}

// OAuthSweep bounds what the sweep does.
type OAuthSweep struct {
	Now int64
	// StuckBefore: an exchange claimed before this was cut short by a crash.
	StuckBefore int64
	// RetainBefore: a finished attempt last changed before this is deleted.
	RetainBefore int64
}

// SweepOAuthAttempts settles abandoned attempts (expired, an exchange a
// crash interrupted, a web receiver whose process is gone) and deletes
// finished ones past retention, keeping any a connection still points at.
// It returns every finished attempt, whose sealed records the caller removes.
func (s *Store) SweepOAuthAttempts(ctx context.Context, sw OAuthSweep) ([]string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`UPDATE oauth_attempts SET status='expired',error_code='authorization_expired',updated_at=? WHERE status IN ('pending','exchanging','awaiting_confirmation') AND expires_at<=?`, []any{sw.Now, sw.Now}},
		{`UPDATE oauth_attempts SET status='failed',error_code='exchange_interrupted',updated_at=? WHERE status='exchanging' AND updated_at<?`, []any{sw.Now, sw.StuckBefore}},
		{`UPDATE oauth_attempts SET status='interrupted',error_code='receiver_lost',updated_at=? WHERE status='pending' AND receiver_owner<>'' AND receiver_lease_until<?`, []any{sw.Now, sw.Now}},
		{`DELETE FROM oauth_attempts WHERE status NOT IN ('pending','exchanging','awaiting_confirmation','authorized') AND updated_at<?
			AND id NOT IN (SELECT active_attempt FROM oauth_connections)`, []any{sw.RetainBefore}},
	} {
		if _, err := tx.ExecContext(ctx, q.query, q.args...); err != nil {
			return nil, err
		}
	}
	var finished []string
	if err := tx.SelectContext(ctx, &finished, `SELECT id FROM oauth_attempts WHERE status NOT IN ('pending','exchanging','awaiting_confirmation','authorized') ORDER BY id`); err != nil {
		return nil, err
	}
	return finished, tx.Commit()
}
