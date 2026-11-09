package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// A restart that moved the database settles in the database the pointer
// names.
func TestSettlingUsesThePointersDatabase(t *testing.T) {
	f := newRollbackFixture(t)
	rec := f.begin(t)
	movedPath := filepath.Join(t.TempDir(), "moved.db")
	other, err := store.New(movedPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	moved := &RestartRollback{Store: other, Settings: f, DBDriver: "sqlite", DBPath: movedPath}

	outcome, err := moved.Settle(context.Background(), f.configPath, func(context.Context) error { return errors.New("down") }, 20*time.Millisecond, 5*time.Millisecond)
	if err != nil || outcome != store.RestartRolledBack {
		t.Fatalf("Settle = %q, %v", outcome, err)
	}
	got, _ := f.db.GetRestartRecord(context.Background(), rec.ID)
	if got.State != store.RestartRolledBack {
		t.Fatalf("the record in the pointer's database = %+v", got)
	}
}

// A pointer whose record cannot be found is not consumed.
func TestAPointerWhoseRecordIsMissingIsKept(t *testing.T) {
	f := newRollbackFixture(t)
	f.begin(t)
	emptyPath := filepath.Join(t.TempDir(), "empty.db")
	other, err := store.New(emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	p, _ := ReadRestartPointer(f.configPath)
	p.DBPath = emptyPath
	raw, _ := json.Marshal(p)
	os.WriteFile(RestartPointerPath(f.configPath), raw, 0o600)
	lost := &RestartRollback{Store: other, Settings: f, DBDriver: "sqlite", DBPath: emptyPath}

	if _, err := lost.Settle(context.Background(), f.configPath, func(context.Context) error { return errors.New("down") }, 10*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("settled a restart with no record")
	}
	if _, ok := ReadRestartPointer(f.configPath); !ok {
		t.Fatal("the pointer was consumed though its record was not found")
	}
}
