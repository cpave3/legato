package store

import (
	"context"
	"fmt"
)

// ListTaskGroups names each group a task that is not archived is in, once,
// in name order.
func (s *Store) ListTaskGroups(ctx context.Context) ([]string, error) {
	groups := []string{}
	err := s.db.SelectContext(ctx, &groups, `
		SELECT DISTINCT task_group FROM tasks
		WHERE task_group IS NOT NULL AND task_group != '' AND archived_at IS NULL
		ORDER BY task_group`)
	if err != nil {
		return nil, fmt.Errorf("list task groups: %w", err)
	}
	return groups, nil
}

// RenameTaskGroup moves every task in the group from to the group to, or out
// of any group when to is nil, and says how many tasks it moved.
func (s *Store) RenameTaskGroup(ctx context.Context, from string, to *string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET task_group = ?, updated_at = datetime('now') WHERE task_group = ?`, to, from)
	if err != nil {
		return 0, fmt.Errorf("rename task group: %w", err)
	}
	return result.RowsAffected()
}
