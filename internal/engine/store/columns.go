package store

import (
	"context"
	"fmt"
	"strings"
)

// ColumnSyncResult describes the changes made while reconciling configured
// columns with the durable board state.
type ColumnSyncResult struct {
	Added       []string `json:"added"`
	Updated     []string `json:"updated"`
	Removed     []string `json:"removed"`
	Fallback    string   `json:"fallback"`
	TasksMoved  int64    `json:"tasks_moved"`
	ColumnCount int      `json:"column_count"`
}

// SyncColumnMappings atomically makes the stored board columns match desired.
// Tasks in removed columns are moved to Unmapped when configured, or otherwise
// to the first desired column.
func (s *Store) SyncColumnMappings(ctx context.Context, desired []ColumnMapping) (result ColumnSyncResult, err error) {
	result.Added = []string{}
	result.Updated = []string{}
	result.Removed = []string{}
	if len(desired) == 0 {
		return result, fmt.Errorf("at least one board column must be configured")
	}

	seen := make(map[string]struct{}, len(desired))
	for i := range desired {
		desired[i].ColumnName = strings.TrimSpace(desired[i].ColumnName)
		if desired[i].ColumnName == "" {
			return result, fmt.Errorf("board column %d has an empty name", i+1)
		}
		key := strings.ToLower(desired[i].ColumnName)
		if _, exists := seen[key]; exists {
			return result, fmt.Errorf("duplicate board column name %q", desired[i].ColumnName)
		}
		seen[key] = struct{}{}
		desired[i].SortOrder = i
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin column sync: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var existing []ColumnMapping
	if err = tx.SelectContext(ctx, &existing, "SELECT * FROM column_mappings ORDER BY sort_order ASC"); err != nil {
		return result, fmt.Errorf("list existing columns: %w", err)
	}
	existingByName := make(map[string]ColumnMapping, len(existing))
	for _, column := range existing {
		existingByName[column.ColumnName] = column
	}

	for _, column := range desired {
		if current, exists := existingByName[column.ColumnName]; exists {
			column.ID = current.ID
			if current.RemoteStatuses != column.RemoteStatuses ||
				current.RemoteTransition != column.RemoteTransition ||
				current.SortOrder != column.SortOrder {
				if _, err = tx.NamedExecContext(ctx, `
					UPDATE column_mappings SET
						remote_statuses = :remote_statuses,
						remote_transition = :remote_transition,
						sort_order = :sort_order
					WHERE id = :id`, column); err != nil {
					return result, fmt.Errorf("update column %q: %w", column.ColumnName, err)
				}
				result.Updated = append(result.Updated, column.ColumnName)
			}
			delete(existingByName, column.ColumnName)
			continue
		}
		if _, err = tx.NamedExecContext(ctx, `
			INSERT INTO column_mappings (column_name, remote_statuses, remote_transition, sort_order)
			VALUES (:column_name, :remote_statuses, :remote_transition, :sort_order)`, column); err != nil {
			return result, fmt.Errorf("add column %q: %w", column.ColumnName, err)
		}
		result.Added = append(result.Added, column.ColumnName)
	}

	result.Fallback = desired[0].ColumnName
	for _, column := range desired {
		if strings.EqualFold(column.ColumnName, "Unmapped") {
			result.Fallback = column.ColumnName
			break
		}
	}
	for _, column := range existing {
		removed, exists := existingByName[column.ColumnName]
		if !exists {
			continue
		}
		move, moveErr := tx.ExecContext(ctx,
			"UPDATE tasks SET status = ?, updated_at = datetime('now') WHERE status = ?",
			result.Fallback, removed.ColumnName)
		if moveErr != nil {
			err = fmt.Errorf("move tasks from removed column %q: %w", removed.ColumnName, moveErr)
			return result, err
		}
		moved, rowsErr := move.RowsAffected()
		if rowsErr != nil {
			err = fmt.Errorf("count tasks moved from %q: %w", removed.ColumnName, rowsErr)
			return result, err
		}
		result.TasksMoved += moved
		if _, err = tx.ExecContext(ctx, "DELETE FROM column_mappings WHERE id = ?", removed.ID); err != nil {
			return result, fmt.Errorf("remove column %q: %w", removed.ColumnName, err)
		}
		result.Removed = append(result.Removed, removed.ColumnName)
	}

	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit column sync: %w", err)
	}
	result.ColumnCount = len(desired)
	return result, nil
}
