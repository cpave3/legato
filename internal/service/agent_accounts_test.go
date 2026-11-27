package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// The fake terminal executes the service's real launch line in a shell whose
// config-dir variable differs from tmux's environment, as shell startup can.
type accountChildTmux struct {
	*mockTmux
	path      string
	shellAuth bool
	output    string
}

func (m *accountChildTmux) SendKeysShellCommand(name, command string) error {
	if err := m.mockTmux.SendKeysShellCommand(name, command); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = []string{"PATH=" + m.path + ":/usr/bin:/bin", "HOME=" + os.Getenv("HOME"), "CLAUDE_CONFIG_DIR=/wrong-shell-directory"}
	for key, value := range m.envVarsFor(name) {
		if key != "CLAUDE_CONFIG_DIR" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	if m.shellAuth {
		cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY=fixture-shell-only")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		m.output = string(output)
		return fmt.Errorf("fake child: %s: %w", output, err)
	}
	return nil
}

func TestClaudeAccountsStayIsolatedAcrossLaunchAndResume(t *testing.T) {
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nprintf '%s' \"$CLAUDE_CONFIG_DIR\" > \"$CLAUDE_CONFIG_DIR/child-root\"\nprintf '%s' \"$*\" > \"$CLAUDE_CONFIG_DIR/child-args\"\nmkdir -p \"$CLAUDE_CONFIG_DIR/projects/fixture\"\nprintf 'fixture' > \"$CLAUDE_CONFIG_DIR/projects/fixture/transcript.jsonl\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var accounts []config.ClaudeAccount
	for _, name := range []string{"work", "extra"} {
		dir := filepath.Join(t.TempDir(), name+" ' account")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fixture-only","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())), 0600); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, config.ClaudeAccount{Name: name, Label: "Subscription " + name, ConfigDir: dir})
		createTask(t, db, name)
	}
	mt := &accountChildTmux{mockTmux: newMockTmux(), path: bin}
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), ClaudeAccounts: accounts, DisablePeerAwareness: true})
	var wg sync.WaitGroup
	for _, account := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svc.SpawnAgent(context.Background(), account.Name, 0, 0, AgentSpawnOptions{Account: account.Name, Unsandboxed: true}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	listed, err := svc.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range listed {
		if agent.Account != agent.TaskID || agent.AccountLabel != "Subscription "+agent.TaskID {
			t.Fatalf("configured account label lost: %+v", agent)
		}
	}
	for _, account := range accounts {
		got, err := os.ReadFile(filepath.Join(account.ConfigDir, "child-root"))
		if err != nil || string(got) != account.ConfigDir {
			t.Fatalf("selected identity did not reach child for %s: %q, %v", account.Name, got, err)
		}
		env := mt.envVarsFor("legato-" + account.Name)
		identity, err := db.ExecutionAccount(context.Background(), env["LEGATO_EXECUTION_ID"])
		if err != nil || identity == nil || identity.Account != account.Name || identity.ConfigDir != account.ConfigDir || !identity.ConfigEnvSet {
			t.Fatalf("launch identity not persisted: %+v, %v", identity, err)
		}
	}
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		t.Fatal("spawn mutated process environment")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "child-root")); !os.IsNotExist(err) {
		t.Fatal("named spawn wrote default state")
	}
	t.Run("auth conflicts do not fall back", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "fixture-not-a-real-key")
		createTask(t, db, "auth-conflict")
		if err := svc.SpawnAgent(context.Background(), "auth-conflict", 0, 0, AgentSpawnOptions{Account: "work"}); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") || strings.Contains(err.Error(), "fixture-not-a-real-key") {
			t.Fatalf("auth conflict not sanitized/refused: %v", err)
		}
		if mt.sessionAlive("legato-auth-conflict") {
			t.Fatal("conflicting named account launched")
		}
		createTask(t, db, "legacy-default")
		plain := NewAgentService(db, newMockTmux(), t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), DisablePeerAwareness: true})
		if err := plain.SpawnAgent(context.Background(), "legacy-default", 0, 0); err != nil {
			t.Fatalf("intentional default auth rejected: %v", err)
		}
	})
	ctx := context.Background()
	session, err := db.GetAgentSessionByTaskID(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	execution, err := db.GetExecution(ctx, session.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordTaskNativeSession(ctx, "work", session.ID, session.ExecutionID, "claude-code", "fixture-session", execution.WorkingDir, accounts[1].ConfigDir); err == nil {
		t.Fatal("native bridge changed account storage")
	}
	if _, err := db.RecordTaskNativeSession(ctx, "work", session.ID, session.ExecutionID, "claude-code", "fixture-session", execution.WorkingDir, accounts[0].ConfigDir); err != nil {
		t.Fatal(err)
	}
	if err := mt.Kill("legato-work"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentSessionStatus(ctx, "work", "dead"); err != nil {
		t.Fatal(err)
	}
	resumer := NewAgentService(db, mt, execution.WorkingDir, AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), ClaudeAccounts: accounts, NewWorkDefaults: func(context.Context, string) (NewWorkDefaults, error) {
		t.Fatal("resume followed changed profile")
		return NewWorkDefaults{Agent: "claude-code", Account: "extra"}, nil
	}, DisablePeerAwareness: true})
	if err := os.Remove(filepath.Join(accounts[0].ConfigDir, "child-root")); err != nil {
		t.Fatal(err)
	}
	if err := resumer.SpawnAgent(ctx, "work", 0, 0, AgentSpawnOptions{SessionPolicy: "resume", Unsandboxed: true}); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(accounts[0].ConfigDir, "child-root"))
	if err != nil || string(marker) != accounts[0].ConfigDir {
		t.Fatalf("resume did not reach original child root: %q %v", marker, err)
	}
	args, err := os.ReadFile(filepath.Join(accounts[0].ConfigDir, "child-args"))
	if err != nil || !strings.Contains(string(args), "--resume fixture-session") {
		t.Fatalf("child did not resume captured session: %q %v", args, err)
	}
	if got := mt.envVarsFor("legato-work")["CLAUDE_CONFIG_DIR"]; got != accounts[0].ConfigDir {
		t.Fatalf("resume switched identity: %s", got)
	}
	if err := mt.Kill("legato-work"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentSessionStatus(ctx, "work", "dead"); err != nil {
		t.Fatal(err)
	}
	for _, configured := range [][]config.ClaudeAccount{accounts[1:], {{Name: "work", ConfigDir: accounts[1].ConfigDir}}} {
		changed := NewAgentService(db, mt, execution.WorkingDir, AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), ClaudeAccounts: configured, DisablePeerAwareness: true})
		if err := changed.SpawnAgent(ctx, "work", 0, 0, AgentSpawnOptions{SessionPolicy: "resume"}); err == nil {
			t.Fatal("resume accepted a missing or remapped account")
		}
		if mt.sessionAlive("legato-work") {
			t.Fatal("failed account resume launched a child")
		}
	}
	t.Run("shell-only auth override cannot reach Claude", func(t *testing.T) {
		createTask(t, db, "shell-only")
		marker := filepath.Join(accounts[0].ConfigDir, "child-root")
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		mt.shellAuth = true
		defer func() { mt.shellAuth = false }()
		if err := svc.SpawnAgent(ctx, "shell-only", 0, 0, AgentSpawnOptions{Account: "work", Unsandboxed: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("shell override ran the child: %v", err)
		}
		if !strings.Contains(mt.output, "conflicting auth/provider override") || strings.Contains(mt.output, "fixture-shell-only") {
			t.Fatalf("shell guidance not sanitized/actionable: %s", mt.output)
		}
	})
}

func TestUnknownAccountFailsBeforeLaunch(t *testing.T) {
	svc, db, mt := newTestAgentService(t)
	createTask(t, db, "unknown-account")
	err := svc.SpawnAgent(context.Background(), "unknown-account", 0, 0, AgentSpawnOptions{Account: "absent"})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("missing account not refused: %v", err)
	}
	sessions, err := mt.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatal("unknown account created a terminal")
	}
}
