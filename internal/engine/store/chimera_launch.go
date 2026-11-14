package store

import "context"

// BindChimeraDiscoveryIfUnbound prevents lockfile verification racing a native
// mod binding from replacing it with a legacy endpoint.
func (s *Store) BindChimeraDiscoveryIfUnbound(ctx context.Context, id int, sessionID, endpoint string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET chimera_session_id = ?, chimera_endpoint = ?, native_off = 0 WHERE id = ? AND status = 'running' AND chimera_session_id IS NULL AND chimera_endpoint IS NULL
		AND id = (SELECT latest.id FROM agent_sessions latest WHERE latest.task_id = agent_sessions.task_id AND latest.slot = agent_sessions.slot ORDER BY latest.started_at DESC, latest.id DESC LIMIT 1)`, sessionID, endpoint, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
