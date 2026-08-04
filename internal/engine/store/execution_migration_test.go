package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestExecutionActivityErrorMigrationPreservesRowsAndIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-error.db")
	db, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range migrations {
		if i >= 55 {
			break
		}
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sqlText, err := prepareMigration(db, name, string(data))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(sqlText); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	_, err = db.Exec(`
		INSERT INTO executions (id, kind, status, task_id, execution_task_id, task_title, started_at)
		VALUES ('exec-preserved', 'interactive', 'running', 'task-1', 'task-1', 'Task', '2026-08-03 01:00:00');
		INSERT INTO execution_activity (execution_id, state, started_at, working_dir)
		VALUES ('exec-preserved', 'working', '2026-08-03 01:00:00', '/repo');
		PRAGMA user_version = 55;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var state string
	if err := s.db.Get(&state, `SELECT state FROM execution_activity WHERE execution_id='exec-preserved'`); err != nil || state != ActivityWorking {
		t.Fatalf("preserved state=%q err=%v", state, err)
	}
	if err := s.RecordExecutionActivity(context.Background(), "exec-preserved", ActivityError, "/repo"); err != nil {
		t.Fatalf("record error after migration: %v", err)
	}
	for _, index := range []string{"idx_execution_activity_execution_time", "idx_execution_activity_range", "idx_execution_activity_one_open"} {
		var count int
		if err := s.db.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, index); err != nil || count != 1 {
			t.Fatalf("index %s count=%d err=%v", index, count, err)
		}
	}
}

func TestExecutionMigrationDropsLegacyActivityAndPreservesDomainHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range migrations {
		if i >= 54 {
			break
		}
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sqlText, err := prepareMigration(db, name, string(data))
		if err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
		if _, err := db.Exec(sqlText); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	_, err = db.Exec(`
		INSERT INTO tasks (id, title, status) VALUES ('legacy-task', 'Preserved task', 'Done');
		INSERT INTO state_intervals (task_id, state, started_at) VALUES ('legacy-task', 'working', '2026-01-01 00:00:00');
		INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status, task_id)
		VALUES ('legacy-run', 'test', 'Preserved workflow', '/tmp/test.yaml', 'digest', '{}', 'succeeded', 'legacy-task');
		PRAGMA user_version = 54;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second)
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after := time.Now().UTC().Add(time.Second)

	var count int
	if err := s.db.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='state_intervals'`); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("state_intervals still exists after lossy migration")
	}
	for _, table := range []string{"executions", "execution_activity", "reporting_metadata"} {
		if err := s.db.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
	if err := s.db.Get(&count, `SELECT COUNT(*) FROM tasks WHERE id='legacy-task'`); err != nil || count != 1 {
		t.Fatalf("task history count=%d err=%v", count, err)
	}
	if err := s.db.Get(&count, `SELECT COUNT(*) FROM workflow_runs WHERE id='legacy-run'`); err != nil || count != 1 {
		t.Fatalf("workflow history count=%d err=%v", count, err)
	}
	var epoch time.Time
	if err := s.db.Get(&epoch, `SELECT activity_epoch FROM reporting_metadata WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if epoch.Before(before) || epoch.After(after) {
		t.Fatalf("activity epoch %v outside migration window %v..%v", epoch, before, after)
	}
}
