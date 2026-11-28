package store

import (
	"context"
	"time"
)

// NativeSecurityIncident is a refused native control call, deduplicated by
// execution and bridge generation. It outlives the pending request and session.
// Category describes the first refusal; no request body, URL or key is stored.
type NativeSecurityIncident struct {
	TaskID      string `db:"task_id"`
	ExecutionID string `db:"execution_id"`
	Generation  string `db:"generation"`
	Category    string `db:"category"`
	FirstSeenAt string `db:"first_seen_at"`
	LastSeenAt  string `db:"last_seen_at"`
	Count       int64  `db:"count"`
}

func (s *Store) RecordNativeSecurityIncident(ctx context.Context, taskID, executionID, generation, category string) error {
	switch category {
	case "permission", "question", "plan", "control":
	default:
		category = "unknown"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO native_security_incidents
 (task_id, execution_id, generation, category, first_seen_at, last_seen_at, count)
 VALUES (?, ?, ?, ?, ?, ?, 1)
 ON CONFLICT(execution_id, generation) DO UPDATE SET
 last_seen_at = excluded.last_seen_at, count = native_security_incidents.count + 1`,
		taskID, executionID, generation, category, now, now)
	return err
}

func (s *Store) ListNativeSecurityIncidents(ctx context.Context) ([]NativeSecurityIncident, error) {
	records := []NativeSecurityIncident{}
	err := s.db.SelectContext(ctx, &records, `SELECT * FROM native_security_incidents ORDER BY first_seen_at, execution_id, generation`)
	return records, err
}
