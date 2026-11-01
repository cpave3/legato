package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// IntegrationToken is a bearer token an integration presents to the web
// API, with the scopes it may use. Only the token's hash is kept.
type IntegrationToken struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	TokenHash  string   `json:"-"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
}

type integrationTokenRow struct {
	ID         string  `db:"id"`
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
	return IntegrationToken{ID: r.ID, Name: r.Name, Scopes: scopes, TokenHash: r.TokenHash, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt}
}

// InsertIntegrationToken stores a token. A name already held by a token that
// is not revoked is ErrConflict.
func (s *Store) InsertIntegrationToken(ctx context.Context, t IntegrationToken) error {
	scopes, err := json.Marshal(t.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO integration_tokens (id, name, scopes, token_hash) VALUES (?, ?, ?, ?)`,
		t.ID, t.Name, string(scopes), t.TokenHash)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
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

// ListIntegrationTokens lists every token, revoked ones too, oldest first.
func (s *Store) ListIntegrationTokens(ctx context.Context) ([]IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM integration_tokens ORDER BY created_at, rowid`); err != nil {
		return nil, err
	}
	tokens := make([]IntegrationToken, 0, len(rows))
	for _, r := range rows {
		tokens = append(tokens, r.token())
	}
	return tokens, nil
}

// RevokeIntegrationToken revokes the live token of a name.
func (s *Store) RevokeIntegrationToken(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE name = ? AND revoked_at IS NULL`, name)
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

// LatestAgentSessionForTask is a task's newest session row, running or
// ended; nil when it has none.
func (s *Store) LatestAgentSessionForTask(ctx context.Context, taskID string) (*AgentSession, error) {
	var a AgentSession
	err := s.db.GetContext(ctx, &a, agentSessionSelect+" WHERE a.task_id = ? ORDER BY a.started_at DESC, a.id DESC LIMIT 1", taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
