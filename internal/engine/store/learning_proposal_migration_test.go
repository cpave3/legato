package store

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestMigration052AddsRevisionContentToExistingLearningProposals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-revision.db")
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE learnings (id TEXT PRIMARY KEY);
	CREATE TABLE learning_proposals (
		id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, operation TEXT NOT NULL,
		learning_id TEXT NOT NULL DEFAULT '', canonical_learning_id TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '', project_key TEXT NOT NULL DEFAULT '', evidence_ids_json TEXT NOT NULL DEFAULT '[]',
		reason TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending', revision INTEGER NOT NULL DEFAULT 1,
		target_learning_revision INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	); INSERT INTO learning_proposals (id,generation_id,operation) VALUES ('p','g','discard'); PRAGMA user_version = 51;`)
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
	var proposal LearningProposal
	if err := s.DB().Get(&proposal, `SELECT * FROM learning_proposals WHERE id = 'p'`); err != nil {
		t.Fatal(err)
	}
	if proposal.Claim != "" || proposal.Rationale != "" || proposal.Kind != "" {
		t.Fatalf("revision fields = %+v", proposal)
	}
}

func TestMigration050BackfillsExistingLearningProposals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE learning_proposals (
		id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, operation TEXT NOT NULL,
		learning_id TEXT NOT NULL DEFAULT '', canonical_learning_id TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '', project_key TEXT NOT NULL DEFAULT '', evidence_ids_json TEXT NOT NULL DEFAULT '[]',
		reason TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending', revision INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
	); INSERT INTO learning_proposals (id,generation_id,operation) VALUES ('p','g','discard'); PRAGMA user_version = 49;`)
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
	var revision int
	if err := s.DB().Get(&revision, `SELECT target_learning_revision FROM learning_proposals WHERE id = 'p'`); err != nil {
		t.Fatal(err)
	}
	if revision != 0 {
		t.Fatalf("target_learning_revision = %d", revision)
	}
}
