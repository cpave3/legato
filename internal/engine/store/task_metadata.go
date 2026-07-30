package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func ParseTaskMetadata(raw *string) (map[string]any, error) {
	metadata := map[string]any{}
	if raw == nil || *raw == "" {
		return metadata, nil
	}
	if err := json.Unmarshal([]byte(*raw), &metadata); err != nil {
		return nil, fmt.Errorf("parse task metadata: %w", err)
	}
	return metadata, nil
}

func (s *Store) SetTaskMetadata(ctx context.Context, taskID string, path []string, value any) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw sql.NullString
	if err := tx.GetContext(ctx, &raw, "SELECT metadata FROM tasks WHERE id = ?", taskID); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	var pointer *string
	if raw.Valid {
		pointer = &raw.String
	}
	metadata, err := ParseTaskMetadata(pointer)
	if err != nil {
		return err
	}
	current := metadata
	for _, part := range path[:len(path)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode task metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET metadata = ?, updated_at = datetime('now') WHERE id = ?", string(encoded), taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func ValidateTaskMetadataPath(path string) ([]string, error) {
	parts := strings.Split(path, ".")
	if path == "" || strings.HasPrefix(path, "legato.") || path == "legato" {
		return nil, fmt.Errorf("invalid task metadata path %q", path)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("invalid task metadata path %q", path)
		}
	}
	return parts, nil
}
