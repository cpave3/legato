package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/cpave3/legato/config"
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

func TestRuntimeLooksAfterSTChimeraAndClaudeCode(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	updates := newUpdateService(cfg, db, nil, nil, nil)
	got := updates.Status(context.Background()).Tools
	if len(got) != 3 || got[0].Name != "st" || got[1].Name != "chimera" || got[2].Name != "claude" {
		t.Fatalf("runtime tools = %+v", got)
	}
}

func TestAgentsLaunchLegatosOwnClaudeOnceItHasOne(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	if cmd, managed := claudeLaunch(cfg)(); cmd != "claude" || managed {
		t.Fatalf("before Legato has a claude: %q, %v", cmd, managed)
	}
	folder := claudeToolFolder(cfg)
	if err := os.MkdirAll(folder.VersionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder.VersionsDir(), "2.1.326"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := folder.SetCurrent("2.1.326"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cfg.DataDir, "tools", "claude", "claude")
	if cmd, managed := claudeLaunch(cfg)(); cmd != want || !managed {
		t.Fatalf("launch = %q, %v; want %s", cmd, managed, want)
	}
	cfg.Updates.Claude.Command = "/opt/claude"
	if cmd, managed := claudeLaunch(cfg)(); cmd != "/opt/claude" || managed {
		t.Fatalf("with an override: %q, %v", cmd, managed)
	}
}
