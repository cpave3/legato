package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// TestMigration014RewritesV0StatusValues verifies that migration 014 rewrites
// historical v0 status values to their v1 equivalents:
//
//	building → in_progress
//	review   → reporting
//	rejected → cancelled
//
// queued and done are unchanged. This is the regression test for the case
// reviewed in the swarm-conductor change — without it, a v0 → v1 upgrade
// could silently leave subtasks frozen with stale status strings.
func TestMigration014RewritesV0StatusValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v0.db")

	// Step 1: open raw sqlx, apply migrations 001..013 (everything before 014).
	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}

	preMigrations := []string{
		"001_init.sql", "002_stale_and_move_tracking.sql", "003_rename_jira_to_remote.sql",
		"004_agent_sessions.sql", "005_tasks.sql", "006_agent_activity.sql",
		"007_state_intervals.sql", "008_workspaces.sql", "009_archive.sql",
		"010_pr_meta.sql", "011_ephemeral.sql", "012_swarm.sql", "013_agent_role.sql",
	}
	for _, name := range preMigrations {
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("exec %s: %v", name, err)
		}
	}

	// Step 2: seed parent task + subtasks with v0 status values.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, description, description_md, status, priority, sort_order, created_at, updated_at)
		VALUES ('parent-1', 'Parent', '', '', 'Doing', '', 0, datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	v0Cases := []struct{ id, status string }{
		{"st-aa01", "queued"},
		{"st-aa02", "building"},
		{"st-aa03", "review"},
		{"st-aa04", "done"},
		{"st-aa05", "rejected"},
	}
	for _, c := range v0Cases {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO swarm_subtasks (id, parent_task_id, title, description, scope_globs, role, status, created_at)
			VALUES (?, 'parent-1', ?, '', '[]', 'builder', ?, datetime('now'))`,
			c.id, "Sub "+c.id, c.status); err != nil {
			t.Fatalf("insert %s: %v", c.id, err)
		}
	}

	// Step 3: apply migration 014.
	data, err := migrationsFS.ReadFile("migrations/014_swarm_v1.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(data)); err != nil {
		t.Fatalf("exec 014: %v", err)
	}

	// Step 4: assert each row now has the v1 status.
	want := map[string]string{
		"st-aa01": "queued",      // unchanged
		"st-aa02": "in_progress", // building → in_progress
		"st-aa03": "reporting",   // review   → reporting
		"st-aa04": "done",        // unchanged
		"st-aa05": "cancelled",   // rejected → cancelled
	}
	for id, expected := range want {
		var got string
		if err := db.GetContext(ctx, &got, "SELECT status FROM swarm_subtasks WHERE id = ?", id); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if got != expected {
			t.Errorf("row %s: status = %q, want %q", id, got, expected)
		}
	}
}
