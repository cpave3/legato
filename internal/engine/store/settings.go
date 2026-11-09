package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SettingRow is a setting's value kept in the database, as JSON.
type SettingRow struct {
	Key       string `db:"key"`
	Value     string `db:"value"`
	UpdatedAt string `db:"updated_at"`
	UpdatedBy string `db:"updated_by"`
}

// ListSettings lists every setting kept in the database.
func (s *Store) ListSettings(ctx context.Context) ([]SettingRow, error) {
	rows := []SettingRow{}
	err := s.db.SelectContext(ctx, &rows, `SELECT key, value, updated_at, updated_by FROM settings ORDER BY key`)
	return rows, err
}

// SettingValues is every setting kept in the database, by key.
func (s *Store) SettingValues(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	values := make(map[string]json.RawMessage, len(rows))
	for _, r := range rows {
		values[r.Key] = json.RawMessage(r.Value)
	}
	return values, nil
}

// GetSetting reads one setting; ok is false when the database has none.
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var value string
	err := s.db.GetContext(ctx, &value, `SELECT value FROM settings WHERE key = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(value), true, nil
}

// SetSetting keeps a setting's value.
func (s *Store) SetSetting(ctx context.Context, key string, value json.RawMessage, by string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_by) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		key, string(value), by)
	return err
}

// DeleteSetting removes a setting's value, so its default applies.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

// SettingEntry is one entry of a list of named entries.
type SettingEntry struct {
	List      string `db:"list"`
	Name      string `db:"name"`
	Value     string `db:"value"`
	UpdatedAt string `db:"updated_at"`
	UpdatedBy string `db:"updated_by"`
}

// ListSettingEntries lists every entry kept, by list and name.
func (s *Store) ListSettingEntries(ctx context.Context) ([]SettingEntry, error) {
	rows := []SettingEntry{}
	err := s.db.SelectContext(ctx, &rows, `SELECT list, name, value, updated_at, updated_by FROM setting_entries ORDER BY list, rowid`)
	return rows, err
}

// SettingEntries is every entry kept, by list and name.
func (s *Store) SettingEntries(ctx context.Context) (map[string]map[string]json.RawMessage, error) {
	rows, err := s.ListSettingEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]json.RawMessage{}
	for _, r := range rows {
		if out[r.List] == nil {
			out[r.List] = map[string]json.RawMessage{}
		}
		out[r.List][r.Name] = json.RawMessage(r.Value)
	}
	return out, nil
}

// SetSettingEntry keeps one entry, replacing any of the same name.
func (s *Store) SetSettingEntry(ctx context.Context, list, name string, value json.RawMessage, by string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO setting_entries (list, name, value, updated_by) VALUES (?, ?, ?, ?)
		ON CONFLICT(list, name) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		list, name, string(value), by)
	return err
}

// DeleteSettingEntry removes one entry; none is ErrNotFound.
func (s *Store) DeleteSettingEntry(ctx context.Context, list, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM setting_entries WHERE list = ? AND name = ?`, list, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
