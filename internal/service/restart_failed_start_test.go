package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A start that fails before it is healthy (a port taken, say) does not wait
// for the health deadline: the restart is rolled back at once.
func TestAStartThatFailsAfterARestartIsRolledBackAtOnce(t *testing.T) {
	f := newRollbackFixture(t)
	rec := f.begin(t)

	rolled, err := f.rb.RollBackFailedStart(context.Background(), f.configPath, errors.New("listen tcp :3080: bind: address already in use"))
	if err != nil || !rolled {
		t.Fatalf("RollBackFailedStart = %v, %v", rolled, err)
	}
	if got := f.config(t); got != "theme: old\n" {
		t.Fatalf("config.yaml = %q, want the backup", got)
	}
	if len(f.restored) != 1 || f.restored[0].Key != "updates.mode" {
		t.Fatalf("settings restored = %+v", f.restored)
	}
	got, _ := f.db.GetRestartRecord(context.Background(), rec.ID)
	if got.State != store.RestartRolledBack || !strings.Contains(got.Failure, "address already in use") {
		t.Fatalf("record = %+v", got)
	}
	if _, ok := ReadRestartPointer(f.configPath); ok {
		t.Fatal("the pointer is still there")
	}
	// Nothing pending: nothing to do.
	if rolled, err := f.rb.RollBackFailedStart(context.Background(), f.configPath, errors.New("again")); err != nil || rolled {
		t.Fatalf("with nothing pending: %v, %v", rolled, err)
	}
}
