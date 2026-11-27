package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Mount a synthetic policy over /etc only in the test subprocess. The host's
// policy, login and tmux server are never changed or contacted.
func TestCommandAgentsRefuseManagedAuthForNamedSubscription(t *testing.T) {
	if os.Getenv("LEGATO_POLICY_TEST_CHILD") != "1" {
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			t.Skip("bwrap not installed")
		}
		if err := exec.Command(bwrap, "--ro-bind", "/", "/", "true").Run(); err != nil {
			t.Skipf("bwrap unavailable: %v", err)
		}
		root := t.TempDir()
		etc := filepath.Join(root, "etc")
		policy := filepath.Join(etc, "claude-code")
		if err := os.MkdirAll(policy, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(policy, "managed-settings.json"), []byte(`{"apiKeyHelper":"fixture-policy-secret"}`), 0600); err != nil {
			t.Fatal(err)
		}
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bwrap, "--ro-bind", "/", "/", "--tmpfs", "/tmp", "--ro-bind", etc, "/etc", "--dev", "/dev", "--ro-bind", binary, "/tmp/account-test", "/tmp/account-test", "-test.run=^TestCommandAgentsRefuseManagedAuthForNamedSubscription$", "-test.v")
		cmd.Env = append(os.Environ(), "LEGATO_POLICY_TEST_CHILD=1", "TMPDIR=/tmp")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command factory policy check: %v\n%s", err, out)
		}
		return
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_PROFILE", "ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(key, "")
	}
	bin := filepath.Join(root, "bin")
	account := filepath.Join(root, "work")
	cwd := filepath.Join(root, "repo")
	for _, dir := range []string{bin, account, cwd} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"claude", "tmux"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho fixture\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	if err := os.WriteFile(filepath.Join(account, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"synthetic-only"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf("data_dir: %q\ndb: {path: %q}\nagents: {default_tool: claude-code, peer_awareness: false}\nclaude_accounts: [{name: work, config_dir: %q}]\n", filepath.Join(root, "data"), filepath.Join(root, "store.db"), account))
	cfg, err := config.LoadBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", path)
	db, err := store.New(cfg.DB.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "policy-test", Title: "Synthetic policy test", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	agents, _, err := commandAgentService(cfg, db, cwd, nil, events.New(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = agents.SpawnAgent(ctx, "policy-test", 0, 0, service.AgentSpawnOptions{AgentKind: "claude-code", Account: "work", WorkingDir: cwd})
	if err == nil || !strings.Contains(err.Error(), "apiKeyHelper") || strings.Contains(err.Error(), "fixture-policy-secret") {
		t.Fatalf("managed policy was not refused with sanitized guidance: %v", err)
	}
}
