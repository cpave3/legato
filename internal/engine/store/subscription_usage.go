package store

import "context"

// SubscriptionUsageRow is a provider's newest subscription usage reading,
// kept as the service's JSON.
type SubscriptionUsageRow struct {
	Provider  string `db:"provider"`
	Reading   string `db:"reading"`
	UpdatedAt string `db:"updated_at"`
}

// ListSubscriptionUsage lists each provider's newest reading, by provider.
func (s *Store) ListSubscriptionUsage(ctx context.Context) ([]SubscriptionUsageRow, error) {
	rows := []SubscriptionUsageRow{}
	err := s.db.SelectContext(ctx, &rows, `SELECT provider, reading, updated_at FROM subscription_usage ORDER BY provider`)
	return rows, err
}

// PutSubscriptionUsage keeps a provider's reading in place of the last.
func (s *Store) PutSubscriptionUsage(ctx context.Context, provider, reading string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO subscription_usage (provider, reading) VALUES (?, ?)
		ON CONFLICT(provider) DO UPDATE SET reading = excluded.reading, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		provider, reading)
	return err
}
