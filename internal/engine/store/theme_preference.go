package store

import (
	"context"
	"database/sql"
	"errors"
)

// ThemePreference is this server's preference, not a client's appearance.
type ThemePreference struct {
	ID       string `db:"id" json:"id"`
	Revision uint64 `db:"revision" json:"revision"`
}

func (s *Store) ThemePreference(ctx context.Context) (ThemePreference, error) {
	var value ThemePreference
	err := s.db.GetContext(ctx, &value, `SELECT id, revision FROM server_theme_preference WHERE singleton = 1`)
	return value, err
}

// ChangeThemePreference atomically checks the observed revision and commits
// the preference. A conflict cannot overwrite another process's choice.
func (s *Store) ChangeThemePreference(ctx context.Context, expected uint64, id, actor string) (ThemePreference, error) {
	var value ThemePreference
	err := s.db.GetContext(ctx, &value, `UPDATE server_theme_preference
 SET id = ?, revision = revision + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_by = ?
 WHERE singleton = 1 AND revision = ? RETURNING id, revision`, id, actor, expected)
	if errors.Is(err, sql.ErrNoRows) {
		return ThemePreference{}, ErrConflict
	}
	return value, err
}
