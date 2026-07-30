package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestColumnsSyncCommandReconcilesConfigAndReportsJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Backlog", "Doing", "Done"} {
		if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{
			ColumnName: name, RemoteStatuses: `[]`, SortOrder: i,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Active", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := "db:\n  path: " + dbPath + "\nboard:\n  columns:\n" +
		"    - name: Backlog\n      remote_statuses: [To Do]\n" +
		"    - name: Triage\n" +
		"    - name: Unmapped\n      remote_statuses: [Unknown]\n" +
		"    - name: Done\n      remote_transition_id: \"31\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)

	output := captureStdout(t, func() int {
		return runCLI([]string{"columns", "sync", "--json"})
	})
	var envelope struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    struct {
			Fallback   string   `json:"fallback"`
			Added      []string `json:"added"`
			Removed    []string `json:"removed"`
			TasksMoved int64    `json:"tasks_moved"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, output)
	}
	if !envelope.OK || envelope.Command != "columns.sync" ||
		envelope.Data.Fallback != "Unmapped" || envelope.Data.TasksMoved != 1 {
		t.Fatalf("envelope = %#v", envelope)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	columns, err := s.ListColumnMappings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 4 || columns[1].ColumnName != "Triage" ||
		columns[3].RemoteTransition != "31" ||
		columns[0].RemoteStatuses != `["To Do"]` {
		t.Fatalf("columns = %#v", columns)
	}
	task, err := s.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Unmapped" {
		t.Fatalf("task status = %q, want Unmapped", task.Status)
	}
}
