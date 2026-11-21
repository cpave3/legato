package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Where a conversation in the session history came from.
const (
	// ConversationRecorded is one an agent's bridge or hooks reported.
	ConversationRecorded = "recorded"
	// ConversationRecovered is one found by folder and time for a run that
	// never reported one.
	ConversationRecovered = "recovered"
)

// Whether a conversation's transcript was found where its tool keeps it.
const (
	TranscriptPresent = "present"
	TranscriptMissing = "missing"
	TranscriptUnknown = "unknown"
)

// Why a run of a conversation ended. A live run has none.
const (
	RunExited      = "exited"      // the agent ended on its own
	RunStopped     = "stopped"     // the operator stopped it, or its task was deleted or archived
	RunLost        = "lost"        // a reconcile found its session gone
	RunInterrupted = "interrupted" // the startup sweep found it taken by a crash
	RunDismissed   = "dismissed"   // an interrupted run the operator dismissed
	RunReplaced    = "replaced"    // the run went on in another conversation (/clear, /resume)
)

// AgentConversation is one provider conversation of one agent tool, named by
// its session ID: an entry of the session history. It outlives its task.
type AgentConversation struct {
	ID                  int64      `db:"id"`
	AgentKind           string     `db:"agent_kind"`
	ProviderSessionID   string     `db:"provider_session_id"`
	StorageDir          string     `db:"storage_dir"`
	TaskID              string     `db:"task_id"`
	Slot                string     `db:"slot"`
	TaskTitle           string     `db:"task_title"`
	Ephemeral           bool       `db:"ephemeral"`
	Reeve               bool       `db:"reeve"`
	CWD                 string     `db:"cwd"`
	Repo                string     `db:"repo"`
	Branch              string     `db:"branch"`
	Source              string     `db:"source"`
	FirstMessage        string     `db:"first_message"`
	LastMessage         string     `db:"last_message"`
	LastMessageAt       *time.Time `db:"last_message_at"`
	FirstSeenAt         time.Time  `db:"first_seen_at"`
	LastSeenAt          time.Time  `db:"last_seen_at"`
	TranscriptPath      string     `db:"transcript_path"`
	TranscriptState     string     `db:"transcript_state"`
	TranscriptCheckedAt *time.Time `db:"transcript_checked_at"`
	// WorkspaceID is the task's workspace when it was last recorded.
	WorkspaceID *int `db:"workspace_id"`
}

// AgentConversationRun is one execution a conversation was live in.
type AgentConversationRun struct {
	ConversationID int64      `db:"conversation_id"`
	ExecutionID    string     `db:"execution_id"`
	StartedAt      time.Time  `db:"started_at"`
	EndedAt        *time.Time `db:"ended_at"`
	EndReason      *string    `db:"end_reason"`
	// Sandboxed is whether the run's agent ran in Legato's sandbox; nil
	// when it is not known.
	Sandboxed *bool `db:"sandboxed"`
}

// conversationIdentity is what recording a conversation needs.
type conversationIdentity struct {
	agentKind, providerSessionID, storageDir string
	taskID, slot, cwd, executionID           string
}

// recordConversation keeps a reported conversation in the session history,
// in the caller's transaction: the entry upserted with its task's newest
// title and the live job clone at its folder, its FTS row rewritten, and a
// live run for the execution. Another conversation of the same tool still
// live in that execution has been replaced.
func recordConversation(ctx context.Context, tx *sqlx.Tx, c conversationIdentity) error {
	var task struct {
		Title       string `db:"title"`
		Ephemeral   bool   `db:"ephemeral"`
		Kind        string `db:"kind"`
		WorkspaceID *int   `db:"workspace_id"`
	}
	if err := tx.GetContext(ctx, &task, `SELECT title, ephemeral, COALESCE(kind, '') AS kind, workspace_id FROM tasks WHERE id = ?`, c.taskID); err != nil {
		return fmt.Errorf("conversation's task %q: %w", c.taskID, err)
	}
	cwd := c.cwd
	if cwd == "" && c.executionID != "" {
		if err := tx.GetContext(ctx, &cwd, `SELECT COALESCE((SELECT working_dir FROM executions WHERE id = ?), '')`, c.executionID); err != nil {
			return err
		}
	}
	var clone struct {
		Repo   string `db:"repo"`
		Branch string `db:"branch"`
	}
	if cwd != "" {
		if err := tx.GetContext(ctx, &clone, `SELECT COALESCE(MAX(repo), '') AS repo, COALESCE(MAX(branch), '') AS branch
			FROM job_clones WHERE path = ? AND removed_at IS NULL`, cwd); err != nil {
			return err
		}
	}
	stamp := formatExecutionTime(time.Now())
	var id int64
	if err := tx.GetContext(ctx, &id, `
		INSERT INTO agent_conversations (
			agent_kind, provider_session_id, storage_dir, task_id, slot, task_title,
			ephemeral, reeve, cwd, repo, branch, source, first_seen_at, last_seen_at, workspace_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (agent_kind, storage_dir, provider_session_id) DO UPDATE SET
			task_id = excluded.task_id, slot = excluded.slot, task_title = excluded.task_title,
			ephemeral = excluded.ephemeral, reeve = excluded.reeve, workspace_id = excluded.workspace_id,
			cwd = CASE WHEN excluded.cwd = '' THEN agent_conversations.cwd ELSE excluded.cwd END,
			repo = CASE WHEN excluded.cwd = '' THEN agent_conversations.repo ELSE excluded.repo END,
			branch = CASE WHEN excluded.cwd = '' THEN agent_conversations.branch ELSE excluded.branch END,
			source = 'recorded', last_seen_at = excluded.last_seen_at
		RETURNING id`,
		c.agentKind, c.providerSessionID, c.storageDir, c.taskID, slotOrMain(c.slot), task.Title,
		task.Ephemeral, task.Kind == TaskKindReeve, cwd, clone.Repo, clone.Branch, ConversationRecorded, stamp, stamp, task.WorkspaceID); err != nil {
		return err
	}
	if err := writeConversationFTS(ctx, tx, id); err != nil {
		return err
	}
	if c.executionID == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_conversation_runs SET ended_at = ?, end_reason = ?
		WHERE execution_id = ? AND ended_at IS NULL AND conversation_id != ?
			AND conversation_id IN (SELECT id FROM agent_conversations WHERE agent_kind = ?)`,
		stamp, RunReplaced, c.executionID, id, c.agentKind); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO agent_conversation_runs (conversation_id, execution_id, started_at, sandboxed)
		VALUES (?, ?, ?, (SELECT a.sandboxed FROM executions e JOIN agent_sessions a ON a.id = e.agent_session_id WHERE e.id = ?))
		ON CONFLICT (conversation_id, execution_id) DO UPDATE SET ended_at = NULL, end_reason = NULL
		WHERE agent_conversation_runs.ended_at IS NOT NULL`,
		id, c.executionID, stamp, c.executionID)
	return err
}

// writeConversationFTS rewrites a conversation's search row from its entry.
func writeConversationFTS(ctx context.Context, tx *sqlx.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_conversations_fts WHERE conversation_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO agent_conversations_fts (conversation_id, task_title, task_id, cwd, repo, branch, first_message, last_message)
		SELECT id, task_title, task_id, cwd, repo, branch, first_message, last_message FROM agent_conversations WHERE id = ?`, id)
	return err
}

// runEndForStatus is why a run ends when its execution ends as status and
// the caller gives no reason of its own.
func runEndForStatus(status string) string {
	switch status {
	case ExecutionCancelled:
		return RunStopped
	case ExecutionLost:
		return RunLost
	default:
		return RunExited
	}
}

// endConversationRuns ends an execution's live runs, in the caller's
// transaction, and marks their conversations seen then.
func endConversationRuns(ctx context.Context, tx *sqlx.Tx, executionID, reason, stamp string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET last_seen_at = ?
		WHERE id IN (SELECT conversation_id FROM agent_conversation_runs WHERE execution_id = ? AND ended_at IS NULL)`, stamp, executionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE agent_conversation_runs SET ended_at = ?, end_reason = ?
		WHERE execution_id = ? AND ended_at IS NULL`, stamp, reason, executionID)
	return err
}

// DismissConversationRuns marks dismissed the interrupted runs of an
// execution whose interrupted agent the operator dismissed.
func (s *Store) DismissConversationRuns(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_conversation_runs SET end_reason = ?
		WHERE execution_id = ? AND end_reason = ?`, RunDismissed, executionID, RunInterrupted)
	return err
}

// ListTaskConversations returns the conversations of a task, deleted or
// not, the most recently seen first.
func (s *Store) ListTaskConversations(ctx context.Context, taskID string) ([]AgentConversation, error) {
	conversations := make([]AgentConversation, 0)
	err := s.db.SelectContext(ctx, &conversations, `SELECT * FROM agent_conversations WHERE task_id = ?
		ORDER BY last_seen_at DESC, id DESC`, taskID)
	return conversations, err
}

// ListConversationRuns returns a conversation's runs, the oldest first.
func (s *Store) ListConversationRuns(ctx context.Context, conversationID int64) ([]AgentConversationRun, error) {
	runs := make([]AgentConversationRun, 0)
	err := s.db.SelectContext(ctx, &runs, `SELECT conversation_id, execution_id, started_at, ended_at, end_reason, sandboxed
		FROM agent_conversation_runs WHERE conversation_id = ? ORDER BY started_at, rowid`, conversationID)
	return runs, err
}

// MatchAgentConversations returns the conversations an FTS5 expression
// matches, the best first.
func (s *Store) MatchAgentConversations(ctx context.Context, match string, limit int) ([]AgentConversation, error) {
	conversations := make([]AgentConversation, 0)
	err := s.db.SelectContext(ctx, &conversations, `SELECT c.* FROM agent_conversations_fts f
		JOIN agent_conversations c ON c.id = f.conversation_id
		WHERE agent_conversations_fts MATCH ?
		ORDER BY bm25(agent_conversations_fts), c.last_seen_at DESC, c.id DESC LIMIT ?`, match, limit)
	return conversations, err
}

// SweepAgentConversations removes the conversations last seen before a
// time, with their runs and search rows, except those with a live or an
// interrupted run. It returns how many it removed.
func (s *Store) SweepAgentConversations(ctx context.Context, before time.Time) (int, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	const old = `SELECT id FROM agent_conversations c WHERE c.last_seen_at < ?
		AND NOT EXISTS (SELECT 1 FROM agent_conversation_runs r WHERE r.conversation_id = c.id
			AND (r.ended_at IS NULL OR r.end_reason = 'interrupted'))`
	cutoff := formatExecutionTime(before)
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_conversations_fts WHERE conversation_id IN (`+old+`)`, cutoff); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM agent_conversations WHERE id IN (`+old+`)`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// RecordConversationText keeps the text of the conversations live in an
// execution: first, the first operator message, is set once (a tagged
// [legato …] one stands only until an untagged one is seen); last, the
// newest assistant message, replaces the one before at time at. An empty
// value changes nothing. The search rows are rewritten with them.
func (s *Store) RecordConversationText(ctx context.Context, executionID, first, last string, at time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := formatExecutionTime(at)
	var ids []int64
	if err := tx.SelectContext(ctx, &ids, `
		UPDATE agent_conversations SET
			first_message = CASE
				WHEN ? = '' THEN first_message
				WHEN first_message = '' THEN ?
				WHEN first_message LIKE '[legato %' AND ? NOT LIKE '[legato %' THEN ?
				ELSE first_message END,
			last_message = CASE WHEN ? = '' THEN last_message ELSE ? END,
			last_message_at = CASE WHEN ? = '' THEN last_message_at ELSE ? END,
			last_seen_at = ?
		WHERE id IN (SELECT conversation_id FROM agent_conversation_runs WHERE execution_id = ? AND ended_at IS NULL)
		RETURNING id`,
		first, first, first, first, last, last, last, stamp, stamp, executionID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := writeConversationFTS(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FindTaskConversation returns the task slot's conversation of a tool by
// its provider session ID, the most recently seen when several storage
// folders hold one; nil, nil when there is none.
func (s *Store) FindTaskConversation(ctx context.Context, taskID, slot, agentKind, providerSessionID string) (*AgentConversation, error) {
	var found []AgentConversation
	if err := s.db.SelectContext(ctx, &found, `SELECT * FROM agent_conversations
		WHERE task_id = ? AND slot = ? AND agent_kind = ? AND provider_session_id = ?
		ORDER BY last_seen_at DESC, id DESC LIMIT 1`, taskID, slotOrMain(slot), agentKind, providerSessionID); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// MoveAgentConversation gives a conversation to another task's slot, as
// restoring it into a new task does before the new task's agent starts.
func (s *Store) MoveAgentConversation(ctx context.Context, id int64, taskID, slot string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET task_id = ?, slot = ? WHERE id = ?`, taskID, slotOrMain(slot), id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err == nil {
			err = ErrNotFound
		}
		return err
	}
	if err := writeConversationFTS(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordTranscriptCheck keeps what a look for a conversation's transcript
// found: its path and state (TranscriptPresent or TranscriptMissing) at a
// time, and, from the transcript, the first and last messages the entry
// still lacks, with their search row.
func (s *Store) RecordTranscriptCheck(ctx context.Context, id int64, path, state string, at time.Time, first, last string, lastAt *time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lastStamp any
	if lastAt != nil {
		lastStamp = formatExecutionTime(*lastAt)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET
		transcript_path = ?, transcript_state = ?, transcript_checked_at = ?,
		first_message = CASE WHEN first_message = '' THEN ? ELSE first_message END,
		last_message_at = CASE WHEN last_message = '' AND ? != '' THEN ? ELSE last_message_at END,
		last_message = CASE WHEN last_message = '' THEN ? ELSE last_message END
		WHERE id = ?`, path, state, formatExecutionTime(at), first, last, lastStamp, last, id); err != nil {
		return err
	}
	if err := writeConversationFTS(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}
