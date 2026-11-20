package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// The states of an operator item.
const (
	OperatorItemOpen   = "open"
	OperatorItemClosed = "closed"
)

// Why an operator item closed.
const (
	// OperatorItemCleared: the agent cleared it with clear_for_operator.
	OperatorItemCleared = "cleared"
	// OperatorItemOperatorTurn: the operator next typed to the agent.
	OperatorItemOperatorTurn = "operator_turn"
	// OperatorItemAgentEnded: the agent that posted it ended.
	OperatorItemAgentEnded = "agent_ended"
	// OperatorItemDone: the operator pressed Done.
	OperatorItemDone = "done"
	// OperatorItemAnswered: the operator answered a question from Attention.
	OperatorItemAnswered = "answered"
)

// OperatorItem is something an agent posted for the operator to see or act
// on (for_operator). See docs/ai/attention.md § Operator items.
type OperatorItem struct {
	ID string `db:"id"`
	// TaskID is the posting session's task; AttributionTaskID the task the
	// operator sees the item on (a swarm worker's parent).
	TaskID            string `db:"task_id"`
	AttributionTaskID string `db:"attribution_task_id"`
	ExecutionID       string `db:"execution_id"`
	// Kind is action, question, done or other.
	Kind  string `db:"kind"`
	Title string `db:"title"`
	Why   string `db:"why"`
	// HowKind is command, slash, link or empty; HowText what to run or open.
	HowKind     string  `db:"how_kind"`
	HowText     string  `db:"how_text"`
	OptionsJSON string  `db:"options_json"`
	Status      string  `db:"status"`
	CloseReason string  `db:"close_reason"`
	Revision    int     `db:"revision"`
	CreatedAt   string  `db:"created_at"`
	UpdatedAt   string  `db:"updated_at"`
	ClosedAt    *string `db:"closed_at"`
}

// Options are a question's answers to choose from.
func (i OperatorItem) Options() []string {
	var out []string
	_ = json.Unmarshal([]byte(i.OptionsJSON), &out)
	return out
}

const operatorItemColumns = `id, task_id, attribution_task_id, execution_id, kind, title, why, how_kind, how_text,
	options_json, status, close_reason, revision, created_at, updated_at, closed_at`

const operatorItemNow = `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`

// PostOperatorItem keeps an item. When the same execution has an open item
// of the same title, that item takes the new kind, why, how and options and
// a new revision instead, and is returned with its own ID.
func (s *Store) PostOperatorItem(ctx context.Context, item OperatorItem, options []string) (OperatorItem, error) {
	if options == nil {
		options = []string{}
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return OperatorItem{}, err
	}
	item.OptionsJSON = string(raw)
	var out OperatorItem
	err = withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var existing string
		err := tx.GetContext(ctx, &existing, `SELECT id FROM operator_items
			WHERE status = 'open' AND execution_id = ? AND task_id = ? AND title = ? LIMIT 1`,
			item.ExecutionID, item.TaskID, item.Title)
		switch {
		case err == nil:
			item.ID = existing
			if _, err := tx.ExecContext(ctx, `UPDATE operator_items
				SET kind = ?, why = ?, how_kind = ?, how_text = ?, options_json = ?,
				    revision = revision + 1, updated_at = `+operatorItemNow+`
				WHERE id = ?`, item.Kind, item.Why, item.HowKind, item.HowText, item.OptionsJSON, item.ID); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `INSERT INTO operator_items
				(id, task_id, attribution_task_id, execution_id, kind, title, why, how_kind, how_text, options_json)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				item.ID, item.TaskID, item.AttributionTaskID, item.ExecutionID, item.Kind, item.Title, item.Why,
				item.HowKind, item.HowText, item.OptionsJSON); err != nil {
				return err
			}
		default:
			return err
		}
		return tx.GetContext(ctx, &out, `SELECT `+operatorItemColumns+` FROM operator_items WHERE id = ?`, item.ID)
	})
	return out, err
}

// GetOperatorItem returns one item, open or closed.
func (s *Store) GetOperatorItem(ctx context.Context, id string) (OperatorItem, error) {
	var item OperatorItem
	err := s.db.GetContext(ctx, &item, `SELECT `+operatorItemColumns+` FROM operator_items WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return OperatorItem{}, ErrNotFound
	}
	return item, err
}

// ListOpenOperatorItems returns every open item, oldest first.
func (s *Store) ListOpenOperatorItems(ctx context.Context) ([]OperatorItem, error) {
	items := []OperatorItem{}
	err := s.db.SelectContext(ctx, &items, `SELECT `+operatorItemColumns+` FROM operator_items
		WHERE status = 'open' ORDER BY created_at, id`)
	return items, err
}

// CloseOperatorItem closes one open item of an execution, for reason.
// ErrNotFound when that execution has no such open item.
func (s *Store) CloseOperatorItem(ctx context.Context, id, executionID, reason string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE operator_items
		SET status = 'closed', close_reason = ?, closed_at = `+operatorItemNow+`, updated_at = `+operatorItemNow+`
		WHERE id = ? AND execution_id = ? AND status = 'open'`, reason, id, executionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CloseExecutionOperatorItems closes every open item of an execution, for
// reason, and says how many it closed.
func (s *Store) CloseExecutionOperatorItems(ctx context.Context, executionID, reason string) (int, error) {
	if executionID == "" {
		return 0, fmt.Errorf("closing operator items needs an execution")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE operator_items
		SET status = 'closed', close_reason = ?, closed_at = `+operatorItemNow+`, updated_at = `+operatorItemNow+`
		WHERE execution_id = ? AND status = 'open'`, reason, executionID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// MoveOperatorItems gives the open items of one execution to another: a
// resumed agent takes over what its interrupted one posted.
func (s *Store) MoveOperatorItems(ctx context.Context, fromExecutionID, toExecutionID string) (int, error) {
	if fromExecutionID == "" || toExecutionID == "" || fromExecutionID == toExecutionID {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE operator_items SET execution_id = ?
		WHERE execution_id = ? AND status = 'open'`, toExecutionID, fromExecutionID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// CloseOrphanOperatorItems closes, as agent_ended, the open items of
// executions that no longer run, except an interrupted agent's, which wait
// for it to be resumed or dismissed. It is the startup sweep: an agent can
// end while no Legato runs to close what it posted.
func (s *Store) CloseOrphanOperatorItems(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE operator_items
		SET status = 'closed', close_reason = ?, closed_at = `+operatorItemNow+`, updated_at = `+operatorItemNow+`
		WHERE status = 'open'
		AND NOT EXISTS (SELECT 1 FROM executions e WHERE e.id = operator_items.execution_id AND e.status = 'running')
		AND NOT EXISTS (SELECT 1 FROM executions e JOIN agent_sessions a ON a.id = e.agent_session_id
			WHERE e.id = operator_items.execution_id AND a.status = 'interrupted')`, OperatorItemAgentEnded)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
