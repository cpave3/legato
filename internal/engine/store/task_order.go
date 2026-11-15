package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// boardOrder is how every listing of a column orders its cards: by
// sort_order, ties (imported tickets all start at 0) in the order the tasks
// were added.
const boardOrder = "tasks.sort_order ASC, tasks.rowid ASC"

// PlaceTask moves a task within its own column so that it comes just before
// the task beforeID, or last when beforeID is empty, and numbers the column's
// cards again from 0. Only sort_order changes: a card's place on the board is
// not a change to the task. A beforeID that is not a card in the same column
// (moved, archived or gone since the board was drawn) is ErrConflict.
func (s *Store) PlaceTask(ctx context.Context, id, beforeID string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var status string
		if err := tx.GetContext(ctx, &status, `SELECT status FROM tasks WHERE id = ?`, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if beforeID == id {
			return nil
		}
		var ids []string
		if err := tx.SelectContext(ctx, &ids, `SELECT id FROM tasks
			WHERE status = ? AND archived_at IS NULL AND ephemeral = 0
			ORDER BY `+boardOrder, status); err != nil {
			return err
		}
		order := make([]string, 0, len(ids))
		placed, onBoard := beforeID == "", false
		for _, other := range ids {
			if other == id {
				onBoard = true
				continue
			}
			if other == beforeID {
				order = append(order, id)
				placed = true
			}
			order = append(order, other)
		}
		if !onBoard {
			return fmt.Errorf("%s is not on the board: %w", id, ErrConflict)
		}
		if !placed {
			return fmt.Errorf("%s is not a card in %s: %w", beforeID, status, ErrConflict)
		}
		if beforeID == "" {
			order = append(order, id)
		}
		for i, taskID := range order {
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET sort_order = ? WHERE id = ?`, i, taskID); err != nil {
				return err
			}
		}
		return nil
	})
}
