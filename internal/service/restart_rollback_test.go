package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

type rollbackFixture struct {
	db         *store.Store
	dbPath     string
	configPath string
	backup     string
	rb         *RestartRollback
	restored   []RestartSetting
}

func (f *rollbackFixture) RestoreSettings(_ context.Context, s []RestartSetting) error {
	f.restored = append(f.restored, s...)
	return nil
}

// newRollbackFixture is an instance whose config.yaml was edited (to "new")
// from a good file (kept as the backup, "old").
func newRollbackFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	dir := t.TempDir()
	f := &rollbackFixture{dbPath: filepath.Join(dir, "legato.db"), configPath: filepath.Join(dir, "config.yaml"), backup: filepath.Join(dir, "config.yaml.20261109T120000.bak")}
	db, err := store.New(f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f.db = db
	os.WriteFile(f.backup, []byte("theme: old\n"), 0o600)
	os.WriteFile(f.configPath, []byte("theme: new\n"), 0o600)
	f.rb = &RestartRollback{Store: db, Settings: f, DBDriver: "sqlite", DBPath: f.dbPath}
	return f
}

func (f *rollbackFixture) begin(t *testing.T) store.RestartRecord {
	t.Helper()
	old := json.RawMessage(`"ask"`)
	rec, err := f.rb.Begin(context.Background(), RestartPlan{Reason: "move settings", RequestedBy: "the Reeve", ConfigPath: f.configPath, ConfigBackup: f.backup,
		Settings: []RestartSetting{{Key: "updates.mode", Old: old}}})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func (f *rollbackFixture) config(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(f.configPath)
	return string(b)
}

func TestOnlyOneRestartIsPendingAtATime(t *testing.T) {
	f := newRollbackFixture(t)
	f.begin(t)
	if _, err := f.rb.Begin(context.Background(), RestartPlan{ConfigPath: f.configPath}); !errors.Is(err, ErrRestartPending) {
		t.Fatalf("second Begin: %v", err)
	}
}

func TestAStartThatCannotLoadItsConfigurationPutsTheOldFileBack(t *testing.T) {
	f := newRollbackFixture(t)
	rec := f.begin(t)
	f.db.Close()

	rolled, err := RollBackUnloadable(context.Background(), f.configPath, errors.New("yaml: line 1: did not find expected key"), nil)
	if err != nil || !rolled {
		t.Fatalf("RollBackUnloadable = %v, %v", rolled, err)
	}
	if got := f.config(t); got != "theme: old\n" {
		t.Fatalf("config.yaml = %q, want the backup", got)
	}
	db, _ := store.New(f.dbPath)
	defer db.Close()
	got, _ := db.GetRestartRecord(context.Background(), rec.ID)
	if got.State != store.RestartRolledBack || !strings.Contains(got.Failure, "did not find expected key") {
		t.Fatalf("record = %+v", got)
	}
	if _, ok := ReadRestartPointer(f.configPath); ok {
		t.Fatal("the pointer is still there")
	}
}

func TestAHealthyStartAppliesTheRestart(t *testing.T) {
	f := newRollbackFixture(t)
	rec := f.begin(t)

	outcome, err := f.rb.Settle(context.Background(), f.configPath, func(context.Context) error { return nil }, time.Second, 10*time.Millisecond)
	if err != nil || outcome != store.RestartApplied {
		t.Fatalf("Settle = %q, %v", outcome, err)
	}
	if got := f.config(t); got != "theme: new\n" {
		t.Fatalf("config.yaml = %q, want the new file kept", got)
	}
	got, _ := f.db.GetRestartRecord(context.Background(), rec.ID)
	if got.State != store.RestartApplied {
		t.Fatalf("record = %+v", got)
	}
}

func TestAStartNotHealthyInTimeIsRolledBack(t *testing.T) {
	f := newRollbackFixture(t)
	rec := f.begin(t)

	outcome, err := f.rb.Settle(context.Background(), f.configPath, func(context.Context) error { return errors.New("/health did not answer") }, 50*time.Millisecond, 10*time.Millisecond)
	if err != nil || outcome != store.RestartRolledBack {
		t.Fatalf("Settle = %q, %v", outcome, err)
	}
	if got := f.config(t); got != "theme: old\n" {
		t.Fatalf("config.yaml = %q, want the backup", got)
	}
	if len(f.restored) != 1 || f.restored[0].Key != "updates.mode" || string(f.restored[0].Old) != `"ask"` {
		t.Fatalf("settings restored = %+v", f.restored)
	}
	got, _ := f.db.GetRestartRecord(context.Background(), rec.ID)
	if got.State != store.RestartRolledBack || !strings.Contains(got.Failure, "/health did not answer") {
		t.Fatalf("record = %+v", got)
	}
}

func TestWithNoPendingRestartThereIsNothingToSettle(t *testing.T) {
	f := newRollbackFixture(t)
	outcome, err := f.rb.Settle(context.Background(), f.configPath, func(context.Context) error { return errors.New("never") }, 20*time.Millisecond, 5*time.Millisecond)
	if err != nil || outcome != "" {
		t.Fatalf("Settle = %q, %v", outcome, err)
	}
}

func TestARolledBackRestartIsInAttention(t *testing.T) {
	f := newRollbackFixture(t)
	f.begin(t)
	f.rb.Settle(context.Background(), f.configPath, func(context.Context) error { return errors.New("the broker did not answer") }, 20*time.Millisecond, 5*time.Millisecond)

	items, err := NewRestartAttentionAdapter(f.db).Snapshot(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, %v", items, err)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalRestartRolledBack || !strings.Contains(item.Title, "rolled back") || !strings.Contains(item.Detail, "the broker did not answer") || !strings.Contains(item.Reason, "move settings") {
		t.Fatalf("item = %+v", item)
	}
}
