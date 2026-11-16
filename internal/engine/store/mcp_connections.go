package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jmoiron/sqlx"
)

// MCPConnection is an instance-owned upstream MCP server: where it is and how
// it is reached, never its credentials. Revision changes with every edit and is
// never reused for a name, so work begun against one revision can be fenced.
type MCPConnection struct {
	Name     string `db:"name" json:"name"`
	URL      string `db:"url" json:"url"`
	Issuer   string `db:"issuer" json:"issuer,omitempty"`
	Networks string `db:"networks" json:"-"`
	Auth     string `db:"auth" json:"auth"`
	// StaticClientID and StaticRedirectURI are an advanced, pre-registered
	// client; its secret is sealed, and StaticSecretSet says there is one.
	StaticClientID    string `db:"static_client_id" json:"static_client_id,omitempty"`
	StaticRedirectURI string `db:"static_redirect_uri" json:"static_redirect_uri,omitempty"`
	StaticSecretSet   bool   `db:"static_secret_set" json:"static_client_secret_set,omitempty"`
	RulesJSON         string `db:"rules_json" json:"-"`
	ReadOnlyTools     string `db:"read_only_tools" json:"-"`
	Enabled           bool   `db:"enabled" json:"enabled"`
	InstanceID        string `db:"instance_id" json:"-"`
	Revision          int64  `db:"revision" json:"revision"`
	LastContactAt     int64  `db:"last_contact_at" json:"-"`
	LastError         string `db:"last_error" json:"-"`
	CreatedAt         int64  `db:"created_at" json:"-"`
	UpdatedAt         int64  `db:"updated_at" json:"-"`
}

func nextMCPRevision(ctx context.Context, tx *sqlx.Tx, name string) (int64, error) {
	var next int64
	err := tx.GetContext(ctx, &next, `INSERT INTO mcp_connection_revisions(name,last) VALUES(?,1) ON CONFLICT(name) DO UPDATE SET last=last+1 RETURNING last`, name)
	return next, err
}

// CreateMCPConnection adds an enabled connection; ErrConflict if the name is taken.
func (s *Store) CreateMCPConnection(ctx context.Context, c MCPConnection, now int64) (MCPConnection, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return MCPConnection{}, err
	}
	defer tx.Rollback()
	var taken int
	if err := tx.GetContext(ctx, &taken, `SELECT COUNT(*) FROM mcp_connections WHERE name=?`, c.Name); err != nil {
		return MCPConnection{}, err
	}
	if taken != 0 {
		return MCPConnection{}, ErrConflict
	}
	if c.Revision, err = nextMCPRevision(ctx, tx, c.Name); err != nil {
		return MCPConnection{}, err
	}
	c.Enabled, c.CreatedAt, c.UpdatedAt, c.LastContactAt, c.LastError = true, now, now, 0, ""
	_, err = tx.NamedExecContext(ctx, `INSERT INTO mcp_connections(name,url,issuer,networks,auth,static_client_id,static_redirect_uri,static_secret_set,rules_json,read_only_tools,enabled,instance_id,revision,last_contact_at,last_error,created_at,updated_at)
		VALUES(:name,:url,:issuer,:networks,:auth,:static_client_id,:static_redirect_uri,:static_secret_set,:rules_json,:read_only_tools,:enabled,:instance_id,:revision,:last_contact_at,:last_error,:created_at,:updated_at)`, c)
	if err != nil {
		return MCPConnection{}, err
	}
	return c, tx.Commit()
}

func (s *Store) GetMCPConnection(ctx context.Context, name string) (MCPConnection, error) {
	var c MCPConnection
	err := s.db.GetContext(ctx, &c, `SELECT * FROM mcp_connections WHERE name=?`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPConnection{}, ErrNotFound
	}
	return c, err
}

func (s *Store) ListMCPConnections(ctx context.Context) ([]MCPConnection, error) {
	var all []MCPConnection
	err := s.db.SelectContext(ctx, &all, `SELECT * FROM mcp_connections ORDER BY name`)
	return all, err
}

// UpdateMCPConnection saves c if c.Revision is still current (ErrConflict
// otherwise) and advances the revision. Changing the resource, issuer, auth
// mode or pre-registered client invalidates the connection's OAuth authorization and every unfinished
// attempt; disabling only ends unfinished attempts. The fence shares the
// update's transaction. It returns the attempts whose sealed records the
// caller must clean.
func (s *Store) UpdateMCPConnection(ctx context.Context, c MCPConnection, now int64) (MCPConnection, []OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return MCPConnection{}, nil, err
	}
	defer tx.Rollback()
	var old MCPConnection
	err = tx.GetContext(ctx, &old, `SELECT * FROM mcp_connections WHERE name=?`, c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPConnection{}, nil, ErrNotFound
	}
	if err != nil {
		return MCPConnection{}, nil, err
	}
	if old.Revision != c.Revision {
		return MCPConnection{}, nil, ErrConflict
	}
	var fenced []OAuthAttempt
	switch {
	case old.URL != c.URL || old.Issuer != c.Issuer || old.Auth != c.Auth || old.StaticClientID != c.StaticClientID || old.StaticRedirectURI != c.StaticRedirectURI:
		if fenced, err = invalidateOAuthConnectionTx(ctx, tx, c.Name, now); err != nil {
			return MCPConnection{}, nil, err
		}
		c.LastContactAt, c.LastError = 0, ""
	case old.Enabled && !c.Enabled:
		if fenced, err = endUnfinishedOAuthAttemptsTx(ctx, tx, c.Name, now); err != nil {
			return MCPConnection{}, nil, err
		}
		c.LastContactAt, c.LastError = old.LastContactAt, old.LastError
	default:
		c.LastContactAt, c.LastError = old.LastContactAt, old.LastError
	}
	if c.Revision, err = nextMCPRevision(ctx, tx, c.Name); err != nil {
		return MCPConnection{}, nil, err
	}
	c.InstanceID, c.CreatedAt, c.UpdatedAt = old.InstanceID, old.CreatedAt, now
	_, err = tx.NamedExecContext(ctx, `UPDATE mcp_connections SET url=:url,issuer=:issuer,networks=:networks,auth=:auth,static_client_id=:static_client_id,static_redirect_uri=:static_redirect_uri,static_secret_set=:static_secret_set,rules_json=:rules_json,read_only_tools=:read_only_tools,enabled=:enabled,revision=:revision,
		last_contact_at=:last_contact_at,last_error=:last_error,updated_at=:updated_at WHERE name=:name`, c)
	if err != nil {
		return MCPConnection{}, nil, err
	}
	return c, fenced, tx.Commit()
}

// DeleteMCPConnection removes a connection and, in the same transaction,
// invalidates its OAuth authorization and attempts. It returns the attempts
// whose sealed records the caller must clean.
func (s *Store) DeleteMCPConnection(ctx context.Context, name string, now int64) ([]OAuthAttempt, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM mcp_connections WHERE name=?`, name)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, ErrNotFound
	}
	fenced, err := invalidateOAuthConnectionTx(ctx, tx, name, now)
	if err != nil {
		return nil, err
	}
	return fenced, tx.Commit()
}

// RecordMCPContact records the outcome of reaching the upstream, with a fixed
// error code ("" for success), only if revision is still current.
func (s *Store) RecordMCPContact(ctx context.Context, name string, revision int64, code string, now int64) error {
	code = strings.TrimSpace(code)
	result, err := s.db.ExecContext(ctx, `UPDATE mcp_connections SET last_contact_at=CASE WHEN ?='' THEN ? ELSE last_contact_at END,last_error=? WHERE name=? AND revision=?`,
		code, now, code, name, revision)
	return oauthTransitionResult(result, err)
}

func endUnfinishedOAuthAttemptsTx(ctx context.Context, tx *sqlx.Tx, connection string, now int64) ([]OAuthAttempt, error) {
	var attempts []OAuthAttempt
	if err := tx.SelectContext(ctx, &attempts, `SELECT * FROM oauth_attempts WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation') ORDER BY created_at,id`, connection); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_attempts SET status='cancelled',error_code='connection_disabled',updated_at=? WHERE connection=? AND status IN ('pending','exchanging','awaiting_confirmation')`, now, connection); err != nil {
		return nil, err
	}
	return attempts, nil
}
