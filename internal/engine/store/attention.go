package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) UpsertAttentionItem(ctx context.Context, item AttentionItem) (*AttentionItem, bool, error) {
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO attention_items
			(id, producer, source_key, source_revision, task_id, workspace_id, title, reason,
			 detail, category, urgency, navigation_json, actions_json, status)
		VALUES
			(:id, :producer, :source_key, :source_revision, :task_id, :workspace_id, :title, :reason,
			 :detail, :category, :urgency, :navigation_json, :actions_json, 'open')
		ON CONFLICT(producer, source_key) DO UPDATE SET
			source_revision = excluded.source_revision,
			task_id = excluded.task_id,
			workspace_id = excluded.workspace_id,
			title = excluded.title,
			reason = excluded.reason,
			detail = excluded.detail,
			category = excluded.category,
			urgency = excluded.urgency,
			navigation_json = excluded.navigation_json,
			actions_json = excluded.actions_json,
			status = CASE
				WHEN attention_items.status = 'dismissed'
				 AND attention_items.dismissed_revision = excluded.source_revision THEN 'dismissed'
				ELSE 'open'
			END,
			dismissed_revision = CASE
				WHEN attention_items.status = 'dismissed'
				 AND attention_items.dismissed_revision = excluded.source_revision
				THEN attention_items.dismissed_revision ELSE '' END,
			resolved_at = NULL,
			updated_at = datetime('now')
		WHERE attention_items.source_revision != excluded.source_revision
			OR attention_items.task_id IS NOT excluded.task_id
			OR attention_items.workspace_id IS NOT excluded.workspace_id
			OR attention_items.title != excluded.title
			OR attention_items.reason != excluded.reason
			OR attention_items.detail != excluded.detail
			OR attention_items.category != excluded.category
			OR attention_items.urgency != excluded.urgency
			OR attention_items.navigation_json != excluded.navigation_json
			OR attention_items.actions_json != excluded.actions_json
			OR attention_items.status = 'resolved'
			OR (attention_items.status = 'dismissed' AND attention_items.dismissed_revision != excluded.source_revision)`, item)
	if err != nil {
		return nil, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	stored, err := s.GetAttentionItemBySource(ctx, item.Producer, item.SourceKey)
	return stored, rows > 0, err
}

func (s *Store) GetAttentionItem(ctx context.Context, id string) (*AttentionItem, error) {
	var item AttentionItem
	if err := s.db.GetContext(ctx, &item, "SELECT * FROM attention_items WHERE id = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *Store) GetAttentionItemBySource(ctx context.Context, producer, sourceKey string) (*AttentionItem, error) {
	var item AttentionItem
	if err := s.db.GetContext(ctx, &item, "SELECT * FROM attention_items WHERE producer = ? AND source_key = ?", producer, sourceKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *Store) CountAttentionItems(ctx context.Context, status string) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM attention_items"
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	if err := s.db.GetContext(ctx, &count, query, args...); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) ListAttentionItems(ctx context.Context, status string) ([]AttentionItem, error) {
	items := []AttentionItem{}
	query := "SELECT * FROM attention_items"
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY CASE urgency WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END, updated_at DESC, id"
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) DismissAttentionItem(ctx context.Context, id, sourceRevision string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE attention_items
		SET status = 'dismissed', dismissed_revision = source_revision, updated_at = datetime('now')
		WHERE id = ? AND status = 'open' AND source_revision = ?`, id, sourceRevision)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

func (s *Store) ResolveAttentionItemsMissing(ctx context.Context, producer string, sourceKeys []string) error {
	query := `UPDATE attention_items SET status = 'resolved', resolved_at = datetime('now'), updated_at = datetime('now')
		WHERE producer = ? AND status != 'resolved'`
	args := []any{producer}
	if len(sourceKeys) > 0 {
		query += " AND source_key NOT IN (" + strings.TrimRight(strings.Repeat("?,", len(sourceKeys)), ",") + ")"
		for _, key := range sourceKeys {
			args = append(args, key)
		}
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ResolveAttentionItem(ctx context.Context, producer, sourceKey string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE attention_items
		SET status = 'resolved', resolved_at = datetime('now'), updated_at = datetime('now')
		WHERE producer = ? AND source_key = ? AND status != 'resolved'`, producer, sourceKey)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows > 0 {
		return err
	}
	_, err = s.GetAttentionItemBySource(ctx, producer, sourceKey)
	if err == ErrNotFound {
		return nil
	}
	return err
}

func expectOneRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}
