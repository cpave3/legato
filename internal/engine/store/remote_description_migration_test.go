package store

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestRemoteDescriptionMigrationSeparatesProviderAndLocalDescriptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-remote-description.db")
	db, err := sqlx.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range migrations {
		if i >= 56 {
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
	if _, err := db.Exec(`
		INSERT INTO tasks (id, title, description, description_md, status, provider, remote_id)
		VALUES ('local-1', 'Local', 'Local plain', 'Local **Markdown**', 'Backlog', NULL, NULL),
		       ('jira-1', 'Remote', 'Provider plain', 'Provider **Markdown**', 'Backlog', 'jira', 'JIRA-1');
		PRAGMA user_version = 56;
	`); err != nil {
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

	local, err := s.GetTask(t.Context(), "local-1")
	if err != nil {
		t.Fatal(err)
	}
	if local.Description != "Local plain" || local.DescriptionMD != "Local **Markdown**" || local.RemoteDescriptionMD != "" {
		t.Fatalf("local descriptions = %q / %q / %q", local.Description, local.DescriptionMD, local.RemoteDescriptionMD)
	}

	remote, err := s.GetTask(t.Context(), "jira-1")
	if err != nil {
		t.Fatal(err)
	}
	if remote.Description != "" || remote.DescriptionMD != "" || remote.RemoteDescriptionMD != "Provider **Markdown**" {
		t.Fatalf("remote descriptions = %q / %q / %q", remote.Description, remote.DescriptionMD, remote.RemoteDescriptionMD)
	}
	if remote.Title != "Remote" || remote.Status != "Backlog" || remote.Provider == nil || *remote.Provider != "jira" {
		t.Fatalf("unrelated remote data changed: %#v", remote)
	}
}
