package store

import "context"

// DirectoryPreset is a directory the person saved to start agents in.
type DirectoryPreset struct {
	Path      string `db:"path" json:"path"`
	Name      string `db:"name" json:"name"`
	CreatedAt string `db:"created_at" json:"created_at"`
}

// SaveDirectoryPreset keeps a directory preset. Saving a path that is already
// kept renames it and leaves its place in the list.
func (s *Store) SaveDirectoryPreset(ctx context.Context, preset DirectoryPreset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO directory_presets (path, name) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET name = excluded.name`,
		preset.Path, preset.Name)
	return err
}

// ListDirectoryPresets returns the saved directory presets, oldest first.
func (s *Store) ListDirectoryPresets(ctx context.Context) ([]DirectoryPreset, error) {
	var presets []DirectoryPreset
	err := s.db.SelectContext(ctx, &presets, `SELECT path, name, created_at FROM directory_presets ORDER BY rowid`)
	return presets, err
}
