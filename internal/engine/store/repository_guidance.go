package store

import "context"

// RepositoryGuidance is guidance the person saved in the web UI for the
// agents that work in a repository.
type RepositoryGuidance struct {
	Repo string `db:"repo"` // host/owner/name
	Text string `db:"text"`
}

// SaveRepositoryGuidance sets a repository's guidance, replacing any it had.
func (s *Store) SaveRepositoryGuidance(ctx context.Context, g RepositoryGuidance) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO repository_guidance (repo, text) VALUES (?, ?)
		ON CONFLICT(repo) DO UPDATE SET text = excluded.text, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		g.Repo, g.Text)
	return err
}

// ListRepositoryGuidance lists the saved guidance, oldest first.
func (s *Store) ListRepositoryGuidance(ctx context.Context) ([]RepositoryGuidance, error) {
	guidance := []RepositoryGuidance{}
	err := s.db.SelectContext(ctx, &guidance, `SELECT repo, text FROM repository_guidance ORDER BY created_at, repo`)
	return guidance, err
}

// DeleteRepositoryGuidance removes a repository's guidance; none is ErrNotFound.
func (s *Store) DeleteRepositoryGuidance(ctx context.Context, repo string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM repository_guidance WHERE repo = ?`, repo)
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
