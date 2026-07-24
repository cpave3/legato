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

func TestParseCommandArgsRejectsUnknownAndDuplicateFlags(t *testing.T) {
	specs := map[string]flagSpec{"status": {}}
	if _, err := parseCommandArgs([]string{"--wat", "x"}, specs); err == nil || err.Code != "unknown_flag" {
		t.Fatalf("unknown flag error = %#v", err)
	}
	if _, err := parseCommandArgs([]string{"--status", "Doing", "--status=Done"}, specs); err == nil || err.Code != "duplicate_flag" {
		t.Fatalf("duplicate flag error = %#v", err)
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
