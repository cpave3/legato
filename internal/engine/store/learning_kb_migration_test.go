package store

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestMigrationFromVersion52PreservesLearningsAndAddsKBStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE learnings (
		id TEXT PRIMARY KEY, scope TEXT NOT NULL, project_key TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
		claim TEXT NOT NULL, rationale TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'candidate',
		confidence REAL NOT NULL DEFAULT 0, prompt_enabled INTEGER NOT NULL DEFAULT 0, prompt_decided INTEGER NOT NULL DEFAULT 0,
		revision INTEGER NOT NULL DEFAULT 1, curator_run_id TEXT NOT NULL DEFAULT '', superseded_by TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	); INSERT INTO learnings (id,scope,kind,claim) VALUES ('legacy-learning','user','fact','preserved'); PRAGMA user_version=52;`)
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
	var learning struct{ Claim, Category string }
	if err := s.DB().Get(&learning, `SELECT claim, category FROM learnings WHERE id='legacy-learning'`); err != nil {
		t.Fatal(err)
	}
	if learning.Claim != "preserved" || learning.Category != "" {
		t.Fatalf("learning=%+v", learning)
	}
	for _, table := range []string{"learning_tags", "learning_embeddings"} {
		var count int
		if err := s.DB().Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
}
