package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The states of a conversation in the session history, as the store can
// tell them: one with a live run is running, one whose latest run was
// interrupted is interrupted, and the rest have ended.
const (
	ConversationRunning     = "running"
	ConversationInterrupted = "interrupted"
	ConversationEnded       = "ended"
)

// AgentConversationRow is a conversation as the history lists it: with how
// many runs it had, whether one is live, why its latest ended, and whether
// its task still exists. TaskTitle is the task's own while it exists.
type AgentConversationRow struct {
	AgentConversation
	RunCount      int    `db:"run_count"`
	Live          bool   `db:"live"`
	LastEndReason string `db:"last_end_reason"`
	TaskExists    bool   `db:"task_exists"`
	TaskArchived  bool   `db:"task_archived"`
}

// ConversationFilter narrows the history. Match is an FTS5 expression the
// caller has made safe; with it the best matches come first, without it
// the most recently seen.
type ConversationFilter struct {
	Match     string
	AgentKind string
	State     string // ConversationRunning, ConversationInterrupted or ConversationEnded
	TaskID    string
	Repo      string
	Since     time.Time // last seen at or after
	Until     time.Time // last seen before
	Limit     int       // 50 when zero
	Offset    int
}

const conversationRowColumns = `c.id, c.agent_kind, c.provider_session_id, c.storage_dir, c.task_id, c.slot,
	COALESCE(t.title, c.task_title) AS task_title, c.ephemeral, c.reeve, c.cwd, c.repo, c.branch, c.source,
	c.first_message, c.last_message, c.last_message_at, c.first_seen_at, c.last_seen_at,
	c.transcript_path, c.transcript_state, c.transcript_checked_at,
	(SELECT COUNT(*) FROM agent_conversation_runs r WHERE r.conversation_id = c.id) AS run_count,
	EXISTS (SELECT 1 FROM agent_conversation_runs r WHERE r.conversation_id = c.id AND r.ended_at IS NULL) AS live,
	COALESCE((SELECT r.end_reason FROM agent_conversation_runs r WHERE r.conversation_id = c.id
		ORDER BY r.started_at DESC, r.rowid DESC LIMIT 1), '') AS last_end_reason,
	t.id IS NOT NULL AS task_exists,
	COALESCE(t.archived_at IS NOT NULL, 0) AS task_archived`

// ListAgentConversations returns a page of the history.
func (s *Store) ListAgentConversations(ctx context.Context, f ConversationFilter) ([]AgentConversationRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	var where []string
	var args []any
	from := `agent_conversations c`
	rank := `0`
	if f.Match != "" {
		from = `agent_conversations_fts f JOIN agent_conversations c ON c.id = f.conversation_id`
		where = append(where, `agent_conversations_fts MATCH ?`)
		args = append(args, f.Match)
		rank = `bm25(agent_conversations_fts)`
	}
	for _, eq := range []struct{ column, value string }{{"c.agent_kind", f.AgentKind}, {"c.task_id", f.TaskID}, {"c.repo", f.Repo}} {
		if eq.value != "" {
			where = append(where, eq.column+` = ?`)
			args = append(args, eq.value)
		}
	}
	if !f.Since.IsZero() {
		where = append(where, `c.last_seen_at >= ?`)
		args = append(args, formatExecutionTime(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, `c.last_seen_at < ?`)
		args = append(args, formatExecutionTime(f.Until))
	}
	query := `SELECT ` + conversationRowColumns + `, ` + rank + ` AS rank FROM ` + from + `
		LEFT JOIN tasks t ON t.id = c.task_id`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	outer := ``
	switch f.State {
	case ConversationRunning:
		outer = ` WHERE live`
	case ConversationInterrupted:
		outer = ` WHERE NOT live AND last_end_reason = 'interrupted'`
	case ConversationEnded:
		outer = ` WHERE NOT live AND last_end_reason != 'interrupted'`
	}
	query = `SELECT * FROM (` + query + `)` + outer + ` ORDER BY rank, last_seen_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)

	var rows []struct {
		AgentConversationRow
		Rank float64 `db:"rank"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make([]AgentConversationRow, len(rows))
	for i, row := range rows {
		out[i] = row.AgentConversationRow
	}
	return out, nil
}

// GetAgentConversation returns one entry of the history, nil, nil when
// there is none.
func (s *Store) GetAgentConversation(ctx context.Context, id int64) (*AgentConversationRow, error) {
	var row AgentConversationRow
	err := s.db.GetContext(ctx, &row, `SELECT `+conversationRowColumns+` FROM agent_conversations c
		LEFT JOIN tasks t ON t.id = c.task_id WHERE c.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ForgetAgentConversation removes a conversation, its runs and its search
// row. A conversation running or interrupted is ErrConflict: its agent is
// still the task's.
func (s *Store) ForgetAgentConversation(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state struct {
		Live          bool   `db:"live"`
		LastEndReason string `db:"last_end_reason"`
	}
	err = tx.GetContext(ctx, &state, `SELECT
		EXISTS (SELECT 1 FROM agent_conversation_runs r WHERE r.conversation_id = c.id AND r.ended_at IS NULL) AS live,
		COALESCE((SELECT r.end_reason FROM agent_conversation_runs r WHERE r.conversation_id = c.id
			ORDER BY r.started_at DESC, r.rowid DESC LIMIT 1), '') AS last_end_reason
		FROM agent_conversations c WHERE c.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state.Live || state.LastEndReason == RunInterrupted {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_conversations_fts WHERE conversation_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_conversations WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
