package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sqlx.DB
}

func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying sqlx.DB for advanced queries.
func (s *Store) DB() *sqlx.DB {
	return s.db
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.Get(&version, "PRAGMA user_version"); err != nil {
		return err
	}

	migrations := []string{"001_init.sql", "002_stale_and_move_tracking.sql", "003_rename_jira_to_remote.sql", "004_agent_sessions.sql", "005_tasks.sql", "006_agent_activity.sql", "007_state_intervals.sql", "008_workspaces.sql", "009_archive.sql", "010_pr_meta.sql", "011_ephemeral.sql"}

	for i := version; i < len(migrations); i++ {
		data, err := migrationsFS.ReadFile("migrations/" + migrations[i])
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", migrations[i], err)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(string(data)); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", migrations[i], err)
		}

		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Task CRUD

func (s *Store) CreateTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, status,
			priority, sort_order, provider, remote_id, remote_meta,
			workspace_id, ephemeral, created_at, updated_at)
		VALUES (:id, :title, :description, :description_md, :status,
			:priority, :sort_order, :provider, :remote_id, :remote_meta,
			:workspace_id, :ephemeral, :created_at, :updated_at)`, t)
	return err
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	err := s.db.GetContext(ctx, &t, "SELECT * FROM tasks WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &t, err
}

// ErrNotFound is returned when a record is not found.
var ErrNotFound = fmt.Errorf("not found")

func (s *Store) ListTasksByStatus(ctx context.Context, status string) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks,
		"SELECT * FROM tasks WHERE status = ? AND archived_at IS NULL AND ephemeral = 0 ORDER BY sort_order ASC", status)
	return tasks, err
}

func (s *Store) UpdateTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE tasks SET
			title = :title, description = :description, description_md = :description_md,
			status = :status, priority = :priority, sort_order = :sort_order,
			provider = :provider, remote_id = :remote_id, remote_meta = :remote_meta,
			workspace_id = :workspace_id, updated_at = :updated_at
		WHERE id = :id`, t)
	return err
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
	return err
}

// UpsertTask inserts a task or updates it if it already exists.
func (s *Store) UpsertTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, status,
			priority, sort_order, provider, remote_id, remote_meta,
			workspace_id, ephemeral, created_at, updated_at)
		VALUES (:id, :title, :description, :description_md, :status,
			:priority, :sort_order, :provider, :remote_id, :remote_meta,
			:workspace_id, :ephemeral, :created_at, :updated_at)
		ON CONFLICT(id) DO UPDATE SET
			title = :title, description = :description, description_md = :description_md,
			status = :status, priority = :priority, sort_order = :sort_order,
			provider = :provider, remote_id = :remote_id, remote_meta = :remote_meta,
			workspace_id = :workspace_id, updated_at = :updated_at`, t)
	return err
}

// ListAllTasks returns all tasks in the store.
func (s *Store) ListAllTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks, "SELECT * FROM tasks ORDER BY id")
	return tasks, err
}

// ListTaskIDs returns all task IDs in the store.
func (s *Store) ListTaskIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.db.SelectContext(ctx, &ids, "SELECT id FROM tasks ORDER BY id")
	return ids, err
}

// WorkspaceView represents a workspace filter for task queries.
type WorkspaceView struct {
	Kind        WorkspaceViewKind
	WorkspaceID int // only used when Kind == ViewWorkspace
}

type WorkspaceViewKind int

const (
	ViewAll WorkspaceViewKind = iota
	ViewUnassigned
	ViewWorkspace
)

// ListTasksByStatusAndWorkspace returns tasks filtered by status and workspace view.
func (s *Store) ListTasksByStatusAndWorkspace(ctx context.Context, status string, view WorkspaceView) ([]Task, error) {
	var tasks []Task
	switch view.Kind {
	case ViewAll:
		return s.ListTasksByStatus(ctx, status)
	case ViewUnassigned:
		err := s.db.SelectContext(ctx, &tasks,
			"SELECT * FROM tasks WHERE status = ? AND workspace_id IS NULL AND archived_at IS NULL AND ephemeral = 0 ORDER BY sort_order ASC", status)
		return tasks, err
	case ViewWorkspace:
		err := s.db.SelectContext(ctx, &tasks,
			"SELECT * FROM tasks WHERE status = ? AND workspace_id = ? AND archived_at IS NULL AND ephemeral = 0 ORDER BY sort_order ASC", status, view.WorkspaceID)
		return tasks, err
	default:
		return s.ListTasksByStatus(ctx, status)
	}
}

// UpdateTaskWorkspace sets the workspace_id for a task.
func (s *Store) UpdateTaskWorkspace(ctx context.Context, taskID string, workspaceID *int) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET workspace_id = ?, updated_at = datetime('now') WHERE id = ?",
		workspaceID, taskID)
	return err
}

// CreateEphemeralTask creates a lightweight ephemeral task row for backing an agent session.
// It generates an 8-char ID, sets ephemeral=1, and uses the first column as status.
func (s *Store) CreateEphemeralTask(ctx context.Context, title string) (string, error) {
	// Get first column for default status
	var status string
	mappings, err := s.ListColumnMappings(ctx)
	if err != nil || len(mappings) == 0 {
		status = "Backlog" // fallback
	} else {
		status = mappings[0].ColumnName
	}

	id := GenerateTaskID()
	now := "datetime('now')"
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, status,
			priority, sort_order, ephemeral, created_at, updated_at)
		VALUES (?, ?, '', '', ?, '', 0, 1, `+now+`, `+now+`)`,
		id, title, status)
	if err != nil {
		return "", fmt.Errorf("creating ephemeral task: %w", err)
	}
	return id, nil
}

// Workspace CRUD

func (s *Store) CreateWorkspace(ctx context.Context, w Workspace) (int, error) {
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO workspaces (name, color, sort_order)
		VALUES (:name, :color, :sort_order)`, w)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return int(id), err
}

func (s *Store) GetWorkspace(ctx context.Context, id int) (*Workspace, error) {
	var w Workspace
	err := s.db.GetContext(ctx, &w, "SELECT * FROM workspaces WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &w, err
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var workspaces []Workspace
	err := s.db.SelectContext(ctx, &workspaces,
		"SELECT * FROM workspaces ORDER BY sort_order ASC, name ASC")
	return workspaces, err
}

// EnsureWorkspace inserts a workspace if it doesn't exist by name, or updates color/sort_order if it does.
func (s *Store) EnsureWorkspace(ctx context.Context, w Workspace) (int, error) {
	result, err := s.db.NamedExecContext(ctx, `
		INSERT INTO workspaces (name, color, sort_order)
		VALUES (:name, :color, :sort_order)
		ON CONFLICT(name) DO UPDATE SET
			color = :color, sort_order = :sort_order`, w)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return int(id), err
}

// Column Mapping CRUD

func (s *Store) CreateColumnMapping(ctx context.Context, m ColumnMapping) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO column_mappings (column_name, remote_statuses, remote_transition, sort_order)
		VALUES (:column_name, :remote_statuses, :remote_transition, :sort_order)`, m)
	return err
}

func (s *Store) ListColumnMappings(ctx context.Context) ([]ColumnMapping, error) {
	var mappings []ColumnMapping
	err := s.db.SelectContext(ctx, &mappings,
		"SELECT * FROM column_mappings ORDER BY sort_order ASC")
	return mappings, err
}

func (s *Store) UpdateColumnMapping(ctx context.Context, m ColumnMapping) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE column_mappings SET
			column_name = :column_name, remote_statuses = :remote_statuses,
			remote_transition = :remote_transition, sort_order = :sort_order
		WHERE id = :id`, m)
	return err
}

func (s *Store) DeleteColumnMapping(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM column_mappings WHERE id = ?", id)
	return err
}

// Sync Log

func (s *Store) InsertSyncLog(ctx context.Context, entry SyncLogEntry) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO sync_log (task_id, action, detail)
		VALUES (:task_id, :action, :detail)`, entry)
	return err
}

func (s *Store) ListSyncLogs(ctx context.Context, taskID string) ([]SyncLogEntry, error) {
	var entries []SyncLogEntry
	err := s.db.SelectContext(ctx, &entries,
		"SELECT * FROM sync_log WHERE task_id = ? ORDER BY created_at DESC, id DESC", taskID)
	return entries, err
}

// ArchiveTask sets archived_at for a single task.
func (s *Store) ArchiveTask(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET archived_at = datetime('now') WHERE id = ? AND archived_at IS NULL", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		// Check if task exists at all
		if _, err := s.GetTask(ctx, id); err != nil {
			return err
		}
		// Task exists but already archived — no-op
	}
	return nil
}

// UpdatePRMeta sets the pr_meta JSON field for a task.
func (s *Store) UpdatePRMeta(ctx context.Context, taskID string, prMeta *string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET pr_meta = ?, updated_at = datetime('now') WHERE id = ?",
		prMeta, taskID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPRTrackedTasks returns all non-archived tasks that have pr_meta set.
func (s *Store) ListPRTrackedTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks,
		"SELECT * FROM tasks WHERE pr_meta IS NOT NULL AND archived_at IS NULL ORDER BY id")
	return tasks, err
}

// ArchiveTasksByStatus archives all non-archived tasks with the given status.
func (s *Store) ArchiveTasksByStatus(ctx context.Context, status string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET archived_at = datetime('now') WHERE status = ? AND archived_at IS NULL", status)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
