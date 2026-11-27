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
	"github.com/cpave3/legato/internal/engine/sandbox"
)

// claudeMounts is how a plan treats each Claude state path: "rw", "ro",
// "hide", "hidedir", "-" when not mounted at all.
func claudeMounts(plan sandbox.Plan, paths ...string) map[string]string {
	names := map[sandbox.Kind]string{sandbox.ReadOnly: "ro", sandbox.ReadWrite: "rw", sandbox.Hide: "hide", sandbox.HideDir: "hidedir", sandbox.Symlink: "link", sandbox.Overlay: "overlay"}
	m := mounted(plan)
	out := map[string]string{}
	for _, p := range paths {
		if got, ok := m[p]; ok {
			out[p] = names[got.Kind]
		} else {
			out[p] = "-"
		}
	}
	return out
}

// claudeFixture is a sandboxed agent service whose home has the default
// Claude folder and .claude.json, a configured account "work" outside it,
// and (envDir) a folder Legato's own CLAUDE_CONFIG_DIR may name.
type claudeFixture struct {
	svc                AgentService
	backend            *fakeBackend
	home, work, envDir string
}

func newClaudeFixture(t *testing.T, envConfig bool) claudeFixture {
	t.Helper()
	f := claudeFixture{backend: &fakeBackend{}}
	svc, db, _ := sandboxedService(t, f.backend, func(settings *SandboxSettings) { f.home = settings.Base.Home })
	f.svc = svc
	f.work = filepath.Join(t.TempDir(), "work-account")
	f.envDir = filepath.Join(t.TempDir(), "env-claude")
	for _, dir := range []string{filepath.Join(f.home, ".claude", "proj"), filepath.Join(f.work, "proj"), filepath.Join(f.envDir, "proj")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.home, ".claude.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	login := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fixture-only","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(f.work, ".credentials.json"), []byte(login), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	if envConfig {
		t.Setenv("CLAUDE_CONFIG_DIR", f.envDir)
	}
	svc.(*agentService).claudeAccounts = []config.ClaudeAccount{{Name: "work", ConfigDir: f.work}}
	createTask(t, db, "task-1")
	return f
}

// What a sandboxed agent may reach of Claude's state: its own account's
// folder (the default one in the home when it names none), never another
// account's, and the home's .claude.json only with the default account.
// Pinned before sandboxPrefix's account rules were moved (CodeEval E6,
// i95qqzwb): the mounts it gets, and which working directories it is
// refused because they are inside another account's folder.
func TestASandboxSeesOnlyItsOwnClaudeAccount(t *testing.T) {
	for _, tc := range []struct {
		name      string
		agent     string
		account   string
		envConfig bool // CLAUDE_CONFIG_DIR set in Legato's own environment
		mounts    func(f claudeFixture) map[string]string
		refused   func(f claudeFixture) map[string]bool // working dir → refused as another account's
	}{
		{"claude-code on the default account", "claude-code", "", false,
			func(f claudeFixture) map[string]string {
				return map[string]string{filepath.Join(f.home, ".claude"): "rw", filepath.Join(f.home, ".claude.json"): "rw", f.work: "-"}
			},
			func(f claudeFixture) map[string]bool {
				return map[string]bool{filepath.Join(f.work, "proj"): true, filepath.Join(f.envDir, "proj"): false}
			}},
		{"claude-code on the work account", "claude-code", "work", false,
			func(f claudeFixture) map[string]string {
				return map[string]string{f.work: "rw", filepath.Join(f.home, ".claude"): "-", filepath.Join(f.home, ".claude.json"): "-"}
			},
			func(f claudeFixture) map[string]bool {
				return map[string]bool{filepath.Join(f.home, ".claude", "proj"): true, filepath.Join(f.envDir, "proj"): false}
			}},
		{"chimera reaches no Claude account", "chimera", "", false,
			func(f claudeFixture) map[string]string {
				return map[string]string{filepath.Join(f.home, ".claude"): "-", filepath.Join(f.home, ".claude.json"): "-", f.work: "-"}
			},
			func(f claudeFixture) map[string]bool {
				return map[string]bool{filepath.Join(f.work, "proj"): true, filepath.Join(f.home, ".claude", "proj"): true, filepath.Join(f.envDir, "proj"): false}
			}},
		{"chimera with Legato's CLAUDE_CONFIG_DIR elsewhere", "chimera", "", true,
			func(f claudeFixture) map[string]string {
				return map[string]string{filepath.Join(f.home, ".claude"): "-", f.envDir: "-", f.work: "-"}
			},
			func(f claudeFixture) map[string]bool {
				return map[string]bool{filepath.Join(f.work, "proj"): true, filepath.Join(f.home, ".claude", "proj"): true, filepath.Join(f.envDir, "proj"): true}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClaudeFixture(t, tc.envConfig)
			if err := f.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{AgentKind: tc.agent, Account: tc.account}); err != nil {
				t.Fatal(err)
			}
			want := tc.mounts(f)
			var paths []string
			for p := range want {
				paths = append(paths, p)
			}
			if got := claudeMounts(f.backend.plans[0], paths...); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("Claude state = %v, want %v", got, want)
			}
			for dir, refused := range tc.refused(f) {
				g := newClaudeFixture(t, tc.envConfig)
				err := g.svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{AgentKind: tc.agent, Account: tc.account, WorkingDir: g.rebase(f, dir)})
				got := err != nil && strings.Contains(err.Error(), "another Claude account's private state")
				if got != refused {
					t.Errorf("working in %s: refused = %v (%v), want %v", dir, got, err, refused)
				}
			}
		})
	}
}

// rebase maps a path of fixture f onto the same place in g.
func (g claudeFixture) rebase(f claudeFixture, path string) string {
	for _, pair := range [][2]string{{f.work, g.work}, {f.envDir, g.envDir}, {f.home, g.home}} {
		if rel, err := filepath.Rel(pair[0], path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join(pair[1], rel)
		}
	}
	return path
}

// The rule itself, for the cases a spawn cannot easily reach: a Base that
// already protects something, an account that is Legato's own default, and
// a non-Claude agent keeping the Base's config folder.
func TestWithClaudeAccounts(t *testing.T) {
	accounts := []config.ClaudeAccount{{Name: "work", ConfigDir: "/acct/work"}, {Name: "home", ConfigDir: "/acct/home/"}}
	for _, tc := range []struct {
		name                  string
		base                  SandboxPlanInput
		agent, launch, legato string
		wantDir               string
		wantExplicit          bool
		wantProtected         []string
	}{
		{"default account", SandboxPlanInput{Home: "/h"}, "claude-code", "", "", "/h/.claude", false,
			[]string{"/acct/work", "/acct/home/"}},
		{"named account", SandboxPlanInput{Home: "/h"}, "claude-code", "/acct/work", "", "/acct/work", true,
			[]string{"/h/.claude", "/h/.claude", "/acct/home/", "/h/.claude.json"}},
		{"named account, spelled with a slash", SandboxPlanInput{Home: "/h"}, "claude-code", "/acct/home", "", "/acct/home", true,
			[]string{"/h/.claude", "/h/.claude", "/acct/work", "/h/.claude.json"}},
		{"Legato's default is the account in use", SandboxPlanInput{Home: "/h"}, "claude-code", "/acct/work", "/acct/work", "/acct/work", true,
			[]string{"/h/.claude", "/acct/home/", "/h/.claude.json"}},
		{"chimera", SandboxPlanInput{Home: "/h", ProtectedClaudeAccounts: []string{"/kept"}}, "chimera", "/ignored", "/env", "", false,
			[]string{"/kept", "/env", "/h/.claude", "/acct/work", "/acct/home/", "/h/.claude.json"}},
		{"chimera keeps the Base's folder", SandboxPlanInput{Home: "/h", ClaudeConfigDir: "/acct/work"}, "chimera", "", "", "/acct/work", false,
			[]string{"/h/.claude", "/h/.claude", "/acct/home/", "/h/.claude.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			before := append([]string(nil), base.ProtectedClaudeAccounts...)
			got := withClaudeAccounts(base, tc.agent, tc.launch, tc.legato, accounts)
			if got.ClaudeConfigDir != tc.wantDir || got.ClaudeConfigExplicit != tc.wantExplicit || fmt.Sprint(got.ProtectedClaudeAccounts) != fmt.Sprint(tc.wantProtected) {
				t.Fatalf("got dir %q explicit %v protected %v", got.ClaudeConfigDir, got.ClaudeConfigExplicit, got.ProtectedClaudeAccounts)
			}
			if fmt.Sprint(base.ProtectedClaudeAccounts) != fmt.Sprint(before) {
				t.Fatal("the Base's protected list was changed in place")
			}
		})
	}
}
