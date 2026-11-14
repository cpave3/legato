package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestOnAMacSandboxedClaudeCodeLogsInWithTheConfiguredToken(t *testing.T) {
	home := t.TempDir()
	noEnv := func(string) string { return "" }
	resolve := func(_ context.Context, ref string) (string, error) {
		if ref == "legato://claude-code-token" {
			return "sk-ant-oat01-x\n", nil
		}
		return "", errors.New("no such secret")
	}
	env, err := sandboxToolLogin("legato://claude-code-token", resolve, home, noEnv)(context.Background(), "claude-code")
	if err != nil || env["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-oat01-x" {
		t.Fatalf("env %v, %v", env, err)
	}
	if _, err := sandboxToolLogin("legato://missing", resolve, home, noEnv)(context.Background(), "claude-code"); err == nil {
		t.Error("a token that cannot be read was not reported")
	}
	// Without a token, a login Claude Code can reach inside is enough.
	if _, err := sandboxToolLogin("", resolve, home, noEnv)(context.Background(), "claude-code"); err == nil || !strings.Contains(err.Error(), "claude setup-token") {
		t.Errorf("no login: %v", err)
	}
	if env, err := sandboxToolLogin("", resolve, home, func(k string) string {
		return map[string]string{"ANTHROPIC_API_KEY": "sk-ant-api"}[k]
	})(context.Background(), "claude-code"); err != nil || len(env) != 0 {
		t.Errorf("an API key in the environment: %v, %v", env, err)
	}
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte("{}"), 0o600)
	if _, err := sandboxToolLogin("", resolve, home, noEnv)(context.Background(), "claude-code"); err != nil {
		t.Errorf("a credentials file: %v", err)
	}
}

func TestOnAMacSandboxedCodexIsRefusedWhenItsLoginIsInTheKeychain(t *testing.T) {
	home := t.TempDir()
	login := sandboxToolLogin("", nil, home, func(string) string { return "" })
	if _, err := login(context.Background(), "codex"); err != nil {
		t.Fatalf("no config: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt\"\ncli_auth_credentials_store = \"keyring\"\n"), 0o600)
	if _, err := login(context.Background(), "codex"); err == nil || !strings.Contains(err.Error(), `"file"`) {
		t.Fatalf("keyring: %v", err)
	}
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600)
	if _, err := login(context.Background(), "codex"); err != nil {
		t.Fatalf("file: %v", err)
	}
	if env, err := login(context.Background(), "chimera"); err != nil || len(env) != 0 {
		t.Fatalf("chimera: %v %v", env, err)
	}
}

// A worker accepts handed-off tasks only when its own sandbox can run: the
// configured backend, not bwrap by name.
func TestAWorkerAsksItsOwnBackendWhetherTheSandboxCanRun(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Sandbox.Enabled = true
	cfg.Agents.Sandbox.Backend = "seatbelt"
	if got := sandboxBackend(cfg).Name(); got != "seatbelt" {
		t.Fatalf("backend %s", got)
	}
	if runtime.GOOS != "darwin" && sandboxAvailable(cfg) {
		t.Error("Seatbelt is said to run on " + runtime.GOOS)
	}
	cfg.Agents.Sandbox.Backend = "bwrap"
	if got := sandboxBackend(cfg).Name(); got != "bwrap" {
		t.Fatalf("backend %s", got)
	}
	cfg.Agents.Sandbox.Enabled = false
	if sandboxAvailable(cfg) {
		t.Error("available with the sandbox off")
	}
}
