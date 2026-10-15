package store

import (
	"context"
	"fmt"
)

// Resource is a named set of files and folders an agent can be given.
type Resource struct {
	ID        int64          `db:"id" json:"id"`
	Name      string         `db:"name" json:"name"`
	Paths     []ResourcePath `db:"-" json:"paths"`
	CreatedAt string         `db:"created_at" json:"created_at"`
	UpdatedAt string         `db:"updated_at" json:"updated_at"`
}

// ResourcePath is one file or folder of a resource and what an agent may do
// with it: "read" or "write".
type ResourcePath struct {
	Path   string `db:"path" json:"path"`
	Access string `db:"access" json:"access"`
}

// SaveResource creates a resource, or replaces the name and paths of the one
// with the resource's ID.
func (s *Store) SaveResource(ctx context.Context, resource Resource) (Resource, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()

	if resource.ID == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO resources (name) VALUES (?)`, resource.Name)
		if err != nil {
			return Resource{}, err
		}
		if resource.ID, err = result.LastInsertId(); err != nil {
			return Resource{}, err
		}
	} else {
		result, err := tx.ExecContext(ctx,
			`UPDATE resources SET name = ?, updated_at = datetime('now') WHERE id = ?`,
			resource.Name, resource.ID)
		if err != nil {
			return Resource{}, err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return Resource{}, fmt.Errorf("resource %d: %w", resource.ID, ErrNotFound)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM resource_paths WHERE resource_id = ?`, resource.ID); err != nil {
			return Resource{}, err
		}
	}
	for i, path := range resource.Paths {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO resource_paths (resource_id, position, path, access) VALUES (?, ?, ?, ?)`,
			resource.ID, i, path.Path, path.Access); err != nil {
			return Resource{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

// ListResources returns the stored resources by name, each with its paths in
// the order they were saved.
func (s *Store) ListResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource
	if err := s.db.SelectContext(ctx, &resources,
		`SELECT id, name, created_at, updated_at FROM resources ORDER BY name COLLATE NOCASE`); err != nil {
		return nil, err
	}
	var rows []struct {
		ResourceID int64 `db:"resource_id"`
		ResourcePath
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT resource_id, path, access FROM resource_paths ORDER BY resource_id, position`); err != nil {
		return nil, err
	}
	index := make(map[int64]int, len(resources))
	for i, resource := range resources {
		index[resource.ID] = i
	}
	for _, row := range rows {
		if i, ok := index[row.ResourceID]; ok {
			resources[i].Paths = append(resources[i].Paths, row.ResourcePath)
		}
	}
	return resources, nil
}

// DeleteResource removes a resource and its paths.
func (s *Store) DeleteResource(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM resources WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("resource %d: %w", id, ErrNotFound)
	}
	return nil
}

// AddExecutionResources records that an execution was given the named
// resources. A name it already has is kept once.
func (s *Store) AddExecutionResources(ctx context.Context, executionID string, names []string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range names {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO execution_resources (execution_id, name) VALUES (?, ?)
			 ON CONFLICT(execution_id, name) DO NOTHING`, executionID, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListExecutionResources returns the names of the resources an execution was
// given, in the order it was given them.
func (s *Store) ListExecutionResources(ctx context.Context, executionID string) ([]string, error) {
	var names []string
	err := s.db.SelectContext(ctx, &names,
		`SELECT name FROM execution_resources WHERE execution_id = ? ORDER BY rowid`, executionID)
	return names, err
}
