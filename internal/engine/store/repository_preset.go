package store

import (
	"context"
	"strings"
)

// RepositoryPreset is a repository the person added in the web UI.
type RepositoryPreset struct {
	ID         int64  `db:"id"`
	Name       string `db:"name"`
	Repo       string `db:"repo"`        // owner/name on GitHub; "" for a folder only
	PrimaryDir string `db:"primary_dir"` // a checkout on this machine; "" for none
	BaseBranch string `db:"base_branch"`
}

// SaveRepositoryPreset adds a repository. A name already taken is ErrConflict.
func (s *Store) SaveRepositoryPreset(ctx context.Context, p RepositoryPreset) (RepositoryPreset, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO repository_presets (name, repo, primary_dir, base_branch) VALUES (?, ?, ?, ?)`,
		p.Name, p.Repo, p.PrimaryDir, p.BaseBranch)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return RepositoryPreset{}, ErrConflict
		}
		return RepositoryPreset{}, err
	}
	p.ID, err = result.LastInsertId()
	return p, err
}

// ListRepositoryPresets lists the repositories added, oldest first.
func (s *Store) ListRepositoryPresets(ctx context.Context) ([]RepositoryPreset, error) {
	presets := []RepositoryPreset{}
	err := s.db.SelectContext(ctx, &presets, `SELECT id, name, repo, primary_dir, base_branch FROM repository_presets ORDER BY id`)
	return presets, err
}

// DeleteRepositoryPreset removes one; a missing one is ErrNotFound.
func (s *Store) DeleteRepositoryPreset(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM repository_presets WHERE id = ?`, id)
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
