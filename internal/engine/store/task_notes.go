package store

import "context"

func (s *Store) InsertTaskNote(ctx context.Context, note TaskNote) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO task_notes (id, task_id, body, source, remote_id) VALUES (:id, :task_id, :body, :source, :remote_id)`, note)
	return err
}

func (s *Store) ListTaskNotes(ctx context.Context, taskID string) ([]TaskNote, error) {
	var notes []TaskNote
	err := s.db.SelectContext(ctx, &notes, `SELECT * FROM task_notes WHERE task_id = ? ORDER BY created_at, id`, taskID)
	return notes, err
}
