package store

import (
	"context"
	"database/sql"
	"errors"
)

// OAuthClientRegistration is a client Legato registered with an
// authorization server (DCR) for one resource and exact redirect. A
// confidential client's secret is sealed under the registration's ID.
type OAuthClientRegistration struct {
	ID          string `db:"id"`
	Issuer      string `db:"issuer"`
	Resource    string `db:"resource"`
	RedirectURI string `db:"redirect_uri"`
	ClientID    string `db:"client_id"`
	AuthMethod  string `db:"auth_method"`
	CreatedAt   int64  `db:"created_at"`
}

// SaveOAuthClientRegistration records r, replacing an earlier registration
// for the same issuer, resource and redirect. It returns the ID of the one
// replaced ("" if none), whose sealed secret the caller removes.
func (s *Store) SaveOAuthClientRegistration(ctx context.Context, r OAuthClientRegistration) (string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var replaced string
	err = tx.GetContext(ctx, &replaced, `SELECT id FROM oauth_client_registrations WHERE issuer=? AND resource=? AND redirect_uri=?`, r.Issuer, r.Resource, r.RedirectURI)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if replaced != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_client_registrations WHERE id=?`, replaced); err != nil {
			return "", err
		}
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO oauth_client_registrations(id,issuer,resource,redirect_uri,client_id,auth_method,created_at)
		VALUES(:id,:issuer,:resource,:redirect_uri,:client_id,:auth_method,:created_at)`, r); err != nil {
		return "", err
	}
	return replaced, tx.Commit()
}

// FindOAuthClientRegistration is the registration for exactly this issuer,
// resource and redirect, or ErrNotFound.
func (s *Store) FindOAuthClientRegistration(ctx context.Context, issuer, resource, redirect string) (OAuthClientRegistration, error) {
	var r OAuthClientRegistration
	err := s.db.GetContext(ctx, &r, `SELECT * FROM oauth_client_registrations WHERE issuer=? AND resource=? AND redirect_uri=?`, issuer, resource, redirect)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// OAuthClientRegistrations lists an issuer's registrations for a resource,
// newest first: a local receiver tries their ports again.
func (s *Store) OAuthClientRegistrations(ctx context.Context, issuer, resource string) ([]OAuthClientRegistration, error) {
	var all []OAuthClientRegistration
	err := s.db.SelectContext(ctx, &all, `SELECT * FROM oauth_client_registrations WHERE issuer=? AND resource=? ORDER BY created_at DESC, id`, issuer, resource)
	return all, err
}

// DeleteOAuthClientRegistration forgets a registration the server refused.
func (s *Store) DeleteOAuthClientRegistration(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_client_registrations WHERE id=?`, id)
	return err
}
