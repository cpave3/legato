package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const sqliteBusyTimeoutMS = 5000

var addColumnStatement = regexp.MustCompile(`(?im)^\s*ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+(\w+)\s+([^;]+);\s*`)

var migrations = embeddedMigrationNames()

func embeddedMigrationNames() []string {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		panic(fmt.Sprintf("reading embedded migrations: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	return names
}

type Store struct {
	db *sqlx.DB
}

type SchemaStatus struct {
	Current   int
	Supported int
}

func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sqlx.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	s := &Store{db: db}
	if err := s.preflight(); err != nil {
		db.Close()
		return nil, fmt.Errorf("checking schema compatibility: %w", err)
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	if err := s.enableWAL(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func sqliteDSN(path string) string {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout("+strconv.Itoa(sqliteBusyTimeoutMS)+")")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Set("_txlock", "immediate")
	uri := url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	return uri.String()
}

func (s *Store) preflight() error {
	conn, err := s.db.Connx(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var version int
	if err := sqlx.GetContext(context.Background(), conn, &version, "PRAGMA user_version"); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this Legato binary supports (%d)", version, len(migrations))
	}
	var ledgerExists int
	if err := sqlx.GetContext(context.Background(), conn, &ledgerExists, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`); err != nil {
		return err
	}
	if ledgerExists == 0 {
		return nil
	}
	ledgerVersion, err := validateMigrationLedger(connQueryer{conn: conn})
	if err != nil {
		return err
	}
	if ledgerVersion != version {
		return fmt.Errorf("database schema markers disagree: migration ledger is %d but SQLite user_version is %d", ledgerVersion, version)
	}
	return nil
}

func (s *Store) enableWAL() error {
	var journalMode string
	if err := s.db.Get(&journalMode, "PRAGMA journal_mode"); err != nil {
		return fmt.Errorf("reading journal mode: %w", err)
	}
	if strings.EqualFold(journalMode, "wal") {
		return nil
	}
	deadline := time.Now().Add(time.Duration(sqliteBusyTimeoutMS) * time.Millisecond)
	for {
		if err := s.db.Get(&journalMode, "PRAGMA journal_mode=WAL"); err == nil {
			if !strings.EqualFold(journalMode, "wal") {
				return fmt.Errorf("enabling WAL returned journal mode %q", journalMode)
			}
			return nil
		} else if time.Now().After(deadline) || !strings.Contains(strings.ToLower(err.Error()), "locked") {
			return fmt.Errorf("enabling WAL: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying sqlx.DB for advanced queries.
func (s *Store) DB() *sqlx.DB {
	return s.db
}

func (s *Store) SchemaStatus(ctx context.Context) (SchemaStatus, error) {
	var current int
	if err := s.db.GetContext(ctx, &current, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations"); err != nil {
		return SchemaStatus{}, err
	}
	return SchemaStatus{Current: current, Supported: len(migrations)}, nil
}

func (s *Store) migrate() error {
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var legacyVersion int
	if err := tx.Get(&legacyVersion, "PRAGMA user_version"); err != nil {
		return err
	}
	if legacyVersion > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this Legato binary supports (%d)", legacyVersion, len(migrations))
	}
	var ledgerExists int
	if err := tx.Get(&ledgerExists, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`); err != nil {
		return err
	}
	if ledgerExists != 0 {
		version, err := validateMigrationLedger(tx)
		if err != nil {
			return err
		}
		if legacyVersion != version {
			return fmt.Errorf("database schema markers disagree: migration ledger is %d but SQLite user_version is %d", version, legacyVersion)
		}
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("creating migration ledger: %w", err)
	}
	for i := 0; i < legacyVersion; i++ {
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES (?, ?) ON CONFLICT(version) DO NOTHING`, i+1, migrations[i]); err != nil {
			return fmt.Errorf("bootstrapping migration ledger: %w", err)
		}
	}
	version, err := validateMigrationLedger(tx)
	if err != nil {
		return err
	}

	for i := version; i < len(migrations); i++ {
		data, err := migrationsFS.ReadFile("migrations/" + migrations[i])
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", migrations[i], err)
		}
		sqlText, err := prepareMigration(tx, migrations[i], string(data))
		if err != nil {
			return fmt.Errorf("preparing migration %s: %w", migrations[i], err)
		}
		if _, err := tx.Exec(sqlText); err != nil {
			return fmt.Errorf("executing migration %s: %w", migrations[i], err)
		}
		if migrations[i] == "058_workflow_trigger_confirmation.sql" {
			var tableSQL string
			err := tx.Get(&tableSQL, `SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'workflow_trigger_requests'`)
			if err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("validating migration %s: %w", migrations[i], err)
			}
			if err == nil && strings.Contains(tableSQL, "CHECK (status IN") {
				if !strings.Contains(tableSQL, "'waiting_confirmation'") {
					return fmt.Errorf("migration %s did not update workflow trigger status constraint", migrations[i])
				}
				var schemaVersion int
				if err := tx.Get(&schemaVersion, `PRAGMA schema_version`); err != nil {
					return fmt.Errorf("reading schema version after migration %s: %w", migrations[i], err)
				}
				if _, err := tx.Exec(`PRAGMA schema_version = ` + fmt.Sprint(schemaVersion+1)); err != nil {
					return fmt.Errorf("refreshing schema after migration %s: %w", migrations[i], err)
				}
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, i+1, migrations[i]); err != nil {
			return fmt.Errorf("recording migration %s: %w", migrations[i], err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type migrationQueryer interface {
	Select(dest any, query string, args ...any) error
	Get(dest any, query string, args ...any) error
}

type connQueryer struct {
	conn *sqlx.Conn
}

func (q connQueryer) Select(dest any, query string, args ...any) error {
	return sqlx.SelectContext(context.Background(), q.conn, dest, query, args...)
}

func (q connQueryer) Get(dest any, query string, args ...any) error {
	return sqlx.GetContext(context.Background(), q.conn, dest, query, args...)
}

func validateMigrationLedger(queryer migrationQueryer) (int, error) {
	var rows []struct {
		Version int    `db:"version"`
		Name    string `db:"name"`
	}
	if err := queryer.Select(&rows, `SELECT version, name FROM schema_migrations ORDER BY version`); err != nil {
		return 0, err
	}
	for i, row := range rows {
		version := i + 1
		if version > len(migrations) {
			return 0, fmt.Errorf("migration ledger version %d is newer than this Legato binary supports (%d)", row.Version, len(migrations))
		}
		if row.Version != version || row.Name != migrations[i] {
			return 0, fmt.Errorf("migration ledger entry %d is invalid: got version %d name %q, want version %d name %q", i+1, row.Version, row.Name, version, migrations[i])
		}
	}
	return len(rows), nil
}

func prepareMigration(queryer migrationQueryer, name, migrationSQL string) (string, error) {
	if name == "035_review_passes.sql" {
		migrationSQL = strings.Replace(migrationSQL, "CREATE TABLE review_passes (", "CREATE TABLE IF NOT EXISTS review_passes (", 1)
		migrationSQL = strings.Replace(migrationSQL, "CREATE TABLE review_pass_plans (", "CREATE TABLE IF NOT EXISTS review_pass_plans (", 1)
		migrationSQL = strings.Replace(migrationSQL, "INSERT INTO review_passes ", "INSERT OR IGNORE INTO review_passes ", 1)
		migrationSQL = strings.Replace(migrationSQL, "DROP INDEX idx_review_steps_commit;", "DROP INDEX IF EXISTS idx_review_steps_commit;", 1)
		migrationSQL = strings.ReplaceAll(migrationSQL, "CREATE INDEX idx_review_", "CREATE INDEX IF NOT EXISTS idx_review_")
	}
	if name == "057_remote_description.sql" {
		var taskTables, descriptionColumns int
		if err := queryer.Get(&taskTables, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`); err != nil {
			return "", err
		}
		if taskTables == 0 {
			return "", nil
		}
		if err := queryer.Get(&descriptionColumns, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name IN ('description', 'description_md')`); err != nil {
			return "", err
		}
		if descriptionColumns != 2 {
			migrationSQL = strings.Replace(migrationSQL, `UPDATE tasks
SET remote_description_md = description_md,
    description = '',
    description_md = ''
WHERE provider IS NOT NULL;`, "", 1)
		}
	}
	var checkErr error
	filtered := addColumnStatement.ReplaceAllStringFunc(migrationSQL, func(statement string) string {
		matches := addColumnStatement.FindStringSubmatch(statement)
		if len(matches) != 4 {
			return statement
		}
		compatible, err := existingColumnMatches(queryer, name, matches[1], matches[2], matches[3])
		if err != nil {
			checkErr = err
			return statement
		}
		if compatible {
			return ""
		}
		return statement
	})
	return filtered, checkErr
}

func existingColumnMatches(queryer migrationQueryer, migration, table, column, definition string) (bool, error) {
	var columns []struct {
		Name       string  `db:"name"`
		Type       string  `db:"type"`
		NotNull    int     `db:"notnull"`
		DefaultVal *string `db:"dflt_value"`
	}
	if err := queryer.Select(&columns, fmt.Sprintf(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(%q) WHERE name = ?`, table), column); err != nil {
		return false, err
	}
	if len(columns) == 0 {
		return false, nil
	}
	upper := strings.ToUpper(strings.Join(strings.Fields(definition), " "))
	wantType := strings.Fields(upper)[0]
	wantNotNull := strings.Contains(upper, " NOT NULL")
	wantDefault := ""
	if index := strings.Index(upper, " DEFAULT "); index >= 0 {
		wantDefault = strings.Fields(upper[index+len(" DEFAULT "):])[0]
	}
	gotDefault := ""
	if columns[0].DefaultVal != nil {
		gotDefault = strings.ToUpper(*columns[0].DefaultVal)
	}
	compatible := strings.EqualFold(columns[0].Type, wantType) && (columns[0].NotNull != 0) == wantNotNull && gotDefault == wantDefault
	if migration == "048_workflow_trigger_definition.sql" && table == "workflow_trigger_requests" && column == "definition_json" {
		compatible = strings.EqualFold(columns[0].Type, "TEXT") && columns[0].NotNull != 0 && (gotDefault == "" || gotDefault == wantDefault)
	}
	if !compatible {
		return false, fmt.Errorf("existing column %s.%s is incompatible with migration definition %q", table, column, definition)
	}
	return true, nil
}

// Task CRUD

func (s *Store) CreateTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, remote_description_md, status,
			priority, sort_order, provider, remote_id, remote_meta, metadata,
			workspace_id, ephemeral, swarm_working_dir, task_group, created_at, updated_at)
		VALUES (:id, :title, :description, :description_md, :remote_description_md, :status,
			:priority, :sort_order, :provider, :remote_id, :remote_meta, :metadata,
			:workspace_id, :ephemeral, :swarm_working_dir, :task_group, :created_at, :updated_at)`, t)
	return err
}

func (s *Store) SetTaskChimeraSessionID(ctx context.Context, taskID, sessionID string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET chimera_session_id = ?, updated_at = datetime('now') WHERE id = ?",
		sessionID, taskID)
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

// ErrConflict is returned when persisted state no longer matches a requested mutation.
var ErrConflict = fmt.Errorf("state conflict")

func (s *Store) ListTasksByStatus(ctx context.Context, status string) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks,
		"SELECT * FROM tasks WHERE status = ? AND archived_at IS NULL AND ephemeral = 0 ORDER BY sort_order ASC", status)
	return tasks, err
}

// UpdateTaskFromProvider updates only provider-owned and synchronized fields so
// concurrent edits to Legato-owned task details cannot be overwritten.
func (s *Store) UpdateTaskFromProvider(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE tasks SET title = :title, remote_description_md = :remote_description_md,
			status = :status, priority = :priority, remote_meta = :remote_meta,
			updated_at = :updated_at
		WHERE id = :id`, t)
	return err
}

// UpdateTaskRemoteMeta updates provider synchronization metadata without
// rewriting locally owned task fields.
func (s *Store) UpdateTaskRemoteMeta(ctx context.Context, taskID string, remoteMeta *string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET remote_meta = ?, updated_at = datetime('now') WHERE id = ?`, remoteMeta, taskID)
	return err
}

func (s *Store) UpdateTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE tasks SET
			title = :title, description = :description, description_md = :description_md,
			remote_description_md = :remote_description_md,
			status = :status, priority = :priority, sort_order = :sort_order,
			provider = :provider, remote_id = :remote_id, remote_meta = :remote_meta,
			metadata = COALESCE(:metadata, tasks.metadata), workspace_id = :workspace_id, swarm_working_dir = :swarm_working_dir,
			task_group = :task_group, updated_at = :updated_at
		WHERE id = :id`, t)
	return err
}

// SetTaskSwarmWorkingDir sets the working directory associated with a swarm
// rooted at this task. Use nil to clear.
// SetTaskWorktree atomically replaces the durable worktree metadata for a task.
// A nil worktree clears all fields.
func (s *Store) SetTaskWorktree(ctx context.Context, taskID string, worktree *TaskWorktree) error {
	var primaryDir, path, branch, baseBranch any
	if worktree != nil {
		primaryDir = worktree.PrimaryDir
		path = worktree.Path
		branch = worktree.Branch
		baseBranch = worktree.BaseBranch
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET worktree_primary_dir = ?, worktree_path = ?,
			worktree_branch = ?, worktree_base_branch = ?, updated_at = datetime('now')
		WHERE id = ?`, primaryDir, path, branch, baseBranch, taskID)
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

func (s *Store) SetTaskSwarmWorkingDir(ctx context.Context, taskID string, workingDir *string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET swarm_working_dir = ?, updated_at = datetime('now') WHERE id = ?",
		workingDir, taskID)
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

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET related_task_id = NULL WHERE related_task_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE workflow_runs SET trigger_request_id = NULL
			WHERE trigger_request_id IN (
				SELECT r.id
				FROM workflow_trigger_requests r
				JOIN task_transitions t ON t.id = r.transition_id
				WHERE t.task_id = ?
			)`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
		return err
	})
}

// UpsertTask inserts a task or updates it if it already exists.
func (s *Store) UpsertTask(ctx context.Context, t Task) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, remote_description_md, status,
			priority, sort_order, provider, remote_id, remote_meta, metadata,
			workspace_id, ephemeral, swarm_working_dir, created_at, updated_at)
		VALUES (:id, :title, :description, :description_md, :remote_description_md, :status,
			:priority, :sort_order, :provider, :remote_id, :remote_meta, :metadata,
			:workspace_id, :ephemeral, :swarm_working_dir, :created_at, :updated_at)
		ON CONFLICT(id) DO UPDATE SET
			title = :title, description = :description, description_md = :description_md,
			remote_description_md = :remote_description_md,
			status = :status, priority = :priority, sort_order = :sort_order,
			provider = :provider, remote_id = :remote_id, remote_meta = :remote_meta,
			metadata = COALESCE(:metadata, tasks.metadata), workspace_id = :workspace_id,
			swarm_working_dir = :swarm_working_dir,
			updated_at = :updated_at`, t)
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

const WorkflowWorkspaceName = "Workflows"

// ListTasksByStatusAndWorkspace returns tasks filtered by status and workspace view.
func (s *Store) ListTasksByStatusAndWorkspace(ctx context.Context, status string, view WorkspaceView) ([]Task, error) {
	var tasks []Task
	switch view.Kind {
	case ViewAll:
		err := s.db.SelectContext(ctx, &tasks, `
			SELECT tasks.* FROM tasks
			LEFT JOIN workspaces ON workspaces.id = tasks.workspace_id
			WHERE tasks.status = ? AND tasks.archived_at IS NULL AND tasks.ephemeral = 0
				AND (workspaces.name IS NULL OR workspaces.name <> ?)
			ORDER BY tasks.sort_order ASC`, status, WorkflowWorkspaceName)
		return tasks, err
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
		INSERT INTO tasks (id, title, description, description_md, remote_description_md, status,
			priority, sort_order, ephemeral, created_at, updated_at)
		VALUES (?, ?, '', '', '', ?, '', 0, 1, `+now+`, `+now+`)`,
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
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO workspaces (name, color, sort_order)
		VALUES (:name, :color, :sort_order)
		ON CONFLICT(name) DO UPDATE SET
			color = :color, sort_order = :sort_order`, w)
	if err != nil {
		return 0, err
	}
	var id int
	if err := s.db.GetContext(ctx, &id, `SELECT id FROM workspaces WHERE name = ?`, w.Name); err != nil {
		return 0, err
	}
	return id, nil
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

func (s *Store) ListArchivedTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks, "SELECT * FROM tasks WHERE archived_at IS NOT NULL ORDER BY archived_at DESC, id")
	return tasks, err
}

func (s *Store) RestoreTask(ctx context.Context, id, status string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET archived_at = NULL, status = ?, updated_at = datetime('now') WHERE id = ? AND archived_at IS NOT NULL", status, id)
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

// SetCodeContext atomically replaces the durable repository context for a task.
func (s *Store) SetCodeContext(ctx context.Context, taskID string, codeContext *CodeContext) error {
	raw, err := MarshalCodeContext(codeContext)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET code_context = ?, updated_at = datetime('now') WHERE id = ?", raw, taskID)
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

// ListPRSyncCandidates returns tasks eligible for background GitHub synchronization.
func (s *Store) ListPRSyncCandidates(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.db.SelectContext(ctx, &tasks,
		"SELECT * FROM tasks WHERE archived_at IS NULL AND ephemeral = 0 ORDER BY id")
	return tasks, err
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
