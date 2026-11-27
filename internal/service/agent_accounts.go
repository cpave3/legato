package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/store"
)

func resolvedAccountRoot(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = resolvedAccountRoot(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

func accountRootsOverlap(a, b string) bool { return within(a, b) || within(b, a) }

// ClaudeAccountChoice names a configured subscription without its local paths.
type ClaudeAccountChoice struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
}

func (a *agentService) ClaudeAccountChoices() []ClaudeAccountChoice {
	choices := make([]ClaudeAccountChoice, 0, len(a.claudeAccounts))
	for _, account := range a.claudeAccounts {
		choices = append(choices, ClaudeAccountChoice{Name: account.Name, Label: a.claudeAccountLabel(account.Name), Provider: "claude"})
	}
	return choices
}

func (a *agentService) claudeAccountLabel(account string) string {
	for _, entry := range a.claudeAccounts {
		if entry.Name == account {
			if entry.Label != "" {
				return entry.Label
			}
			return entry.Name
		}
	}
	return account
}

var namedAccountAuthVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_PROFILE", "ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"}

func (a *agentService) resolveClaudeAccount(ctx context.Context, opt AgentSpawnOptions, kind string) (*store.AgentAccountIdentity, error) {
	if kind != "claude-code" {
		if opt.Account != "" || opt.AccountIdentity != nil {
			return nil, errors.New("a Claude account requires the claude-code agent; remove account or select claude-code")
		}
		return nil, nil
	}
	if opt.Account != "" && opt.AgentKind != "" && opt.AgentKind != "claude-code" {
		return nil, errors.New("a Claude account requires the claude-code agent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	envSet := dir != ""
	if !envSet {
		dir = filepath.Join(home, ".claude")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	account := opt.Account
	if account == "default" {
		account = ""
	}
	if opt.AccountIdentity != nil {
		saved := opt.AccountIdentity
		if saved.Provider != "claude" || (account != "" && account != saved.Account) || (opt.Account == "default" && saved.Account != "") {
			return nil, errors.New("saved Claude account conflicts with the selected account")
		}
		account = saved.Account
	}
	if account != "" {
		found := false
		for _, entry := range a.claudeAccounts {
			if entry.Name == account {
				dir, envSet, found = entry.ConfigDir, true, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Claude account %q is not configured on this instance; restore its configuration before resuming", account)
		}
	}
	resolved, err := resolvedAccountRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("Claude account %q directory cannot be resolved", account)
	}
	identity := &store.AgentAccountIdentity{Provider: "claude", Account: account, ConfigDir: dir, ResolvedDir: resolved, ConfigEnvSet: envSet}
	if opt.AccountIdentity != nil && *identity != *opt.AccountIdentity {
		return nil, errors.New("saved Claude account configuration changed; restore its original directory mapping before resuming")
	}
	if account == "" {
		return identity, nil
	}
	if err := namedAccountEnvConflict(account, opt.ExtraEnv); err != nil {
		return nil, err
	}
	if err := a.checkNamedAccountIsolation(account, dir, resolved, home, opt.WorkingDir); err != nil {
		return nil, err
	}
	if err := checkNamedAccountLogin(account, dir); err != nil {
		return nil, err
	}
	return identity, nil
}

// namedAccountEnvConflict refuses a named account when an auth/provider
// override is set, checking each key in order in this process's environment
// and then in the spawn's ExtraEnv.
func namedAccountEnvConflict(account string, extraEnv map[string]string) error {
	for _, key := range namedAccountAuthVars {
		if os.Getenv(key) != "" {
			return fmt.Errorf("Claude account %q conflicts with %s; remove that auth/provider override for a named subscription", account, key)
		}
		if extraEnv[key] != "" {
			return fmt.Errorf("Claude account %q conflicts with %s; remove that auth/provider override for a named subscription", account, key)
		}
	}
	return nil
}

// checkNamedAccountIsolation refuses a named account whose resolved root
// overlaps the default account's, a private root (credentials, tool state,
// Legato's data), contains a home, checkout or Legato state tree, or aliases
// another account; then any of its state files at dir that is a symlink.
// Aliases are compared resolved as well as spelled.
func (a *agentService) checkNamedAccountIsolation(account, dir, resolved, home, workingDir string) error {
	// Auth roots must not be reachable from another account or encompass a
	// home, checkout or Legato state tree. Check aliases as well as spelling.
	defaultDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if defaultDir == "" {
		defaultDir = filepath.Join(home, ".claude")
	}
	defaultDir, err := resolvedAccountRoot(defaultDir)
	if err != nil {
		return errors.New("default Claude directory cannot be resolved")
	}
	if accountRootsOverlap(resolved, defaultDir) {
		return fmt.Errorf("Claude account %q overlaps the default account directory", account)
	}
	privateRoots := append(forbidden(SandboxPlanInput{Home: home, DataDir: a.accountDataDir}), filepath.Join(home, ".local/share/claude"), filepath.Join(home, ".codex"), filepath.Join(home, ".chimera"))
	for _, path := range privateRoots {
		root, err := resolvedAccountRoot(path)
		if err != nil {
			return errors.New("Claude account directory safety check failed")
		}
		if accountRootsOverlap(resolved, root) {
			return fmt.Errorf("Claude account %q directory overlaps credentials, tool installation or protected Legato state", account)
		}
	}
	protected := []string{home, a.workDir, workingDir, a.accountDataDir}
	for _, path := range protected {
		if path == "" {
			continue
		}
		real, err := resolvedAccountRoot(path)
		if err != nil {
			return errors.New("Claude account directory safety check failed")
		}
		if within(real, resolved) || (path == a.accountDataDir && within(resolved, real)) {
			return fmt.Errorf("Claude account %q directory overlaps protected state or a working directory", account)
		}
	}
	for _, other := range a.claudeAccounts {
		if other.Name == account {
			continue
		}
		root, err := resolvedAccountRoot(other.ConfigDir)
		if err != nil {
			return errors.New("Claude account directory safety check failed")
		}
		if accountRootsOverlap(resolved, root) {
			return fmt.Errorf("Claude account %q directory aliases another account", account)
		}
	}
	for _, name := range []string{".credentials.json", ".claude.json", "settings.json"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Claude account %q state cannot be inspected", account)
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Claude account %q state contains a symlink; replace it with an account-local file", account)
		}
	}
	return nil
}

// checkNamedAccountLogin refuses a named account whose dir has no readable,
// unexpired subscription login (on darwin, a Keychain-only login passes).
func checkNamedAccountLogin(account, dir string) error {
	login, err := claudeusage.NamedCredentials(dir).Login()
	if err != nil && !(runtime.GOOS == "darwin" && errors.Is(err, claudeusage.ErrNamedKeychainUnavailable)) {
		return fmt.Errorf("Claude account %q has no readable subscription login; run CLAUDE_CONFIG_DIR=%s claude auth login --claudeai", account, shellWord(dir))
	}
	if err == nil && !login.ExpiresAt.IsZero() && !time.Now().Before(login.ExpiresAt) {
		return fmt.Errorf("Claude account %q login has expired; sign in to that directory with claude auth login --claudeai", account)
	}
	return nil
}

// accountLaunchCommand fixes identity at the child boundary: a pane's login
// shell may have replaced the environment tmux was given.
func accountLaunchCommand(command string, identity *store.AgentAccountIdentity) string {
	if identity == nil {
		return command
	}
	if !identity.ConfigEnvSet {
		return "env -u CLAUDE_CONFIG_DIR " + command
	}
	command = "env CLAUDE_CONFIG_DIR=" + shellWord(identity.ConfigDir) + " " + command
	if identity.Account == "" {
		return command
	}
	var checks []string
	for _, key := range namedAccountAuthVars {
		checks = append(checks, "${"+key+":-}")
	}
	return "if [ -n \"" + strings.Join(checks, "") + "\" ]; then printf '%s\\n' 'Claude account has a conflicting auth/provider override in the launch shell; remove it for this named subscription.' >&2; false; else " + command + "; fi"
}

func applyAccountEnvironment(env map[string]string, identity *store.AgentAccountIdentity) error {
	if identity == nil {
		return nil
	}
	if old, ok := env["CLAUDE_CONFIG_DIR"]; ok && old != "" && old != identity.ConfigDir {
		return errors.New("CLAUDE_CONFIG_DIR override conflicts with the selected Claude account")
	}
	if identity.ConfigEnvSet {
		env["CLAUDE_CONFIG_DIR"] = identity.ConfigDir
	} else {
		delete(env, "CLAUDE_CONFIG_DIR")
	}
	if identity.Account != "" {
		for _, key := range namedAccountAuthVars {
			if env[key] != "" {
				return fmt.Errorf("Claude account conflicts with %s; remove that auth override for a named subscription", key)
			}
		}
	}
	return nil
}
