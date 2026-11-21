package store

import (
	"context"
	"time"
)

// UnrecordedExecution is an ended run of a top-level agent that the
// session history has no conversation of and has not looked for one of.
type UnrecordedExecution struct {
	ID         string     `db:"id"`
	TaskID     string     `db:"task_id"`
	TaskTitle  string     `db:"task_title"`
	AgentKind  string     `db:"agent_kind"`
	Status     string     `db:"status"`
	WorkingDir string     `db:"working_dir"`
	StartedAt  time.Time  `db:"started_at"`
	EndedAt    *time.Time `db:"ended_at"`
}

// ListUnrecordedExecutions returns up to limit ended interactive and
// conductor executions of tools in kinds, started since, in a folder, with
// no run in the history and not yet looked at, the newest first.
func (s *Store) ListUnrecordedExecutions(ctx context.Context, since time.Time, limit int) ([]UnrecordedExecution, error) {
	var out []UnrecordedExecution
	err := s.db.SelectContext(ctx, &out, `
		SELECT e.id, COALESCE(NULLIF(e.execution_task_id, ''), e.task_id, '') AS task_id, e.task_title,
			e.agent_kind, e.status, e.working_dir, e.started_at, e.ended_at
		FROM executions e
		WHERE e.status != 'running' AND e.kind IN ('interactive', 'swarm_conductor')
			AND e.agent_kind IN ('claude-code', 'codex') AND e.working_dir != ''
			AND e.started_at >= ?
			AND NOT EXISTS (SELECT 1 FROM agent_conversation_runs r WHERE r.execution_id = e.id)
			AND NOT EXISTS (SELECT 1 FROM agent_conversation_scans sc WHERE sc.execution_id = e.id)
		ORDER BY e.started_at DESC LIMIT ?`, formatExecutionTime(since), limit)
	return out, err
}

// ConversationStorageDirs are the storage folders the history has seen
// conversations of a tool kept in, the default ("") among them.
func (s *Store) ConversationStorageDirs(ctx context.Context, agentKind string) ([]string, error) {
	var dirs []string
	err := s.db.SelectContext(ctx, &dirs, `SELECT DISTINCT storage_dir FROM agent_conversations WHERE agent_kind = ?
		UNION SELECT '' ORDER BY 1`, agentKind)
	return dirs, err
}

// RecoveredConversation is a transcript found by folder and time for a run
// that never reported a conversation.
type RecoveredConversation struct {
	AgentKind         string
	ProviderSessionID string
	StorageDir        string
	CWD               string
	TranscriptPath    string
	FirstMessage      string
	LastMessage       string
	StartedAt         time.Time
}

// RecordRecoveredConversation keeps a recovered conversation with a run on
// the execution, ended as the execution ended. One already recorded (by any
// storage folder) is left alone and false returned; one already recovered
// gains the run.
func (s *Store) RecordRecoveredConversation(ctx context.Context, execution UnrecordedExecution, c RecoveredConversation) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existing []struct {
		ID     int64  `db:"id"`
		Source string `db:"source"`
	}
	if err := tx.SelectContext(ctx, &existing, `SELECT id, source FROM agent_conversations
		WHERE agent_kind = ? AND provider_session_id = ? ORDER BY id LIMIT 1`, c.AgentKind, c.ProviderSessionID); err != nil {
		return false, err
	}
	now := formatExecutionTime(time.Now())
	var id int64
	switch {
	case len(existing) > 0 && existing[0].Source != ConversationRecovered:
		return false, tx.Commit()
	case len(existing) > 0:
		id = existing[0].ID
	default:
		type taskRow struct {
			Title       string `db:"title"`
			Ephemeral   bool   `db:"ephemeral"`
			Kind        string `db:"kind"`
			WorkspaceID *int   `db:"workspace_id"`
		}
		var task taskRow
		var tasks []taskRow
		if err := tx.SelectContext(ctx, &tasks, `SELECT title, ephemeral, COALESCE(kind, '') AS kind, workspace_id FROM tasks WHERE id = ?`, execution.TaskID); err != nil {
			return false, err
		}
		task.Title = execution.TaskTitle
		if len(tasks) > 0 {
			task = tasks[0]
		}
		var clone struct {
			Repo   string `db:"repo"`
			Branch string `db:"branch"`
		}
		if err := tx.GetContext(ctx, &clone, `SELECT COALESCE(MAX(repo), '') AS repo, COALESCE(MAX(branch), '') AS branch
			FROM job_clones WHERE path = ?`, c.CWD); err != nil {
			return false, err
		}
		seen := formatExecutionTime(c.StartedAt)
		if execution.EndedAt != nil {
			seen = formatExecutionTime(*execution.EndedAt)
		}
		if err := tx.GetContext(ctx, &id, `
			INSERT INTO agent_conversations (
				agent_kind, provider_session_id, storage_dir, task_id, slot, task_title, ephemeral, reeve,
				cwd, repo, branch, source, first_message, last_message, first_seen_at, last_seen_at,
				transcript_path, transcript_state, transcript_checked_at, workspace_id)
			VALUES (?, ?, ?, ?, 'main', ?, ?, ?, ?, ?, ?, 'recovered', ?, ?, ?, ?, ?, 'present', ?, ?)
			RETURNING id`,
			c.AgentKind, c.ProviderSessionID, c.StorageDir, execution.TaskID, task.Title, task.Ephemeral, task.Kind == TaskKindReeve,
			c.CWD, clone.Repo, clone.Branch, c.FirstMessage, c.LastMessage, formatExecutionTime(c.StartedAt), seen,
			c.TranscriptPath, now, task.WorkspaceID); err != nil {
			return false, err
		}
		if err := writeConversationFTS(ctx, tx, id); err != nil {
			return false, err
		}
	}
	var ended any
	if execution.EndedAt != nil {
		ended = formatExecutionTime(*execution.EndedAt)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agent_conversation_runs (conversation_id, execution_id, started_at, ended_at, end_reason)
		VALUES (?, ?, ?, ?, ?)`, id, execution.ID, formatExecutionTime(execution.StartedAt), ended, runEndForStatus(execution.Status)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// MarkExecutionScanned records that recovery has looked at an execution.
func (s *Store) MarkExecutionScanned(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO agent_conversation_scans (execution_id, scanned_at) VALUES (?, ?)`,
		executionID, formatExecutionTime(time.Now()))
	return err
}
