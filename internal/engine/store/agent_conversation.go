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
}

// AgentConversationRun is one execution a conversation was live in.
type AgentConversationRun struct {
	ConversationID int64      `db:"conversation_id"`
	ExecutionID    string     `db:"execution_id"`
	StartedAt      time.Time  `db:"started_at"`
	EndedAt        *time.Time `db:"ended_at"`
	EndReason      *string    `db:"end_reason"`
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
		Title     string `db:"title"`
		Ephemeral bool   `db:"ephemeral"`
		Kind      string `db:"kind"`
	}
	if err := tx.GetContext(ctx, &task, `SELECT title, ephemeral, COALESCE(kind, '') AS kind FROM tasks WHERE id = ?`, c.taskID); err != nil {
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
			ephemeral, reeve, cwd, repo, branch, source, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (agent_kind, storage_dir, provider_session_id) DO UPDATE SET
			task_id = excluded.task_id, slot = excluded.slot, task_title = excluded.task_title,
			ephemeral = excluded.ephemeral, reeve = excluded.reeve,
			cwd = CASE WHEN excluded.cwd = '' THEN agent_conversations.cwd ELSE excluded.cwd END,
			repo = CASE WHEN excluded.cwd = '' THEN agent_conversations.repo ELSE excluded.repo END,
			branch = CASE WHEN excluded.cwd = '' THEN agent_conversations.branch ELSE excluded.branch END,
			source = 'recorded', last_seen_at = excluded.last_seen_at
		RETURNING id`,
		c.agentKind, c.providerSessionID, c.storageDir, c.taskID, slotOrMain(c.slot), task.Title,
		task.Ephemeral, task.Kind == TaskKindReeve, cwd, clone.Repo, clone.Branch, ConversationRecorded, stamp, stamp); err != nil {
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
		INSERT INTO agent_conversation_runs (conversation_id, execution_id, started_at) VALUES (?, ?, ?)
		ON CONFLICT (conversation_id, execution_id) DO UPDATE SET ended_at = NULL, end_reason = NULL
		WHERE agent_conversation_runs.ended_at IS NOT NULL`,
		id, c.executionID, stamp)
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
	err := s.db.SelectContext(ctx, &runs, `SELECT conversation_id, execution_id, started_at, ended_at, end_reason
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
