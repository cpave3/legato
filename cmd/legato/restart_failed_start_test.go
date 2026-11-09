package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The TUI or serve that exits with an error while a restart is pending
// rolls it back and starts again, instead of staying down on the change.
func TestAStartThatExitsWithAnErrorAfterARestartRollsItBack(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	backup := configPath + ".bak"
	os.WriteFile(backup, []byte("theme: old\n"), 0o600)
	os.WriteFile(configPath, []byte("web:\n  port: \"22\"\n"), 0o600)
	dbPath := filepath.Join(dir, "legato.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	rb := &service.RestartRollback{Store: db, DBDriver: "sqlite", DBPath: dbPath}
	if _, err := rb.Begin(context.Background(), service.RestartPlan{Reason: "config edit", ConfigPath: configPath, ConfigBackup: backup}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// A CLI command that fails is not a start: nothing happens.
	if rollBackFailedStart([]string{"task", "list"}, 1, configPath, func() error { t.Fatal("restarted after a CLI command"); return nil }) {
		t.Fatal("a CLI command rolled the restart back")
	}
	// A clean exit is not a failure either.
	if rollBackFailedStart(nil, 0, configPath, func() error { t.Fatal("restarted after a clean exit"); return nil }) {
		t.Fatal("a clean exit rolled the restart back")
	}

	again := 0
	if !rollBackFailedStart([]string{"serve"}, 1, configPath, func() error { again++; return nil }) || again != 1 {
		t.Fatalf("a failed serve: started again %d times", again)
	}
	if b, _ := os.ReadFile(configPath); string(b) != "theme: old\n" {
		t.Fatalf("config.yaml = %q, want the backup", b)
	}
	if _, ok := service.ReadRestartPointer(configPath); ok {
		t.Fatal("the restart is still pending")
	}
}
