package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A name saved in Settings is what the running process calls its Reeve at
// once, without a restart, and a name the setting refuses is never stored.
func TestASavedReeveNameShowsWithoutARestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", filepath.Join(dir, "config.yaml"))
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	name := reeveNameFor(db)
	if got := name(ctx).Start(); got != "The Reeve" {
		t.Fatalf("before = %q", got)
	}
	if err := settingsFor(db).Set(ctx, "reeve.name", json.RawMessage(`"Tessitura"`), "test"); err != nil {
		t.Fatal(err)
	}
	if got := name(ctx).Start(); got != "Tessitura" {
		t.Fatalf("after saving = %q", got)
	}
	if err := settingsFor(db).Set(ctx, "reeve.name", json.RawMessage(`"Tess\n# heading"`), "test"); err == nil {
		t.Fatal("saved a name with a line break")
	}
	if got := name(ctx).Start(); got != "Tessitura" {
		t.Fatalf("after a refused name = %q", got)
	}
}
