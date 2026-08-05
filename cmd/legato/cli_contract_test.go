package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func captureStderrWithCode(t *testing.T, run func() int) (string, int) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := run()
	w.Close()
	os.Stderr = old
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	return buffer.String(), code
}

func TestParseCommandArgsRejectsUnknownAndDuplicateFlags(t *testing.T) {
	specs := map[string]flagSpec{"status": {}}
	if _, err := parseCommandArgs([]string{"--wat", "x"}, specs); err == nil || err.Code != "unknown_flag" {
		t.Fatalf("unknown flag error = %#v", err)
	}
	if _, err := parseCommandArgs([]string{"--status", "Doing", "--status=Done"}, specs); err == nil || err.Code != "duplicate_flag" {
		t.Fatalf("duplicate flag error = %#v", err)
	}
}

func TestReviewAndSwarmParsersRejectUnknownFlags(t *testing.T) {
	if _, _, _, err := parseReviewArgs([]string{"--wat", "value"}); err == nil {
		t.Fatal("review parser accepted unknown flag")
	}
	if _, _, _, err := parseMessageArgs([]string{"task-1", "message", "--wat"}); err == nil {
		t.Fatal("swarm message parser accepted unknown flag")
	}
	if _, _, err := parseSwarmCreateArgs([]string{"goal", "--wat"}); err == nil {
		t.Fatal("swarm create parser accepted unknown flag")
	}
}

func TestReadTextInputPreservesMultilineStdin(t *testing.T) {
	got, err := readTextInput("", false, "-", true, bytes.NewBufferString("one\n\ntwo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "one\n\ntwo\n" {
		t.Fatalf("description = %q", got)
	}
}

func TestTaskCommandRejectsUnsupportedConfiguredDatabaseDriver(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  driver: mysql\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)

	out, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"--json", "task", "list"})
	})
	if code != exitEnvironment {
		t.Fatalf("exit code = %d, want %d", code, exitEnvironment)
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if envelope.OK || envelope.Error.Code != "database_unavailable" || envelope.Error.Message != `database driver "mysql" is not supported` {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestTaskCreateUsesConfiguredWorkspaceDefaultAndPreservesOverrides(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantWorkspace string
	}{
		{name: "omitted workspace uses default", args: []string{"task", "create", "Default task", "--json"}, wantWorkspace: "Personal"},
		{name: "explicit workspace wins", args: []string{"task", "create", "Work task", "--workspace", "Work", "--json"}, wantWorkspace: "Work"},
		{name: "explicit none stays unassigned", args: []string{"task", "create", "Unassigned task", "--workspace", "none", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "legato.db")
			s, err := store.New(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: "Backlog"}); err != nil {
				t.Fatal(err)
			}
			s.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			configYAML := "db:\n  path: " + dbPath + "\nworkspaces:\n  - name: Work\n  - name: Personal\ndefault_workspace: Personal\n"
			if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LEGATO_CONFIG", configPath)

			out := captureStdout(t, func() int { return runCLI(tt.args) })
			var envelope struct {
				OK   bool `json:"ok"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatalf("JSON: %v\n%s", err, out)
			}
			if !envelope.OK || envelope.Data.ID == "" {
				t.Fatalf("envelope = %#v", envelope)
			}

			s, err = store.New(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			workspaces, err := s.ListWorkspaces(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var wantID *int
			for _, workspace := range workspaces {
				if workspace.Name == tt.wantWorkspace {
					id := workspace.ID
					wantID = &id
				}
			}
			created, err := s.GetTask(context.Background(), envelope.Data.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantWorkspace == "" {
				if created.WorkspaceID != nil {
					t.Fatalf("WorkspaceID = %v, want nil", created.WorkspaceID)
				}
			} else if wantID == nil || created.WorkspaceID == nil || *created.WorkspaceID != *wantID {
				t.Fatalf("WorkspaceID = %v, want %v", created.WorkspaceID, wantID)
			}
		})
	}
}

func TestTaskUpdateJSONUsesEnvironmentTaskAndDescriptionFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Backlog", "Doing"} {
		if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Before", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	descriptionPath := filepath.Join(t.TempDir(), "description.md")
	const description = "# Details\n\nPreserve this newline.\n"
	if err := os.WriteFile(descriptionPath, []byte(description), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "task-1")

	out := captureStdout(t, func() int {
		return runCLI([]string{"--json", "task", "update", "--status", "Doing", "--description-file", descriptionPath})
	})
	var envelope struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    struct {
			Status        string `json:"status"`
			DescriptionMD string `json:"description_md"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if !envelope.OK || envelope.Command != "task.update" || envelope.Data.Status != "Doing" || envelope.Data.DescriptionMD != description {
		t.Fatalf("envelope = %#v", envelope)
	}
}
