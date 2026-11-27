package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// accountResolveFixture is one isolated machine for resolveClaudeAccount: a
// home with the default ~/.claude, two named accounts ("work" logged in,
// "personal"), a service checkout and a Legato data folder, all in a temp
// folder whose path has no symlinks, so a configured path is its own
// resolved path unless a case links it.
type accountResolveFixture struct {
	root, home, work, personal string
	svc                        *agentService
	opt                        AgentSpawnOptions
	kind                       string
}

func newAccountResolveFixture(t *testing.T) *accountResolveFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &accountResolveFixture{
		root:     root,
		home:     filepath.Join(root, "home"),
		work:     filepath.Join(root, "accounts", "work"),
		personal: filepath.Join(root, "accounts", "personal"),
		kind:     "claude-code",
	}
	for _, dir := range []string{filepath.Join(f.home, ".claude"), f.work, f.personal, filepath.Join(root, "repo"), filepath.Join(root, "data")} {
		mustMkdir(t, dir)
	}
	writeAccountLogin(t, f.work, time.Now().Add(time.Hour).UnixMilli())
	t.Setenv("HOME", f.home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	f.svc = &agentService{
		claudeAccounts: []config.ClaudeAccount{{Name: "work", ConfigDir: f.work}, {Name: "personal", ConfigDir: f.personal}},
		workDir:        filepath.Join(root, "repo"),
		accountDataDir: filepath.Join(root, "data"),
	}
	return f
}

// setAccountDir points a configured account at another folder.
func (f *accountResolveFixture) setAccountDir(name, dir string) {
	for i := range f.svc.claudeAccounts {
		if f.svc.claudeAccounts[i].Name == name {
			f.svc.claudeAccounts[i].ConfigDir = dir
		}
	}
}

// moveWork points "work" at dir and gives it a fresh login there.
func (f *accountResolveFixture) moveWork(t *testing.T, dir string) {
	t.Helper()
	mustMkdir(t, dir)
	writeAccountLogin(t, dir, time.Now().Add(time.Hour).UnixMilli())
	f.setAccountDir("work", dir)
	f.work = dir
}

func (f *accountResolveFixture) workIdentity() *store.AgentAccountIdentity {
	return &store.AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: f.work, ResolvedDir: f.work, ConfigEnvSet: true}
}

func (f *accountResolveFixture) defaultIdentity() *store.AgentAccountIdentity {
	dir := filepath.Join(f.home, ".claude")
	return &store.AgentAccountIdentity{Provider: "claude", ConfigDir: dir, ResolvedDir: dir}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func writeAccountLogin(t *testing.T, dir string, expiresAtMillis int64) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".credentials.json"), fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fixture-token","expiresAt":%d}}`, expiresAtMillis))
}

const (
	errAccountNeedsClaudeCode = "a Claude account requires the claude-code agent; remove account or select claude-code"
	errAccountKindMismatch    = "a Claude account requires the claude-code agent"
	errSavedAccountConflicts  = "saved Claude account conflicts with the selected account"
	errSavedAccountChanged    = "saved Claude account configuration changed; restore its original directory mapping before resuming"
	errDefaultDirUnresolvable = "default Claude directory cannot be resolved"
	errSafetyCheckFailed      = "Claude account directory safety check failed"
	errWorkOverlapsDefault    = `Claude account "work" overlaps the default account directory`
	errWorkOverlapsPrivate    = `Claude account "work" directory overlaps credentials, tool installation or protected Legato state`
	errWorkOverlapsProtected  = `Claude account "work" directory overlaps protected state or a working directory`
	errWorkAliases            = `Claude account "work" directory aliases another account`
	errWorkStateUninspectable = `Claude account "work" state cannot be inspected`
	errWorkStateSymlink       = `Claude account "work" state contains a symlink; replace it with an account-local file`
	errWorkLoginExpired       = `Claude account "work" login has expired; sign in to that directory with claude auth login --claudeai`
)

func errWorkAuthVar(key string) string {
	return fmt.Sprintf("Claude account %q conflicts with %s; remove that auth/provider override for a named subscription", "work", key)
}

func errWorkNoLogin(dir string) string {
	return fmt.Sprintf("Claude account %q has no readable subscription login; run CLAUDE_CONFIG_DIR=%s claude auth login --claudeai", "work", shellWord(dir))
}

type accountResolveCase struct {
	name  string
	setup func(t *testing.T, f *accountResolveFixture)
	// Exactly one of err, want or wantNil.
	err      func(f *accountResolveFixture) string
	want     func(f *accountResolveFixture) *store.AgentAccountIdentity
	wantNil  bool
	noDarwin bool // the missing-login path differs on darwin (Keychain)
}

func fixed(msg string) func(*accountResolveFixture) string {
	return func(*accountResolveFixture) string { return msg }
}

func named(account string) func(*testing.T, *accountResolveFixture) {
	return func(_ *testing.T, f *accountResolveFixture) { f.opt.Account = account }
}

// accountResolveCases characterises every return of resolveClaudeAccount,
// and its check order where two checks would both refuse.
func accountResolveCases() []accountResolveCase {
	cases := []accountResolveCase{
		// --- agent kind ---
		{name: "non-claude kind without an account resolves nothing", wantNil: true,
			setup: func(_ *testing.T, f *accountResolveFixture) { f.kind = "codex" }},
		{name: "non-claude kind with an account is refused", err: fixed(errAccountNeedsClaudeCode),
			setup: func(_ *testing.T, f *accountResolveFixture) { f.kind, f.opt.Account = "codex", "work" }},
		{name: "non-claude kind with only a saved identity is refused", err: fixed(errAccountNeedsClaudeCode),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.kind, f.opt.AccountIdentity = "codex", f.defaultIdentity()
			}},
		{name: "order: non-claude kind before an unset HOME", err: fixed(errAccountNeedsClaudeCode),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.kind, f.opt.Account = "codex", "work"
				t.Setenv("HOME", "")
			}},
		{name: "account with another requested agent kind is refused", err: fixed(errAccountKindMismatch),
			setup: func(_ *testing.T, f *accountResolveFixture) { f.opt.Account, f.opt.AgentKind = "work", "codex" }},
		{name: "order: kind mismatch before an unset HOME", err: fixed(errAccountKindMismatch),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AgentKind = "work", "codex"
				t.Setenv("HOME", "")
			}},
		{name: "another requested agent kind without an account resolves the default",
			want:  (*accountResolveFixture).defaultIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) { f.opt.AgentKind = "codex" }},
		{name: "unset HOME is the home lookup's error", err: fixed("$HOME is not defined"),
			setup: func(t *testing.T, _ *accountResolveFixture) { t.Setenv("HOME", "") }},

		// --- saved identity ---
		{name: "saved identity of another provider conflicts", err: fixed(errSavedAccountConflicts),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				id := f.workIdentity()
				id.Provider = "codex"
				f.opt.AccountIdentity = id
			}},
		{name: "saved identity of another account conflicts", err: fixed(errSavedAccountConflicts),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AccountIdentity = "personal", f.workIdentity()
			}},
		{name: "explicit default against a saved named identity conflicts", err: fixed(errSavedAccountConflicts),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AccountIdentity = "default", f.workIdentity()
			}},
		{name: "order: saved conflict before an unknown account", err: fixed(errSavedAccountConflicts),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AccountIdentity = "ghost", f.workIdentity()
			}},
		{name: "saved identity names the account when none is selected", want: (*accountResolveFixture).workIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) { f.opt.AccountIdentity = f.workIdentity() }},
		{name: "saved identity agrees with the selected account", want: (*accountResolveFixture).workIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AccountIdentity = "work", f.workIdentity()
			}},
		{name: "explicit default agrees with a saved default identity", want: (*accountResolveFixture).defaultIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account, f.opt.AccountIdentity = "default", f.defaultIdentity()
			}},

		// --- unknown account and unresolvable directory ---
		{name: "unknown account is refused",
			err:   fixed(`Claude account "ghost" is not configured on this instance; restore its configuration before resuming`),
			setup: named("ghost")},
		{name: "saved identity of an account no longer configured is refused",
			err: fixed(`Claude account "ghost" is not configured on this instance; restore its configuration before resuming`),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.AccountIdentity = &store.AgentAccountIdentity{Provider: "claude", Account: "ghost"}
			}},
		{name: "order: unknown account before an auth override",
			err: fixed(`Claude account "ghost" is not configured on this instance; restore its configuration before resuming`),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.Account = "ghost"
				t.Setenv("ANTHROPIC_API_KEY", "fixture")
			}},
		{name: "named account directory that cannot be resolved is refused",
			err: fixed(`Claude account "work" directory cannot be resolved`),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "file"), "")
				f.setAccountDir("work", filepath.Join(f.root, "file", "sub"))
				f.opt.Account = "work"
			}},
		{name: "default directory that cannot be resolved is refused with an empty account name",
			err: fixed(`Claude account "" directory cannot be resolved`),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "file"), "")
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.root, "file", "sub"))
			}},

		// --- changed saved configuration ---
		{name: "saved identity whose mapping changed is refused", err: fixed(errSavedAccountChanged),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				id := f.workIdentity()
				id.ResolvedDir = filepath.Join(f.root, "elsewhere")
				f.opt.AccountIdentity = id
			}},
		{name: "saved default identity whose env setting changed is refused", err: fixed(errSavedAccountChanged),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.AccountIdentity = f.defaultIdentity()
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.home, ".claude"))
			}},
		{name: "order: changed mapping before an auth override", err: fixed(errSavedAccountChanged),
			setup: func(t *testing.T, f *accountResolveFixture) {
				id := f.workIdentity()
				id.ConfigEnvSet = false
				f.opt.AccountIdentity = id
				t.Setenv("ANTHROPIC_API_KEY", "fixture")
			}},

		// --- the default account skips the named-account guards ---
		{name: "default account resolves ~/.claude unset", want: (*accountResolveFixture).defaultIdentity,
			setup: func(*testing.T, *accountResolveFixture) {}},
		{name: "explicit default resolves like no account", want: (*accountResolveFixture).defaultIdentity,
			setup: named("default")},
		{name: "default account follows CLAUDE_CONFIG_DIR",
			want: func(f *accountResolveFixture) *store.AgentAccountIdentity {
				dir := filepath.Join(f.root, "custom")
				return &store.AgentAccountIdentity{Provider: "claude", ConfigDir: dir, ResolvedDir: dir, ConfigEnvSet: true}
			},
			setup: func(t *testing.T, f *accountResolveFixture) {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.root, "custom"))
			}},
		{name: "default account keeps its spelling and resolves a symlink",
			want: func(f *accountResolveFixture) *store.AgentAccountIdentity {
				return &store.AgentAccountIdentity{Provider: "claude", ConfigDir: filepath.Join(f.root, "link"), ResolvedDir: f.work, ConfigEnvSet: true}
			},
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustSymlink(t, f.work, filepath.Join(f.root, "link"))
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.root, "link"))
			}},
		{name: "default account ignores auth overrides, overlaps and a missing login",
			want: func(f *accountResolveFixture) *store.AgentAccountIdentity {
				return &store.AgentAccountIdentity{Provider: "claude", ConfigDir: f.home, ResolvedDir: f.home, ConfigEnvSet: true}
			},
			setup: func(t *testing.T, f *accountResolveFixture) {
				t.Setenv("CLAUDE_CONFIG_DIR", f.home)
				t.Setenv("ANTHROPIC_API_KEY", "fixture")
				f.opt.ExtraEnv = map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "fixture"}
			}},
	}

	// --- auth/provider overrides, each key in the environment and in ExtraEnv ---
	for _, key := range namedAccountAuthVars {
		cases = append(cases,
			accountResolveCase{name: "env " + key + " conflicts with a named account", err: fixed(errWorkAuthVar(key)),
				setup: func(t *testing.T, f *accountResolveFixture) {
					f.opt.Account = "work"
					t.Setenv(key, "fixture")
				}},
			accountResolveCase{name: "ExtraEnv " + key + " conflicts with a named account", err: fixed(errWorkAuthVar(key)),
				setup: func(_ *testing.T, f *accountResolveFixture) {
					f.opt.Account = "work"
					f.opt.ExtraEnv = map[string]string{key: "fixture"}
				}},
		)
	}
	cases = append(cases, []accountResolveCase{
		{name: "order: keys in list order, ExtraEnv of an earlier key before env of a later one",
			err: fixed(errWorkAuthVar("ANTHROPIC_API_KEY")),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.Account = "work"
				t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "fixture")
				f.opt.ExtraEnv = map[string]string{"ANTHROPIC_API_KEY": "fixture"}
			}},
		{name: "order: env of an earlier key before ExtraEnv of a later one",
			err: fixed(errWorkAuthVar("ANTHROPIC_AUTH_TOKEN")),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.Account = "work"
				t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture")
				f.opt.ExtraEnv = map[string]string{"ANTHROPIC_BASE_URL": "fixture"}
			}},
		{name: "empty ExtraEnv values are not overrides", want: (*accountResolveFixture).workIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.Account = "work"
				f.opt.ExtraEnv = map[string]string{"ANTHROPIC_API_KEY": ""}
			}},
		{name: "order: auth override before a default-directory overlap", err: fixed(errWorkAuthVar("ANTHROPIC_PROFILE")),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.opt.Account = "work"
				t.Setenv("ANTHROPIC_PROFILE", "fixture")
				t.Setenv("CLAUDE_CONFIG_DIR", f.work)
			}},

		// --- default directory ---
		{name: "default directory that cannot be resolved refuses a named account", err: fixed(errDefaultDirUnresolvable),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "file"), "")
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.root, "file", "sub"))
				f.opt.Account = "work"
			}},
		{name: "account inside the default directory overlaps it", err: fixed(errWorkOverlapsDefault),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.moveWork(t, filepath.Join(f.home, ".claude", "work"))
				f.opt.Account = "work"
			}},
		{name: "account containing CLAUDE_CONFIG_DIR overlaps the default", err: fixed(errWorkOverlapsDefault),
			setup: func(t *testing.T, f *accountResolveFixture) {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.work, "inner"))
				f.opt.Account = "work"
			}},
		{name: "account linked to the default directory overlaps it", err: fixed(errWorkOverlapsDefault),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustSymlink(t, filepath.Join(f.home, ".claude"), filepath.Join(f.root, "alias"))
				f.setAccountDir("work", filepath.Join(f.root, "alias"))
				f.opt.Account = "work"
			}},
		{name: "order: default overlap before a private-root overlap", err: fixed(errWorkOverlapsDefault),
			setup: func(t *testing.T, f *accountResolveFixture) {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.home, ".codex", "default"))
				f.moveWork(t, filepath.Join(f.home, ".codex"))
				f.opt.Account = "work"
			}},
	}...)

	// --- private roots (credentials, tool state, Legato's data) ---
	private := map[string]func(f *accountResolveFixture) string{
		"~/.ssh": func(f *accountResolveFixture) string { return filepath.Join(f.home, ".ssh", "acct") },
		"~/.aws": func(f *accountResolveFixture) string { return filepath.Join(f.home, ".aws") },
		"~/.local/share/claude": func(f *accountResolveFixture) string {
			return filepath.Join(f.home, ".local", "share", "claude", "acct")
		},
		"~/.local/share legato": func(f *accountResolveFixture) string {
			return filepath.Join(f.home, ".local", "share", "legato", "acct")
		},
		"~/.codex":     func(f *accountResolveFixture) string { return filepath.Join(f.home, ".codex", "acct") },
		"~/.chimera":   func(f *accountResolveFixture) string { return filepath.Join(f.home, ".chimera") },
		"the data dir": func(f *accountResolveFixture) string { return filepath.Join(f.root, "data", "acct") },
		"a parent of ~/.local/share/claude": func(f *accountResolveFixture) string {
			return filepath.Join(f.home, ".local", "share")
		},
	}
	for label, dir := range private {
		cases = append(cases, accountResolveCase{name: "account overlapping " + label + " is refused", err: fixed(errWorkOverlapsPrivate),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.moveWork(t, dir(f))
				f.opt.Account = "work"
			}})
	}
	cases = append(cases, []accountResolveCase{
		{name: "account containing the data dir is refused as private", err: fixed(errWorkOverlapsPrivate),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.svc.accountDataDir = filepath.Join(f.work, "data")
				f.opt.Account = "work"
			}},
		{name: "private root that cannot be resolved fails the safety check", err: fixed(errSafetyCheckFailed),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.home, ".config"), "") // ~/.config/gh cannot resolve
				f.opt.Account = "work"
			}},
		{name: "order: private overlap before a protected working directory", err: fixed(errWorkOverlapsPrivate),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.svc.accountDataDir = filepath.Join(f.work, "data")
				f.svc.workDir = filepath.Join(f.work, "repo")
				f.opt.Account = "work"
			}},

		// --- protected roots (home, checkouts, Legato's data) ---
		{name: "account containing the service checkout is refused", err: fixed(errWorkOverlapsProtected),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.svc.workDir = filepath.Join(f.work, "repo")
				f.opt.Account = "work"
			}},
		{name: "account containing the spawn's working directory is refused", err: fixed(errWorkOverlapsProtected),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.WorkingDir = filepath.Join(f.work, "checkout")
				f.opt.Account = "work"
			}},
		{name: "account equal to the working directory is refused", err: fixed(errWorkOverlapsProtected),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.opt.WorkingDir = f.work
				f.opt.Account = "work"
			}},
		{name: "account containing a linked working directory is refused", err: fixed(errWorkOverlapsProtected),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustMkdir(t, filepath.Join(f.work, "checkout"))
				mustSymlink(t, filepath.Join(f.work, "checkout"), filepath.Join(f.root, "checkout-link"))
				f.opt.WorkingDir = filepath.Join(f.root, "checkout-link")
				f.opt.Account = "work"
			}},
		{name: "account inside a working directory is allowed (one direction only)",
			want: (*accountResolveFixture).workIdentity,
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.moveWork(t, filepath.Join(f.root, "repo", ".claude-work"))
				f.opt.Account = "work"
			}},
		{name: "account with no service checkout configured", want: (*accountResolveFixture).workIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.svc.workDir, f.svc.accountDataDir = "", ""
				f.opt.Account = "work"
			}},
		{name: "protected root that cannot be resolved fails the safety check", err: fixed(errSafetyCheckFailed),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "file"), "")
				f.opt.WorkingDir = filepath.Join(f.root, "file", "sub")
				f.opt.Account = "work"
			}},
		{name: "order: protected overlap before aliasing another account", err: fixed(errWorkOverlapsProtected),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.svc.workDir = filepath.Join(f.work, "repo")
				f.setAccountDir("personal", filepath.Join(f.work, "personal"))
				f.opt.Account = "work"
			}},

		// --- other accounts ---
		{name: "another account inside this one aliases it", err: fixed(errWorkAliases),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.setAccountDir("personal", filepath.Join(f.work, "personal"))
				f.opt.Account = "work"
			}},
		{name: "another account containing this one aliases it", err: fixed(errWorkAliases),
			setup: func(_ *testing.T, f *accountResolveFixture) {
				f.setAccountDir("personal", filepath.Dir(f.work))
				f.opt.Account = "work"
			}},
		{name: "another account linked to this one aliases it", err: fixed(errWorkAliases),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustSymlink(t, f.work, filepath.Join(f.root, "personal-link"))
				f.setAccountDir("personal", filepath.Join(f.root, "personal-link"))
				f.opt.Account = "work"
			}},
		{name: "this account linked to another aliases it", err: fixed(errWorkAliases),
			setup: func(t *testing.T, f *accountResolveFixture) {
				writeAccountLogin(t, f.personal, time.Now().Add(time.Hour).UnixMilli())
				mustSymlink(t, f.personal, filepath.Join(f.root, "work-link"))
				f.setAccountDir("work", filepath.Join(f.root, "work-link"))
				f.opt.Account = "work"
			}},
		{name: "another account that cannot be resolved fails the safety check", err: fixed(errSafetyCheckFailed),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "file"), "")
				f.setAccountDir("personal", filepath.Join(f.root, "file", "sub"))
				f.opt.Account = "work"
			}},
		{name: "order: aliasing before a symlinked state file", err: fixed(errWorkAliases),
			setup: func(t *testing.T, f *accountResolveFixture) {
				f.setAccountDir("personal", filepath.Join(f.work, "personal"))
				mustWrite(t, filepath.Join(f.root, "elsewhere.json"), "{}")
				mustSymlink(t, filepath.Join(f.root, "elsewhere.json"), filepath.Join(f.work, "settings.json"))
				f.opt.Account = "work"
			}},
	}...)

	// --- symlinked state files ---
	for _, name := range []string{".credentials.json", ".claude.json", "settings.json"} {
		cases = append(cases, accountResolveCase{name: "symlinked " + name + " is refused", err: fixed(errWorkStateSymlink),
			setup: func(t *testing.T, f *accountResolveFixture) {
				target := filepath.Join(f.root, "elsewhere", name)
				mustWrite(t, target, `{"claudeAiOauth":{"accessToken":"fixture-token"}}`)
				_ = os.Remove(filepath.Join(f.work, name))
				mustSymlink(t, target, filepath.Join(f.work, name))
				f.opt.Account = "work"
			}})
	}
	cases = append(cases, []accountResolveCase{
		{name: "dangling symlinked state is refused", err: fixed(errWorkStateSymlink),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustSymlink(t, filepath.Join(f.root, "missing.json"), filepath.Join(f.work, ".claude.json"))
				f.opt.Account = "work"
			}},
		{name: "account directory that is a file cannot be inspected", err: fixed(errWorkStateUninspectable),
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.root, "acct-file"), "")
				f.setAccountDir("work", filepath.Join(f.root, "acct-file"))
				f.opt.Account = "work"
			}},
		{name: "symlink check uses the configured spelling: a linked account folder is fine",
			want: func(f *accountResolveFixture) *store.AgentAccountIdentity {
				return &store.AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: filepath.Join(f.root, "work-link"), ResolvedDir: f.work, ConfigEnvSet: true}
			},
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustSymlink(t, f.work, filepath.Join(f.root, "work-link"))
				f.setAccountDir("work", filepath.Join(f.root, "work-link"))
				f.opt.Account = "work"
			}},
		{name: "order: symlinked state before the login check", err: fixed(errWorkStateSymlink),
			setup: func(t *testing.T, f *accountResolveFixture) {
				_ = os.Remove(filepath.Join(f.work, ".credentials.json"))
				mustSymlink(t, filepath.Join(f.root, "missing.json"), filepath.Join(f.work, ".credentials.json"))
				f.opt.Account = "work"
			}},

		// --- login ---
		{name: "missing login is refused with the login command", noDarwin: true,
			err: func(f *accountResolveFixture) string { return errWorkNoLogin(f.work) },
			setup: func(_ *testing.T, f *accountResolveFixture) {
				_ = os.Remove(filepath.Join(f.work, ".credentials.json"))
				f.opt.Account = "work"
			}},
		{name: "missing login names a quoted directory", noDarwin: true,
			err: func(f *accountResolveFixture) string { return errWorkNoLogin(filepath.Join(f.root, "with space")) },
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustMkdir(t, filepath.Join(f.root, "with space"))
				f.setAccountDir("work", filepath.Join(f.root, "with space"))
				f.opt.Account = "work"
			}},
		{name: "unparseable login is refused",
			err: func(f *accountResolveFixture) string { return errWorkNoLogin(f.work) },
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.work, ".credentials.json"), "not json")
				f.opt.Account = "work"
			}},
		{name: "login without an access token is refused", noDarwin: true,
			err: func(f *accountResolveFixture) string { return errWorkNoLogin(f.work) },
			setup: func(t *testing.T, f *accountResolveFixture) {
				mustWrite(t, filepath.Join(f.work, ".credentials.json"), `{"claudeAiOauth":{"expiresAt":1}}`)
				f.opt.Account = "work"
			}},
		{name: "credentials that are a folder are refused",
			err: func(f *accountResolveFixture) string { return errWorkNoLogin(f.work) },
			setup: func(t *testing.T, f *accountResolveFixture) {
				_ = os.Remove(filepath.Join(f.work, ".credentials.json"))
				mustMkdir(t, filepath.Join(f.work, ".credentials.json"))
				f.opt.Account = "work"
			}},
		{name: "expired login is refused", err: fixed(errWorkLoginExpired),
			setup: func(t *testing.T, f *accountResolveFixture) {
				writeAccountLogin(t, f.work, time.Now().Add(-time.Minute).UnixMilli())
				f.opt.Account = "work"
			}},
		{name: "login without an expiry is accepted", want: (*accountResolveFixture).workIdentity,
			setup: func(t *testing.T, f *accountResolveFixture) {
				writeAccountLogin(t, f.work, 0)
				f.opt.Account = "work"
			}},

		// --- success ---
		{name: "named account resolves its identity", want: (*accountResolveFixture).workIdentity, setup: named("work")},
		{name: "named account is unaffected by an unrelated CLAUDE_CONFIG_DIR", want: (*accountResolveFixture).workIdentity,
			setup: func(t *testing.T, f *accountResolveFixture) {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(f.root, "custom"))
				f.opt.Account = "work"
			}},
		{name: "resume with the saved named identity", want: (*accountResolveFixture).workIdentity,
			setup: func(_ *testing.T, f *accountResolveFixture) { f.opt.AccountIdentity = f.workIdentity() }},
	}...)
	return cases
}

func TestResolveClaudeAccountCharacterisation(t *testing.T) {
	for _, tc := range accountResolveCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noDarwin && runtime.GOOS == "darwin" {
				t.Skip("a missing named login is a Keychain refusal on darwin")
			}
			f := newAccountResolveFixture(t)
			tc.setup(t, f)
			got, err := f.svc.resolveClaudeAccount(context.Background(), f.opt, f.kind)
			switch {
			case tc.err != nil:
				want := tc.err(f)
				if err == nil {
					t.Fatalf("resolveClaudeAccount() = %+v, want error %q", got, want)
				}
				if err.Error() != want {
					t.Fatalf("error = %q\nwant    %q", err.Error(), want)
				}
				if got != nil {
					t.Fatalf("identity = %+v with an error, want nil", got)
				}
			case tc.wantNil:
				if err != nil || got != nil {
					t.Fatalf("resolveClaudeAccount() = %+v, %v; want nil, nil", got, err)
				}
			default:
				if err != nil {
					t.Fatalf("resolveClaudeAccount() error = %v", err)
				}
				want := tc.want(f)
				if got == nil || *got != *want {
					t.Fatalf("identity = %+v\nwant       %+v", got, want)
				}
			}
		})
	}
}
