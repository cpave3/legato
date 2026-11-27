package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

func namedAuthFixture(t *testing.T) (AgentService, *mockTmux, string, string) {
	t.Helper()
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	account, work := t.TempDir(), t.TempDir()
	writeNamedAuthFixture(t, filepath.Join(account, ".credentials.json"), fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fixture-only","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli()))
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	createTask(t, db, "auth-source")
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), ClaudeAccounts: []config.ClaudeAccount{{Name: "work", ConfigDir: account}}, DisablePeerAwareness: true})
	return svc, mt, account, work
}

func writeNamedAuthFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNamedAccountAuthSourcesAtSpawn(t *testing.T) {
	for _, tc := range []struct {
		name, file, data, want string
		args                   []string
	}{
		{name: "account environment", file: "account/settings.json", data: `{"env":{"ANTHROPIC_API_KEY":"fixture-secret"}}`, want: "ANTHROPIC_API_KEY"},
		{name: "account saved console key", file: "account/.claude.json", data: `{"primaryApiKey":"fixture-secret"}`, want: "primaryApiKey"},
		{name: "working directory helper", file: "work/.claude/settings.json", data: `{"apiKeyHelper":"fixture-secret"}`, want: "apiKeyHelper"},
		{name: "working directory local provider", file: "work/.claude/settings.local.json", data: `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1"}}`, want: "CLAUDE_CODE_USE_BEDROCK"},
		{name: "ancestor console login", file: "ancestor/.claude/settings.local.json", data: `{"forceLoginMethod":"console"}`, want: "forceLoginMethod"},
		{name: "ancestor shared helper", file: "ancestor/.claude/settings.json", data: `{"apiKeyHelper":"fixture-secret"}`, want: "apiKeyHelper"},
		{name: "account profile", file: "account/settings.json", data: `{"env":{"ANTHROPIC_PROFILE":"fixture-secret"}}`, want: "ANTHROPIC_PROFILE"},
		{name: "redirected account", file: "work/.claude/settings.json", data: `{"env":{"CLAUDE_CONFIG_DIR":"fixture-secret"}}`, want: "CLAUDE_CONFIG_DIR"},
		{name: "settings path argument", args: []string{"--settings", "fixture-secret"}, want: "--settings"},
		{name: "inline settings argument", args: []string{`--settings={"apiKeyHelper":"fixture-secret"}`}, want: "--settings"},
		{name: "profile argument", args: []string{"--profile=fixture-secret"}, want: "--profile"},
		{name: "malformed settings", file: "account/settings.json", data: `{"apiKeyHelper":"fixture-secret",`, want: "invalid JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, mt, account, work := namedAuthFixture(t)
			ancestor := work
			work = filepath.Join(work, "nested")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{"account": account, "work": work, "ancestor": ancestor}
			if tc.file != "" {
				scope, path, _ := strings.Cut(tc.file, "/")
				writeNamedAuthFixture(t, filepath.Join(paths[scope], path), tc.data)
			}
			err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, AgentSpawnOptions{Account: "work", WorkingDir: work, Unsandboxed: true, LaunchArgs: tc.args})
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("expected sanitized %s refusal, got %v", tc.want, err)
			}
			if mt.sessionAlive("legato-auth-source") {
				t.Fatal("conflicting settings launched a child")
			}
		})
	}
}

func TestNamedAccountAuthSettingsSafetyAtSpawn(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		prepare    func(*testing.T, string, string)
	}{
		{"symlink file", "symlink", func(t *testing.T, account, work string) {
			outside := filepath.Join(t.TempDir(), "outside.json")
			writeNamedAuthFixture(t, outside, `{"apiKeyHelper":"fixture-secret"}`)
			if err := os.Symlink(outside, filepath.Join(account, "settings.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink settings directory", "symlink", func(t *testing.T, account, work string) {
			outside := t.TempDir()
			writeNamedAuthFixture(t, filepath.Join(outside, "settings.json"), `{}`)
			if err := os.Symlink(outside, filepath.Join(work, ".claude")); err != nil {
				t.Fatal(err)
			}
		}},
		{"non regular settings", "regular file", func(t *testing.T, account, work string) {
			if err := os.Mkdir(filepath.Join(account, "settings.json"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversize settings", "inspection limit", func(t *testing.T, account, work string) {
			writeNamedAuthFixture(t, filepath.Join(account, "settings.json"), `{"padding":"`+strings.Repeat("x", (1<<20))+`"}`)
		}},
		{"unreadable settings parent", "cannot be inspected", func(t *testing.T, account, work string) {
			writeNamedAuthFixture(t, filepath.Join(work, ".claude"), "not a directory")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, mt, account, work := namedAuthFixture(t)
			tc.prepare(t, account, work)
			err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, AgentSpawnOptions{Account: "work", WorkingDir: work, Unsandboxed: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("unsafe settings not refused with actionable sanitized error: %v", err)
			}
			if mt.sessionAlive("legato-auth-source") {
				t.Fatal("unsafe settings launched a child")
			}
		})
	}
}

func TestNamedAccountAuthAllowsSubscriptionAndDefaultAtSpawn(t *testing.T) {
	for _, named := range []bool{true, false} {
		t.Run(fmt.Sprintf("named=%v", named), func(t *testing.T) {
			svc, mt, account, work := namedAuthFixture(t)
			data := `{"model":"fixture-model","forceLoginMethod":"claudeai","env":{"UNRELATED":"ok"},"apiKeyHelper":""}`
			opt := AgentSpawnOptions{WorkingDir: work, Unsandboxed: true, LaunchArgs: []string{"--model", "fixture-model"}}
			if named {
				opt.Account = "work"
			} else {
				data = `{"apiKeyHelper":"fixture-secret"}`
				opt.Account = "default"
				opt.LaunchArgs = []string{"--settings", `{"apiKeyHelper":"fixture-secret"}`}
			}
			writeNamedAuthFixture(t, filepath.Join(account, "settings.json"), data)
			writeNamedAuthFixture(t, filepath.Join(work, ".claude/settings.local.json"), data)
			if err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, opt); err != nil {
				t.Fatalf("valid launch refused: %v", err)
			}
			if !mt.sessionAlive("legato-auth-source") {
				t.Fatal("allowed launch did not start a child")
			}
		})
	}
}

func TestNamedAccountAuthLinkedWorktreeAtSpawn(t *testing.T) {
	for _, conflict := range []bool{true, false} {
		t.Run(fmt.Sprintf("conflict=%v", conflict), func(t *testing.T) {
			svc, mt, _, work := namedAuthFixture(t)
			main := t.TempDir()
			gitdir := filepath.Join(main, ".git/worktrees/fixture")
			writeNamedAuthFixture(t, filepath.Join(work, ".git"), "gitdir: "+gitdir+"\n")
			writeNamedAuthFixture(t, filepath.Join(gitdir, "commondir"), "../..\n")
			data := `{"model":"fixture-model"}`
			if conflict {
				data = `{"apiKeyHelper":"fixture-secret"}`
			}
			writeNamedAuthFixture(t, filepath.Join(main, ".claude/settings.local.json"), data)
			err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, AgentSpawnOptions{Account: "work", WorkingDir: work, Unsandboxed: true})
			if conflict {
				if err == nil || !strings.Contains(err.Error(), "apiKeyHelper") || strings.Contains(err.Error(), "fixture-secret") {
					t.Fatalf("main-checkout conflict not refused: %v", err)
				}
				if mt.sessionAlive("legato-auth-source") {
					t.Fatal("conflicting main checkout launched a child")
				}
			} else if err != nil || !mt.sessionAlive("legato-auth-source") {
				t.Fatalf("safe worktree did not launch: %v", err)
			}
		})
	}
}
