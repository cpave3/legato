package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/cpave3/legato/internal/engine/store"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/service"
)

// sandboxSettings is Legato's sandbox for the agents a process spawns, or
// nil when agents.sandbox is off. db reads the secrets settings name.
func sandboxSettings(cfg *config.Config, db *store.Store) *service.SandboxSettings {
	if !cfg.Agents.Sandbox.Enabled {
		return nil
	}
	home, _ := os.UserHomeDir()
	runtimeDir := ipc.SocketDir()
	extra := make([]service.SandboxPath, 0, len(cfg.Agents.Sandbox.Mounts))
	for _, m := range cfg.Agents.Sandbox.Mounts {
		extra = append(extra, service.SandboxPath{Path: m.Path, Access: m.Access})
	}
	// Each agent tool's own installation, read-only, for its own agents: a
	// tool linked from a checkout (pnpm link) lives outside every default
	// mount, and other agents have no business in that checkout.
	installs := map[string][]string{}
	for kind, command := range map[string]string{"claude-code": "claude", "codex": "codex", "chimera": "chimera"} {
		installs[kind] = toolInstallPaths([]string{command})
	}
	layout := service.LayoutFor(runtime.GOOS, os.Getuid())
	backend := sandboxBackend(cfg)
	var login func(context.Context, string) (map[string]string, error)
	var endProcesses func(context.Context, string)
	if backend.Name() == "seatbelt" {
		endProcesses = service.EndSandboxProcesses(service.DarwinProcessTable, syscall.Kill, 2*time.Second)
		var resolve service.SecretResolver
		if db != nil {
			resolve = settingSecrets(cfg, db)
		}
		login = sandboxToolLogin(cfg.Agents.Sandbox.ClaudeCodeToken, resolve, home, os.Getenv)
	}
	return &service.SandboxSettings{
		Backend:      backend,
		ToolLogin:    login,
		EndProcesses: endProcesses,
		GitConfig:    &service.SandboxGitConfig{Dir: filepath.Join(config.ResolveDataDir(cfg), "sandbox-git-config")},
		Base: service.SandboxPlanInput{
			Layout: &layout,
			Home:   home, RuntimeDir: runtimeDir, ScratchDir: config.ResolveSandboxScratchDir(cfg),
			DataDir: config.ResolveDataDir(cfg), ConfigPath: config.ResolveConfigPath(),
			BinaryPath: legatoBinary(), DefaultMounts: cfg.Agents.Sandbox.DefaultMountsOn(), Extra: extra,
			CredentialWrapperDir: filepath.Join(runtimeDir, wrapperDirName), GHConfigDir: ghConfigDir(cfg, runtimeDir),
			GitIdentity: gitIdentity(), KnownHosts: cfg.CredentialChannels.SSHAgent.KnownHosts, ToolInstalls: installs,
			Azure: service.SandboxAzureFor(cfg.Credentials),
		},
		Docker: sandboxDocker(cfg),
	}
}

// sandboxBackend is the configured backend: Seatbelt on macOS, which starts
// agents through this binary's `sandbox exec`, bubblewrap elsewhere.
func sandboxBackend(cfg *config.Config) sandbox.Backend {
	if cfg.Agents.Sandbox.Backend == "seatbelt" {
		return sandbox.Seatbelt{Launcher: legatoBinary()}
	}
	return sandbox.Bwrap{}
}

// sandboxScratch is where sandboxes keep their scratch, also when the
// sandbox is off now: what earlier agents left is still swept.
func sandboxScratch(cfg *config.Config) *service.SandboxScratch {
	return &service.SandboxScratch{Root: config.ResolveSandboxScratchDir(cfg), RuntimeDir: ipc.SocketDir()}
}

// gitIdentity is the person's git name and address, which commits in the
// sandbox need: ~/.gitconfig itself is not mounted, as it can hold
// credential helpers.
func gitIdentity() [2]string {
	read := func(key string) string {
		out, err := exec.Command("git", "config", "--global", key).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return [2]string{read("user.name"), read("user.email")}
}

// launcherPath is an absolute path a launcher script runs: a pnpm or npm
// shim names the script it starts.
var launcherPath = regexp.MustCompile(`(/[A-Za-z0-9._@+/-]+\.(?:c|m)?js)\b`)

// toolInstallPaths finds where each command on PATH is installed: the
// folder a link leads to, or the git checkout a launcher script runs from.
// Commands that are not installed are left out.
func toolInstallPaths(names []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, name := range names {
		found, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(found)
		if err != nil {
			continue
		}
		head := make([]byte, 4096)
		f, err := os.Open(real)
		if err != nil {
			continue
		}
		n, _ := f.Read(head)
		_ = f.Close()
		head = head[:n]
		if strings.HasPrefix(string(head), "#!") {
			// A launcher script: mount the checkout it runs from.
			for _, match := range launcherPath.FindAllStringSubmatch(string(head), -1) {
				target := match[1]
				// "$basedir/../../../../var/home/..." is the absolute path
				// after the dots.
				if i := strings.Index(target, "/../"); i >= 0 {
					target = target[strings.LastIndex(target, "/../")+3:]
				}
				if _, err := os.Stat(target); err == nil {
					add(gitRoot(filepath.Dir(target)))
				}
			}
			continue
		}
		if real != found {
			add(filepath.Dir(real))
		}
	}
	return out
}

// gitRoot is the git checkout that holds dir, or dir itself outside one.
func gitRoot(dir string) string {
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
	}
	return dir
}

// sandboxToolLogin is how agent tools log in inside the macOS sandbox,
// which closes the Keychain: Claude Code with the token
// agents.sandbox.claude_code_token names (CLAUDE_CODE_OAUTH_TOKEN), unless
// it has a login it can reach there (a credentials file, a key in the
// environment); Codex only with its login in a file. A tool that could not
// log in is not started, so an unattended agent never waits on a login.
func sandboxToolLogin(token string, resolve service.SecretResolver, home string, getenv func(string) string) func(context.Context, string) (map[string]string, error) {
	return func(ctx context.Context, agentKind string) (map[string]string, error) {
		switch agentKind {
		case "claude-code":
			if token == "" {
				if getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" || getenv("ANTHROPIC_API_KEY") != "" {
					return nil, nil
				}
				if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); err == nil {
					return nil, nil
				}
				return nil, errors.New("the macOS Keychain, where Claude Code keeps its login, is closed in Legato's sandbox: run `claude setup-token`, keep the token with `legato secret add claude-code-token`, and set agents.sandbox.claude_code_token to legato://claude-code-token")
			}
			if strings.HasPrefix(token, "legato://") || strings.HasPrefix(token, "op://") {
				if resolve == nil {
					return nil, fmt.Errorf("agents.sandbox.claude_code_token names %s, which cannot be read here", token)
				}
				value, err := resolve(ctx, token)
				if err != nil {
					return nil, fmt.Errorf("agents.sandbox.claude_code_token: %w", err)
				}
				token = value
			}
			return map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": strings.TrimSpace(token)}, nil
		case "codex":
			dir := getenv("CODEX_HOME")
			if dir == "" {
				dir = filepath.Join(home, ".codex")
			}
			data, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
			for _, line := range strings.Split(string(data), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok && strings.TrimSpace(key) == "cli_auth_credentials_store" && strings.Trim(strings.TrimSpace(value), `"'`) == "keyring" {
					return nil, errors.New("the macOS Keychain, where Codex keeps its login (cli_auth_credentials_store = \"keyring\"), is closed in Legato's sandbox: set it to \"file\" in ~/.codex/config.toml and run `codex login` again")
				}
			}
		}
		return nil, nil
	}
}
