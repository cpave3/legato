package store

import "context"

// ClaimHandoffNotice records that the phone is told of notice (a state of
// an away task or a worker, named by its producer's source key and
// revision) at at. It says whether this call recorded it: only that caller
// sends the notification, so two Legato processes on one database send it
// once, and a restart does not send it again.
func (s *Store) ClaimHandoffNotice(ctx context.Context, notice, at string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO handoff_notices (notice, sent_at) VALUES (?, ?) ON CONFLICT(notice) DO NOTHING`, notice, at)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
