package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Kinds of integration token: Argus's (and any other integration's, made with
// the CLI) and a paired main instance's.
const (
	IntegrationKindArgus = "argus"
	IntegrationKindMain  = "main"
)

// IntegrationToken is a bearer token an integration presents to the web
// API, with the scopes it may use. Only the token's hash is kept. A main
// instance's token (Kind main) carries the main's instance ID.
type IntegrationToken struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	InstanceID string   `json:"instance_id,omitempty"`
	Scopes     []string `json:"scopes"`
	TokenHash  string   `json:"-"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
}

type integrationTokenRow struct {
	ID         string  `db:"id"`
	Kind       string  `db:"kind"`
	InstanceID string  `db:"instance_id"`
	Name       string  `db:"name"`
	Scopes     string  `db:"scopes"`
	TokenHash  string  `db:"token_hash"`
	CreatedAt  string  `db:"created_at"`
	LastUsedAt *string `db:"last_used_at"`
	RevokedAt  *string `db:"revoked_at"`
}

func (r integrationTokenRow) token() IntegrationToken {
	var scopes []string
	_ = json.Unmarshal([]byte(r.Scopes), &scopes)
	return IntegrationToken{ID: r.ID, Kind: r.Kind, InstanceID: r.InstanceID, Name: r.Name, Scopes: scopes, TokenHash: r.TokenHash, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt}
}

// InsertIntegrationToken stores a token, of kind argus when it names none. A
// name already held by a live token of its kind is ErrConflict.
func (s *Store) InsertIntegrationToken(ctx context.Context, t IntegrationToken) error {
	return insertIntegrationToken(ctx, s.db, t)
}

type integrationExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertIntegrationToken(ctx context.Context, db integrationExecer, t IntegrationToken) error {
	scopes, err := json.Marshal(t.Scopes)
	if err != nil {
		return err
	}
	kind := t.Kind
	if kind == "" {
		kind = IntegrationKindArgus
	}
	_, err = db.ExecContext(ctx, `INSERT INTO integration_tokens (id, kind, name, instance_id, scopes, token_hash) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, kind, t.Name, t.InstanceID, string(scopes), t.TokenHash)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

// ReplaceMainIntegrationToken stores a main instance's token and revokes the
// one its instance held, in one transaction. Another live main with the
// same name is ErrConflict, and nothing changes.
func (s *Store) ReplaceMainIntegrationToken(ctx context.Context, t IntegrationToken) error {
	t.Kind = IntegrationKindMain
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a committed transaction ignores it
	if _, err := tx.ExecContext(ctx, `UPDATE integration_tokens SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE kind = 'main' AND instance_id = ? AND revoked_at IS NULL`, t.InstanceID); err != nil {
		return err
	}
	if err := insertIntegrationToken(ctx, tx, t); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMainIntegrationTokens lists the live tokens of paired main instances,
// oldest first.
func (s *Store) ListMainIntegrationTokens(ctx context.Context) ([]IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM integration_tokens WHERE kind = 'main' AND revoked_at IS NULL ORDER BY created_at, rowid`); err != nil {
		return nil, err
	}
	tokens := make([]IntegrationToken, 0, len(rows))
	for _, r := range rows {
		tokens = append(tokens, r.token())
	}
	return tokens, nil
}

// RevokeMainIntegrationToken revokes a main instance's live token.
func (s *Store) RevokeMainIntegrationToken(ctx context.Context, instanceID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE kind = 'main' AND instance_id = ? AND revoked_at IS NULL`, instanceID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// IntegrationTokenByHash returns the token, not revoked, whose hash this is.
func (s *Store) IntegrationTokenByHash(ctx context.Context, hash string) (*IntegrationToken, error) {
	var row integrationTokenRow
	err := s.db.GetContext(ctx, &row, `SELECT * FROM integration_tokens WHERE token_hash = ? AND revoked_at IS NULL`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t := row.token()
	return &t, nil
}

// ListIntegrationTokens lists every token of kind argus, revoked ones too,
// oldest first. Main instances' tokens are listed by
// ListMainIntegrationTokens.
func (s *Store) ListIntegrationTokens(ctx context.Context) ([]IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM integration_tokens WHERE kind = 'argus' ORDER BY created_at, rowid`); err != nil {
		return nil, err
	}
	tokens := make([]IntegrationToken, 0, len(rows))
	for _, r := range rows {
		tokens = append(tokens, r.token())
	}
	return tokens, nil
}

// RevokeIntegrationToken revokes the live argus-kind token of a name.
func (s *Store) RevokeIntegrationToken(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE kind = 'argus' AND name = ? AND revoked_at IS NULL`, name)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchIntegrationToken records that a token was used, at most once a
// minute.
func (s *Store) TouchIntegrationToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 seconds'))`, id)
	return err
}

// ListTasksForRepository lists the tasks, not archived, whose code context or
// linked PR names a repository (owner/name, any case).
func (s *Store) ListTasksForRepository(ctx context.Context, repo string) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks, `SELECT * FROM tasks WHERE archived_at IS NULL AND (
		lower(json_extract(code_context, '$.repo')) = lower(?) OR lower(json_extract(pr_meta, '$.repo')) = lower(?)) ORDER BY id`, repo, repo)
	return tasks, err
}

// LatestAgentSessionForTask is the newest session row of a task's main slot, running or
// ended; nil when it has none.
func (s *Store) LatestAgentSessionForTask(ctx context.Context, taskID string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a, agentSessionSelect+" WHERE a.task_id = ? AND a.slot = 'main' ORDER BY a.started_at DESC, a.id DESC LIMIT 1", taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
