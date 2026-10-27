package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TaskNativeSession is the latest observed provider conversation for a task.
// Ownership survives removal of the owning agent session and its live bindings.
type TaskNativeSession struct {
	TaskID            string    `db:"task_id"`
	AgentKind         string    `db:"agent_kind"`
	ProviderSessionID string    `db:"provider_session_id"`
	CWD               string    `db:"cwd"`
	StorageDir        string    `db:"storage_dir"`
	AgentSessionID    int       `db:"agent_session_id"`
	ExecutionID       string    `db:"execution_id"`
	UpdatedAt         time.Time `db:"updated_at"`
}

// GetTaskNativeSession returns nil, nil when no conversation has been recorded.
func (s *Store) GetTaskNativeSession(ctx context.Context, taskID, agentKind string) (*TaskNativeSession, error) {
	var session TaskNativeSession
	if err := s.db.GetContext(ctx, &session, `SELECT * FROM task_native_sessions WHERE task_id = ? AND agent_kind = ?`, taskID, agentKind); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

// ListTaskNativeSessions returns saved conversations ordered by provider kind.
func (s *Store) ListTaskNativeSessions(ctx context.Context, taskID string) ([]TaskNativeSession, error) {
	sessions := make([]TaskNativeSession, 0)
	err := s.db.SelectContext(ctx, &sessions, `SELECT * FROM task_native_sessions WHERE task_id = ? ORDER BY agent_kind`, taskID)
	return sessions, err
}

// RecordTaskNativeSession atomically checks ownership and records a conversation.
// The owner must be the newest session row (started_at, then ID), still running
// with its exact running execution. A newer stopped row also invalidates it.
// ErrConflict identifies a stale, mismatched, or child owner. An ephemeral
// task keeps its conversation too, so reloading its agent can resume it. Invalid
// input returns a descriptive error. An identical record returns false, nil and
// leaves UpdatedAt unchanged; ownership changes count as record changes.
func (s *Store) RecordTaskNativeSession(ctx context.Context, taskID string, agentSessionID int, executionID, agentKind, providerSessionID, cwd, storageDir string) (bool, error) {
	if agentKind != "claude-code" && agentKind != "codex" {
		return false, fmt.Errorf("unsupported native session agent kind %q", agentKind)
	}
	if strings.TrimSpace(taskID) == "" || agentSessionID <= 0 || strings.TrimSpace(executionID) == "" || strings.TrimSpace(providerSessionID) == "" || strings.TrimSpace(cwd) == "" {
		return false, fmt.Errorf("native session requires task ID, agent session ID, execution ID, provider session ID, and cwd")
	}
	if providerSessionID == taskID {
		return false, fmt.Errorf("native provider session ID must be the actual conversation ID, not task ID %q", taskID)
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var owns bool
	err = tx.GetContext(ctx, &owns, `SELECT EXISTS (
		SELECT 1 FROM agent_sessions a
		JOIN tasks t ON t.id = a.task_id
		JOIN executions e ON e.agent_session_id = a.id
		WHERE t.id = ?
			AND a.id = ? AND a.status = 'running' AND a.agent_kind = ?
			AND a.id = (SELECT id FROM agent_sessions WHERE task_id = t.id ORDER BY started_at DESC, id DESC LIMIT 1)
			AND (COALESCE(a.parent_task_id, '') = '' OR (a.role = 'conductor' AND a.parent_task_id = t.id))
			AND COALESCE(a.subtask_id, '') = ''
			AND a.role != 'worker'
			AND e.id = ? AND e.status = 'running' AND e.agent_kind = a.agent_kind
			AND e.task_id = t.id AND e.execution_task_id = t.id
			AND e.kind != 'swarm_worker' AND e.role != 'worker'
			AND COALESCE(e.swarm_subtask_id, '') = ''
	)`, taskID, agentSessionID, agentKind, executionID)
	if err != nil {
		return false, err
	}
	if !owns {
		return false, fmt.Errorf("record native session for task %q: session %d / execution %q / kind %q is not the latest running top-level owner of a persistent task: %w", taskID, agentSessionID, executionID, agentKind, ErrConflict)
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO task_native_sessions (task_id, agent_kind, provider_session_id, cwd, storage_dir, agent_session_id, execution_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, agent_kind) DO UPDATE SET
			provider_session_id = excluded.provider_session_id, cwd = excluded.cwd,
			storage_dir = excluded.storage_dir,
			agent_session_id = excluded.agent_session_id, execution_id = excluded.execution_id,
			updated_at = excluded.updated_at
		WHERE task_native_sessions.provider_session_id != excluded.provider_session_id
			OR task_native_sessions.cwd != excluded.cwd
			OR task_native_sessions.storage_dir != excluded.storage_dir
			OR task_native_sessions.agent_session_id != excluded.agent_session_id
			OR task_native_sessions.execution_id != excluded.execution_id`,
		taskID, agentKind, providerSessionID, cwd, storageDir, agentSessionID, executionID, formatExecutionTime(time.Now()))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return rows != 0, nil
}
