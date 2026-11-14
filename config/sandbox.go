package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// AgentSandboxConfig runs agents in Legato's sandbox: bubblewrap (Linux) or
// Seatbelt (macOS) around the agent tool, which then sees no credential of
// the person's. See docs/ai/sandbox.md.
type AgentSandboxConfig struct {
	// Enabled sandboxes every agent unless its spawn opts out. Default false.
	Enabled bool `yaml:"enabled"`
	// Backend builds the sandbox: "bwrap" on Linux, "seatbelt" on macOS;
	// empty is this host's own.
	Backend string `yaml:"backend"`
	// DefaultMounts shows the person's toolchains (read-only) and package
	// caches (read-write) inside. Default true.
	DefaultMounts *bool `yaml:"default_mounts"`
	// Mounts are further paths every sandbox sees, read-only by default.
	Mounts []ResourcePathConfig `yaml:"mounts"`
	// ScratchDir holds each sandbox's /tmp, home folder and throwaway
	// layers, on disk, until its agent ends. Default <data_dir>/sandbox.
	ScratchDir string `yaml:"scratch_dir"`
	// Docker gives each sandboxed agent a filtered Docker API socket.
	Docker SandboxDockerConfig `yaml:"docker"`
	// ClaudeCodeToken names the secret (legato:// or op://) holding a
	// `claude setup-token` token, which sandboxed Claude Code sessions log
	// in with on macOS, where Claude Code keeps its own login in the
	// Keychain that the sandbox closes.
	ClaudeCodeToken string `yaml:"claude_code_token"`
}

// SandboxDockerConfig is the Docker API proxy of sandboxed agents: a socket
// per execution that passes to the real daemon only what keeps within the
// agent's sandbox.
type SandboxDockerConfig struct {
	// Enabled gives sandboxed agents the socket. Default false.
	Enabled bool `yaml:"enabled"`
	// Host is the daemon, as DOCKER_HOST names it (unix:// or tcp://);
	// empty: Legato's DOCKER_HOST, else the current Docker context, else
	// /var/run/docker.sock.
	Host string `yaml:"host"`
	// CapAdd are the capabilities containers may add. Load keeps them upper
	// case, without CAP_, each once.
	CapAdd []string `yaml:"cap_add"`
	// Publish is where published ports bind: loopback (127.0.0.1, the
	// default) or any.
	Publish string `yaml:"publish"`
}

// hostCapabilities reach past a container's namespaces to the host kernel,
// so no container of a sandboxed agent may add them.
var hostCapabilities = []string{"ALL", "SYS_ADMIN", "SYS_MODULE", "SYS_RAWIO", "SYS_BOOT", "SYS_TIME", "MAC_ADMIN",
	"MAC_OVERRIDE", "DAC_READ_SEARCH", "BPF", "PERFMON", "SYSLOG", "AUDIT_CONTROL", "AUDIT_READ", "WAKE_ALARM", "BLOCK_SUSPEND"}

func normalizeSandboxDocker(d *SandboxDockerConfig) error {
	d.Host = strings.TrimSpace(d.Host)
	if d.Host != "" && !strings.HasPrefix(d.Host, "unix:///") && !strings.HasPrefix(d.Host, "tcp://") {
		return fmt.Errorf("agents.sandbox.docker.host: %q: want unix:///path/to/socket or tcp://host:port", d.Host)
	}
	switch d.Publish {
	case "":
		d.Publish = "loopback"
	case "loopback", "any":
	default:
		return fmt.Errorf("agents.sandbox.docker.publish: want loopback or any, got %q", d.Publish)
	}
	var caps []string
	for _, c := range d.CapAdd {
		name := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(c)), "CAP_")
		if slices.Contains(hostCapabilities, name) {
			return fmt.Errorf("agents.sandbox.docker.cap_add: %s reaches the host past the container, and is never allowed", name)
		}
		if name != "" && !slices.Contains(caps, name) {
			caps = append(caps, name)
		}
	}
	d.CapAdd = caps
	return nil
}

// sandboxGOOS is the host's OS, which chooses the sandbox backend; tests
// change it.
var sandboxGOOS = runtime.GOOS

// DefaultSandboxBackend is the sandbox of a host running goos: Seatbelt
// (sandbox-exec) on macOS, bubblewrap elsewhere.
func DefaultSandboxBackend(goos string) string {
	if goos == "darwin" {
		return "seatbelt"
	}
	return "bwrap"
}

// ResolveSandboxScratchDir is where sandboxes keep their scratch.
func ResolveSandboxScratchDir(cfg *Config) string {
	if dir := cfg.Agents.Sandbox.ScratchDir; dir != "" {
		return dir
	}
	return filepath.Join(ResolveDataDir(cfg), "sandbox")
}

// DefaultMountsOn reports whether the toolchain mounts are on.
func (c AgentSandboxConfig) DefaultMountsOn() bool { return c.DefaultMounts == nil || *c.DefaultMounts }

func normalizeAgentSandbox(cfg *Config) error {
	sb := &cfg.Agents.Sandbox
	if err := normalizeSandboxDocker(&sb.Docker); err != nil {
		return err
	}
	switch backend := strings.TrimSpace(sb.Backend); backend {
	case "":
		sb.Backend = DefaultSandboxBackend(sandboxGOOS)
	case "bwrap", "seatbelt":
		if want := DefaultSandboxBackend(sandboxGOOS); backend != want {
			where := map[string]string{"bwrap": "Linux", "seatbelt": "macOS"}[backend]
			return fmt.Errorf("agents.sandbox.backend: %s runs on %s only; this host's is %s (or leave it empty)", backend, where, want)
		}
		sb.Backend = backend
	default:
		return fmt.Errorf("agents.sandbox.backend: want bwrap or seatbelt (or empty, for this host's own), got %q", sb.Backend)
	}
	home, _ := os.UserHomeDir()
	if dir := strings.TrimSpace(sb.ScratchDir); dir != "" {
		if dir == "~" {
			dir = home
		} else if strings.HasPrefix(dir, "~/") {
			dir = filepath.Join(home, dir[2:])
		}
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("agents.sandbox.scratch_dir: %q must be absolute or start with ~/", sb.ScratchDir)
		}
		sb.ScratchDir = filepath.Clean(dir)
		// Compared where they really are: a link to the repository cache is
		// the repository cache.
		scratch, realHome := realPath(sb.ScratchDir), realPath(home)
		if home != "" && (scratch == realHome || strings.HasPrefix(realHome, scratch+string(filepath.Separator)) || scratch == "/") {
			return fmt.Errorf("agents.sandbox.scratch_dir: %q holds the home folder; name a folder of its own", sb.ScratchDir)
		}
		// Folders of the data folder that sandboxes are shown: the scratch
		// folder in one would hide it from every agent.
		data := realPath(ResolveDataDir(cfg))
		for _, shared := range []string{"repos", "claude-mod", "jobs"} {
			dir := filepath.Join(data, shared)
			if scratch == dir || strings.HasPrefix(scratch, dir+string(filepath.Separator)) || strings.HasPrefix(dir, scratch+string(filepath.Separator)) {
				return fmt.Errorf("agents.sandbox.scratch_dir: %q overlaps %s, which sandboxes are shown; name a folder of its own, such as %s", sb.ScratchDir, dir, filepath.Join(data, "sandbox"))
			}
		}
	}
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

// realPath is path with its links resolved, as far as it exists: the part
// not made yet is joined to where the rest really is.
func realPath(path string) string {
	if path == "" {
		return path
	}
	rest := ""
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if dir == "/" || dir == "." {
			return filepath.Clean(path)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}
