package store

import "context"

// SkillStates is the enabled state the operator set for each skill, by
// folder name. A skill missing from it was never set.
func (s *Store) SkillStates(ctx context.Context) (map[string]bool, error) {
	var rows []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT name, enabled FROM skill_states`); err != nil {
		return nil, err
	}
	states := make(map[string]bool, len(rows))
	for _, row := range rows {
		states[row.Name] = row.Enabled
	}
	return states, nil
}

// SetSkillEnabled turns a skill on or off, by folder name.
func (s *Store) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO skill_states (name, enabled) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET enabled = excluded.enabled, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		name, enabled)
	return err
}
