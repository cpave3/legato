package store

import (
	"context"
	"database/sql"
)

// UsageProfileSwitch records a committed change of effective defaults.
type UsageProfileSwitch struct {
	ID int64 `db:"id" json:"id"`
	// Scope is the lower-cased baseline profile of the repositories whose
	// defaults switched; empty for the fallback (global) scope.
	Scope  string `db:"scope" json:"scope"`
	From   string `db:"from_profile" json:"from_profile"`
	To     string `db:"to_profile" json:"to_profile"`
	Since  string `db:"since" json:"since"`
	Reason string `db:"reason" json:"reason"`
}

func (s *Store) ActiveUsageProfile(ctx context.Context) (string, error) {
	var state string
	err := s.db.GetContext(ctx, &state, `SELECT state FROM active_usage_profile WHERE id=1`)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return state, err
}

// SaveUsageProfile atomically keeps state and its optional switch. Only the
// process holding usage.lock calls this method. History is bounded to 200 rows.
func (s *Store) SaveUsageProfile(ctx context.Context, state string, change *UsageProfileSwitch) error {
	changes := []*UsageProfileSwitch{}
	if change != nil {
		changes = append(changes, change)
	}
	return s.SaveUsageProfileChanges(ctx, state, changes)
}

// SaveUsageProfileChanges keeps state and every scope's switch in one
// transaction, so readers never see some scopes moved and others not.
func (s *Store) SaveUsageProfileChanges(ctx context.Context, state string, changes []*UsageProfileSwitch) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO active_usage_profile(id,state) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state`, state); err != nil {
		return err
	}
	for _, change := range changes {
		result, err := tx.ExecContext(ctx, `INSERT INTO usage_profile_switches(scope,from_profile,to_profile,since,reason) VALUES(?,?,?,?,?)`, change.Scope, change.From, change.To, change.Since, change.Reason)
		if err != nil {
			return err
		}
		change.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}
	if len(changes) > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM usage_profile_switches WHERE id NOT IN (SELECT id FROM usage_profile_switches ORDER BY id DESC LIMIT 200)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UsageProfileSwitches(ctx context.Context, limit int) ([]UsageProfileSwitch, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	out := []UsageProfileSwitch{}
	err := s.db.SelectContext(ctx, &out, `SELECT id,scope,from_profile,to_profile,since,reason FROM usage_profile_switches ORDER BY id DESC LIMIT ?`, limit)
	return out, err
}
