package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentSandboxConfig runs agents in Legato's sandbox: bubblewrap around the
// agent tool, which then sees no credential of the person's. See
// docs/ai/sandbox.md.
type AgentSandboxConfig struct {
	// Enabled sandboxes every agent unless its spawn opts out. Default false.
	Enabled bool `yaml:"enabled"`
	// Backend builds the sandbox. Only "bwrap" for now.
	Backend string `yaml:"backend"`
	// DefaultMounts shows the person's toolchains (read-only) and package
	// caches (read-write) inside. Default true.
	DefaultMounts *bool `yaml:"default_mounts"`
	// Mounts are further paths every sandbox sees, read-only by default.
	Mounts []ResourcePathConfig `yaml:"mounts"`
}

// DefaultMountsOn reports whether the toolchain mounts are on.
func (c AgentSandboxConfig) DefaultMountsOn() bool { return c.DefaultMounts == nil || *c.DefaultMounts }

func normalizeAgentSandbox(cfg *Config) error {
	sb := &cfg.Agents.Sandbox
	switch strings.TrimSpace(sb.Backend) {
	case "", "bwrap":
		sb.Backend = "bwrap"
	default:
		return fmt.Errorf("agents.sandbox.backend: only bwrap is supported, got %q", sb.Backend)
	}
	home, _ := os.UserHomeDir()
	for i := range sb.Mounts {
		m := &sb.Mounts[i]
		path := strings.TrimSpace(m.Path)
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("agents.sandbox.mounts: path %q must be absolute or start with ~/", m.Path)
		}
		m.Path = filepath.Clean(path)
		switch m.Access {
		case "":
			m.Access = "read"
		case "read", "write":
		default:
			return fmt.Errorf("agents.sandbox.mounts: access of %q must be read or write, got %q", m.Path, m.Access)
		}
	}
	return nil
}
