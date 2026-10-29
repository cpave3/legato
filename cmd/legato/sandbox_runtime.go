package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
