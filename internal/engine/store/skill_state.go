package store

import "context"

// SkillStates is the enabled state the operator set for each skill, by
// folder name. A skill missing from it was never set.
func (s *Store) SkillStates(ctx context.Context) (map[string]bool, error) {
	return s.enabledStates(ctx, `SELECT name, enabled FROM skill_states`)
}

// SetSkillEnabled turns a skill on or off, by folder name.
func (s *Store) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	return s.setEnabled(ctx, `INSERT INTO skill_states (name, enabled) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET enabled = excluded.enabled, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, name, enabled)
}

// ManagedHookStates is the enabled state the operator set for each managed
// hook, by folder name. A hook missing from it was never set.
func (s *Store) ManagedHookStates(ctx context.Context) (map[string]bool, error) {
	return s.enabledStates(ctx, `SELECT name, enabled FROM managed_hook_states`)
}

// SetManagedHookEnabled turns a managed hook on or off for every agent, by
// folder name.
func (s *Store) SetManagedHookEnabled(ctx context.Context, name string, enabled bool) error {
	return s.setEnabled(ctx, `INSERT INTO managed_hook_states (name, enabled) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET enabled = excluded.enabled, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, name, enabled)
}

// enabledStates reads a name → enabled table with query.
func (s *Store) enabledStates(ctx context.Context, query string) (map[string]bool, error) {
	var rows []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	if err := s.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, err
	}
	states := make(map[string]bool, len(rows))
	for _, row := range rows {
		states[row.Name] = row.Enabled
	}
	return states, nil
}

func (s *Store) setEnabled(ctx context.Context, upsert, name string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, upsert, name, enabled)
	return err
}
