package store

import (
	"context"
	"strings"
)

// Integration attention states.
const (
	IntegrationAttentionOpen     = "open"
	IntegrationAttentionResolved = "resolved"
)

// IntegrationAttention is an Attention item an integration pushed: one per
// case of Argus's.
type IntegrationAttention struct {
	Integration      string `db:"integration"`
	Key              string `db:"key"`
	Revision         string `db:"revision"`
	Signal           string `db:"signal"`
	Title            string `db:"title"`
	Reason           string `db:"reason"`
	Detail           string `db:"detail"`
	Urgency          string `db:"urgency"`
	Target           string `db:"target"` // JSON {repo, pr, branch, sha}
	Link             string `db:"link"`
	State            string `db:"state"`
	ResolvedRevision string `db:"resolved_revision"`
	ResolvedReason   string `db:"resolved_reason"`
	CreatedAt        string `db:"created_at"`
	UpdatedAt        string `db:"updated_at"`
}

// UpsertIntegrationAttention stores an item as open, unless it was resolved
// at this same revision: a retry that lands after the resolve changes
// nothing. It returns the item as stored.
func (s *Store) UpsertIntegrationAttention(ctx context.Context, in IntegrationAttention) (*IntegrationAttention, error) {
	if in.Target == "" {
		in.Target = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO integration_attention
			(integration, key, revision, signal, title, reason, detail, urgency, target, link)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(integration, key) DO UPDATE SET
			revision = excluded.revision, signal = excluded.signal, title = excluded.title,
			reason = excluded.reason, detail = excluded.detail, urgency = excluded.urgency,
			target = excluded.target, link = excluded.link, state = 'open',
			resolved_revision = '', resolved_reason = '',
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE NOT (integration_attention.state = 'resolved' AND integration_attention.resolved_revision = excluded.revision)`,
		in.Integration, in.Key, in.Revision, in.Signal, in.Title, in.Reason, in.Detail, in.Urgency, in.Target, in.Link)
	if err != nil {
		return nil, err
	}
	return s.GetIntegrationAttention(ctx, in.Integration, in.Key)
}

func (s *Store) GetIntegrationAttention(ctx context.Context, integration, key string) (*IntegrationAttention, error) {
	var out IntegrationAttention
	if err := s.db.GetContext(ctx, &out, `SELECT * FROM integration_attention WHERE integration = ? AND key = ?`, integration, key); err != nil {
		return nil, notFound(err)
	}
	return &out, nil
}

// ResolveIntegrationAttention resolves an item and records the revision it
// was resolved at. ErrNotFound when the integration has no such item.
func (s *Store) ResolveIntegrationAttention(ctx context.Context, integration, key, revision, reason string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE integration_attention
		SET state = 'resolved', resolved_revision = ?, resolved_reason = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE integration = ? AND key = ?`, revision, reason, integration, key))
}

// ListOpenIntegrationAttention lists an integration's open items, oldest
// first.
func (s *Store) ListOpenIntegrationAttention(ctx context.Context, integration string) ([]IntegrationAttention, error) {
	var out []IntegrationAttention
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM integration_attention
		WHERE integration = ? AND state = 'open' ORDER BY created_at, key`, integration)
	return out, err
}

// ResolveIntegrationAttentionExcept resolves every open item of the
// integration whose key is not in keys, at its current revision, and says
// how many it resolved.
func (s *Store) ResolveIntegrationAttentionExcept(ctx context.Context, integration string, keys []string, reason string) (int, error) {
	query := `UPDATE integration_attention
		SET state = 'resolved', resolved_revision = revision, resolved_reason = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE integration = ? AND state = 'open'`
	args := []any{reason, integration}
	if len(keys) > 0 {
		query += ` AND key NOT IN (?` + strings.Repeat(`, ?`, len(keys)-1) + `)`
		for _, key := range keys {
			args = append(args, key)
		}
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
