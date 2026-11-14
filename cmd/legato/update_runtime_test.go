package main

import (
	"context"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestHowAnUpdatedLegatoRestarts(t *testing.T) {
	const pid = 4242
	cases := []struct {
		name          string
		env           map[string]string
		wantRestarts  bool
		wantSignalled bool
	}{
		{"under the hot-reload supervisor, which restarts on a new binary", map[string]string{hotReloadChildEnv: "1"}, true, false},
		{"as systemd's service, which starts it again", map[string]string{"SYSTEMD_EXEC_PID": strconv.Itoa(pid)}, true, true},
		{"from a shell inside a systemd service (a terminal)", map[string]string{"SYSTEMD_EXEC_PID": "17"}, false, false},
		{"by hand", nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requested atomic.Bool
			signalled := make(chan struct{}, 1)
			restart := processRestarter(func(k string) string { return c.env[k] }, pid, func() { signalled <- struct{}{} }, &requested)

			if got := restart(); got != c.wantRestarts {
				t.Errorf("restarts = %v, want %v", got, c.wantRestarts)
			}
			if requested.Load() != c.wantSignalled {
				t.Errorf("exit for a restart = %v, want %v", requested.Load(), c.wantSignalled)
			}
			if c.wantSignalled {
				<-signalled // shuts down as on SIGTERM
			}
		})
	}
}

func TestRuntimeLooksAfterSTAndChimera(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	updates := newUpdateService(db, nil, nil, nil)
	got := updates.Status(context.Background()).Tools
	if len(got) != 2 || got[0].Name != "st" || got[1].Name != "chimera" {
		t.Fatalf("runtime tools = %+v", got)
	}
}
