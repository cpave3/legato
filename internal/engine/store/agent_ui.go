package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// AgentUIItem is content an agent asked Legato to show to the person. An item
// keeps its identity across revisions; the content fields are those of one
// revision (the latest unless another was asked for).
type AgentUIItem struct {
	ID          string `db:"id"`
	TaskID      string `db:"task_id"`
	ExecutionID string `db:"execution_id"`
	Placement   string `db:"placement"`
	// Key is the agent's own name for the item. Saving under a key the task
	// already used in that placement replaces the content as a new revision.
	Key      string `db:"item_key"`
	Revision int    `db:"revision"`
	// OldestRevision is the earliest revision still stored.
	OldestRevision int    `db:"oldest_revision"`
	Format         string `db:"format"`
	Title          string `db:"title"`
	Content        string `db:"content"`
	FallbackText   string `db:"fallback_text"`
	CreatedAt      string `db:"created_at"`
	UpdatedAt      string `db:"updated_at"`
}

// AgentUILimits bounds what one task may keep.
type AgentUILimits struct {
	// MaxItems is how many items a task keeps; saving past it removes the
	// items changed longest ago.
	MaxItems int
	// MaxRevisions is how many revisions of one item are kept.
	MaxRevisions int
}

const agentUIItemColumns = `
	i.id, i.task_id, i.execution_id, i.placement, COALESCE(i.item_key, '') AS item_key,
	r.revision, (SELECT MIN(revision) FROM agent_ui_item_revisions WHERE item_id = i.id) AS oldest_revision,
	r.format, r.title, r.fallback_text, i.created_at, i.updated_at`

// SaveAgentUIItem stores an item's content. With a key the task already used
// in that placement, the existing item gains a revision and item.ID is unused;
// otherwise a new item is created under item.ID. It returns the stored item
// without its content.
func (s *Store) SaveAgentUIItem(ctx context.Context, item AgentUIItem, limits AgentUILimits) (*AgentUIItem, error) {
	id := item.ID
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		revision := 1
		existing := false
		if item.Key != "" {
			var current struct {
				ID       string `db:"id"`
				Revision int    `db:"revision"`
			}
			err := tx.GetContext(ctx, &current, `
				SELECT id, revision FROM agent_ui_items
				WHERE task_id = ? AND placement = ? AND item_key = ?`, item.TaskID, item.Placement, item.Key)
			switch {
			case err == nil:
				existing, id, revision = true, current.ID, current.Revision+1
			case err != sql.ErrNoRows:
				return err
			}
		}
		if existing {
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_ui_items SET revision = ?, execution_id = ?, updated_at = datetime('now')
				WHERE id = ?`, revision, item.ExecutionID, id); err != nil {
				return err
			}
		} else {
			var key any
			if item.Key != "" {
				key = item.Key
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_ui_items (id, task_id, execution_id, placement, item_key, revision)
				VALUES (?, ?, ?, ?, ?, 1)`, id, item.TaskID, item.ExecutionID, item.Placement, key); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_ui_item_revisions (item_id, revision, format, title, content, fallback_text)
			VALUES (?, ?, ?, ?, ?, ?)`, id, revision, item.Format, item.Title, item.Content, item.FallbackText); err != nil {
			return err
		}
		if limits.MaxRevisions > 0 {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM agent_ui_item_revisions WHERE item_id = ? AND revision <= ?`, id, revision-limits.MaxRevisions); err != nil {
				return err
			}
		}
		if limits.MaxItems > 0 && !existing {
			// rowid breaks ties between items changed within the same second.
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM agent_ui_items WHERE task_id = ? AND id NOT IN (
					SELECT id FROM agent_ui_items WHERE task_id = ?
					ORDER BY updated_at DESC, rowid DESC LIMIT ?)`, item.TaskID, item.TaskID, limits.MaxItems); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var saved AgentUIItem
	if err := s.db.GetContext(ctx, &saved, `
		SELECT`+agentUIItemColumns+`
		FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision
		WHERE i.id = ?`, id); err != nil {
		return nil, err
	}
	return &saved, nil
}

// GetAgentUIItem returns an item with the content of one revision; revision 0
// means the latest.
func (s *Store) GetAgentUIItem(ctx context.Context, id string, revision int) (*AgentUIItem, error) {
	var item AgentUIItem
	err := s.db.GetContext(ctx, &item, `
		SELECT`+agentUIItemColumns+`, r.content
		FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id
		WHERE i.id = ? AND r.revision = CASE WHEN ? > 0 THEN ? ELSE i.revision END`, id, revision, revision)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListAgentUIItems returns a task's items at their latest revision, oldest
// first and without content.
func (s *Store) ListAgentUIItems(ctx context.Context, taskID string) ([]AgentUIItem, error) {
	var items []AgentUIItem
	err := s.db.SelectContext(ctx, &items, `
		SELECT`+agentUIItemColumns+`
		FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision
		WHERE i.task_id = ?
		ORDER BY i.created_at, i.rowid`, taskID)
	return items, err
}

// AgentUISummary says how much a task has shown and what it showed last.
type AgentUISummary struct {
	Cards       int
	Artifacts   int
	LatestID    string
	LatestTitle string
}

// SummarizeAgentUIItems counts a task's items by placement and names the one
// shown last: the item whose current revision was stored most recently.
func (s *Store) SummarizeAgentUIItems(ctx context.Context, taskID string) (AgentUISummary, error) {
	var summary AgentUISummary
	var counts []struct {
		Placement string `db:"placement"`
		Count     int    `db:"count"`
	}
	if err := s.db.SelectContext(ctx, &counts, `
		SELECT placement, COUNT(*) AS count FROM agent_ui_items WHERE task_id = ? GROUP BY placement`, taskID); err != nil {
		return summary, err
	}
	for _, row := range counts {
		if row.Placement == "panel" {
			summary.Artifacts = row.Count
		} else {
			summary.Cards = row.Count
		}
	}
	var latest struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}
	// Ordered by the revision's rowid, which grows with every save. The
	// stored timestamps have one-second precision and cannot order two
	// things shown in quick succession.
	err := s.db.GetContext(ctx, &latest, `
		SELECT i.id, r.title
		FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision
		WHERE i.task_id = ?
		ORDER BY r.rowid DESC LIMIT 1`, taskID)
	if err != nil && err != sql.ErrNoRows {
		return summary, err
	}
	summary.LatestID, summary.LatestTitle = latest.ID, latest.Title
	return summary, nil
}

// DeleteAgentUIItem removes an item and every revision of it.
func (s *Store) DeleteAgentUIItem(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agent_ui_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}
