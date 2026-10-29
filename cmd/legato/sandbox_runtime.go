package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/service"
)

// sandboxSettings is Legato's sandbox for the agents a process spawns, or
// nil when agents.sandbox is off.
func sandboxSettings(cfg *config.Config) *service.SandboxSettings {
	if !cfg.Agents.Sandbox.Enabled {
		return nil
	}
	home, _ := os.UserHomeDir()
	runtimeDir := ipc.SocketDir()
	extra := make([]service.SandboxPath, 0, len(cfg.Agents.Sandbox.Mounts))
	for _, m := range cfg.Agents.Sandbox.Mounts {
		extra = append(extra, service.SandboxPath{Path: m.Path, Access: m.Access})
	}
	// The agent tools' own installations, read-only: a tool linked from a
	// checkout (pnpm link) lives outside every default mount.
	for _, path := range toolInstallPaths([]string{"claude", "codex", "chimera"}) {
		extra = append(extra, service.SandboxPath{Path: path, Access: "read"})
	}
	return &service.SandboxSettings{
		Backend: sandbox.Bwrap{},
		Base: service.SandboxPlanInput{
			Home: home, RuntimeDir: runtimeDir,
			DataDir: config.ResolveDataDir(cfg), ConfigPath: config.ResolveConfigPath(),
			BinaryPath: legatoBinary(), DefaultMounts: cfg.Agents.Sandbox.DefaultMountsOn(), Extra: extra,
			CredentialWrapperDir: filepath.Join(runtimeDir, wrapperDirName), GHConfigDir: ghConfigDir(cfg, runtimeDir),
			GitIdentity: gitIdentity(),
		},
	}
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
