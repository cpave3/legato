package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// LEG-668: an agent a command starts (legato workflow …, legato swarm …)
// gets the host broker as one serve or the TUI starts does. It used to get
// none, which the sandbox now refuses.
func TestACommandsSandboxedAgentGetsTheHostBroker(t *testing.T) {
	root := t.TempDir()
	bin, cwd, runtime, home := filepath.Join(root, "bin"), filepath.Join(root, "work"), filepath.Join(root, "run"), filepath.Join(root, "home")
	for _, dir := range []string{bin, cwd, runtime, home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tmuxLog := filepath.Join(root, "tmux.log")
	scripts := map[string]string{
		"tmux":   fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\n", tmuxLog),
		"bwrap":  "#!/bin/sh\n",
		"claude": "#!/bin/sh\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	raw := []byte(fmt.Sprintf("data_dir: %q\ndb: {path: %q}\nagents: {default_tool: claude-code, peer_awareness: false, sandbox: {enabled: true}}\n", filepath.Join(root, "data"), filepath.Join(root, "store.db")))
	cfg, err := config.LoadBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(cfg.DB.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "cmd-task", Title: "Command agent", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	agents, _, err := commandAgentService(cfg, db, cwd, nil, events.New(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = agents.SpawnAgent(ctx, "cmd-task", 0, 0, service.AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd, WorkflowRunID: "wr-cmd"})
	if errors.Is(err, service.ErrSandboxNoBroker) {
		t.Fatalf("a command's sandboxed agent was refused for want of a broker: %v", err)
	}
	logged, _ := os.ReadFile(tmuxLog)
	if !strings.Contains(string(logged), "LEGATO_BROKER_SOCKET="+hostBrokerSocket()) || !strings.Contains(string(logged), "LEGATO_BROKER_TOKEN=lgb_") {
		t.Fatalf("the session was not given the host broker (spawn err %v); tmux was called with:\n%s", err, logged)
	}
}
