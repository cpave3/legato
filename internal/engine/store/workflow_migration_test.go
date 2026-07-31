package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationAddsWorkflowTriggerDefinitionSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-trigger.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE workflow_trigger_requests (
			id TEXT PRIMARY KEY, transition_id TEXT NOT NULL, workflow_name TEXT NOT NULL,
			definition_source TEXT NOT NULL, definition_digest TEXT NOT NULL,
			inputs_json TEXT NOT NULL DEFAULT '{}', missing_inputs_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL, run_id TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE (transition_id, workflow_name, definition_digest)
		)`,
		`PRAGMA user_version=47`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := New(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer store.Close()

	var definitionJSON string
	if err := store.DB().Get(&definitionJSON, `SELECT definition_json FROM workflow_trigger_requests LIMIT 1`); err != sql.ErrNoRows {
		t.Fatalf("definition_json column check: %v", err)
	}
}

func TestMigrationFromLegacyNullableRelatedTaskID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, pr_meta TEXT)`,
		`CREATE TABLE agent_sessions (id INTEGER PRIMARY KEY, task_id TEXT, tmux_session TEXT, command TEXT, status TEXT, started_at DATETIME, ended_at DATETIME)`,
		`CREATE TABLE workflow_runs (
			id TEXT PRIMARY KEY, workflow_name TEXT NOT NULL, workflow_title TEXT NOT NULL,
			definition_source TEXT NOT NULL, definition_digest TEXT NOT NULL, definition_json TEXT NOT NULL,
			inputs_json TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL, current_step_id TEXT NOT NULL DEFAULT '',
			task_id TEXT, error TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			started_at DATETIME, completed_at DATETIME,
			related_task_id TEXT REFERENCES tasks(id)
		)`,
		`CREATE TABLE workflow_step_runs (run_id TEXT NOT NULL, step_id TEXT NOT NULL, step_order INTEGER NOT NULL, title TEXT NOT NULL, executor_type TEXT NOT NULL, definition_json TEXT NOT NULL, resolved_inputs_json TEXT NOT NULL DEFAULT '{}', outputs_json TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL, latest_attempt INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', started_at DATETIME, completed_at DATETIME, PRIMARY KEY (run_id, step_id))`,
		`CREATE TABLE workflow_step_attempts (id TEXT PRIMARY KEY, run_id TEXT NOT NULL, step_id TEXT NOT NULL, attempt_number INTEGER NOT NULL, executor_type TEXT NOT NULL, agent_session_id INTEGER, task_id TEXT NOT NULL DEFAULT '', tmux_session TEXT NOT NULL DEFAULT '', agent_kind TEXT NOT NULL DEFAULT '', tier TEXT NOT NULL DEFAULT '', working_dir TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', started_at DATETIME NOT NULL DEFAULT (datetime('now')), ended_at DATETIME)`,
		`CREATE TABLE workflow_artifacts (id TEXT PRIMARY KEY, run_id TEXT NOT NULL, step_id TEXT NOT NULL, attempt_id TEXT NOT NULL, task_id TEXT NOT NULL DEFAULT '', output_key TEXT NOT NULL, managed_path TEXT NOT NULL, original_path TEXT NOT NULL, media_type TEXT NOT NULL, size_bytes INTEGER NOT NULL, digest TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT (datetime('now')))`,
		`INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status)
		 VALUES ('wr-legacy', 'legacy', 'Legacy', 'test', 'sha', '{}', 'succeeded')`,
		`PRAGMA user_version=40`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := New(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer store.Close()
	view, err := store.GetWorkflowRun(context.Background(), "wr-legacy")
	if err != nil {
		t.Fatalf("read legacy workflow: %v", err)
	}
	if view.Run.RelatedTaskID != "" {
		t.Fatalf("related task = %q", view.Run.RelatedTaskID)
	}
}
