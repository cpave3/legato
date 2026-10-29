package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// What an agent in Legato's sandbox sees. The rule: the system, its own
// work, its agent tool's own state, the person's toolchains, and Legato's
// sockets for it; never a credential of the person's, Legato's database or
// configuration, or a socket that gives the host away (Docker, Podman, the
// session bus). See docs/ai/sandbox.md.

// PathInfo is what the plan needs to know of a host path.
type PathInfo struct {
	Exists bool
	Dir    bool
	// Link is a symbolic link's target, "" when the path is not one.
	Link string
}

// FSProbe answers questions about host paths.
type FSProbe interface{ Probe(path string) PathInfo }

// HostFS probes the real filesystem.
type HostFS struct{}

// Real resolves symbolic links in a path that exists: /home/me is
// /var/home/me on a host whose /home is a link.
func (HostFS) Real(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	// A path that does not exist yet: resolve the part that does.
	dir, base := filepath.Split(filepath.Clean(path))
	if dir == "" || dir == path {
		return path
	}
	if real, err := filepath.EvalSymlinks(filepath.Clean(dir)); err == nil {
		return filepath.Join(real, base)
	}
	return path
}

func (HostFS) Probe(path string) PathInfo {
	info, err := os.Lstat(path)
	if err != nil {
		return PathInfo{}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		return PathInfo{Exists: true, Link: target}
	}
	return PathInfo{Exists: true, Dir: info.IsDir()}
}

// SandboxPath is a path a sandbox is given, "read" or "write".
type SandboxPath struct {
	Path   string
	Access string
}

// SandboxGH puts Legato's gh wrapper in the place of every real gh: a login
// shell reorders PATH, so the wrapper cannot rely on coming first in it.
type SandboxGH struct {
	Wrapper   string   // the wrapper script
	Real      string   // the real gh, which the wrapper runs
	RealMount string   // where the real gh is mounted inside; LEGATO_GH_REAL
	Overlays  []string // the real paths of every gh on the host's PATH
}

// SandboxPlanInput is everything a sandbox's plan is made from.
type SandboxPlanInput struct {
	Home, WorkDir        string
	ExecutionID, TaskID  string
	AgentKind            string
	RuntimeDir           string // <runtime>/legato
	DataDir, ConfigPath  string
	BinaryPath           string
	BrokerSocket         string
	SSHSocket            string // the execution's own, when it has one
	PastedImagesDir      string
	AgentFilesDir        string // ~/.legato/agents/<task>: role prompt and brief
	GitCommonDir         string // a worktree's main .git, written by commits
	GitIdentity          [2]string
	DefaultMounts        bool
	Extra                []SandboxPath // agents.sandbox.mounts and resources
	CredentialWrapperDir string        // the gh wrapper
	GHConfigDir          string        // the empty gh configuration
	SSHConfigDir         string        // where the copies of the system ssh configuration are
	SSHConfig            []SSHConfigCopy
	KnownHosts           string // the known_hosts file the SSH agent names hosts by
	// ToolInstalls are each agent tool's installation, by agent kind; a
	// sandbox sees only its own tool's, read-only.
	ToolInstalls map[string][]string
	// ToolState are state folders of the agent's tool beyond the usual ones
	// (CLAUDE_CONFIG_DIR, CODEX_HOME, a resumed session's), read-write.
	ToolState []string
	GH        SandboxGH
	FS        FSProbe
}

// systemPaths are the host's system, read-only. A path that is a symbolic
// link on the host (/bin -> usr/bin) is the same link inside.
var systemPaths = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/etc", "/opt", "/home", "/run/systemd/resolve", "/var/lib/alternatives"}

// toolchains are the person's tools, read-only, and their package and build
// caches, read-write. None holds a credential.
var toolchainsRO = []string{".local/bin", ".local/share/mise", ".config/mise", "go/bin", ".cargo/bin", ".rustup", ".bun", ".deno", ".volta", ".nvm", ".pyenv"}
var toolchainsRW = []string{"go/pkg/mod", ".cache/go-build", ".npm", ".cache/pnpm", ".local/share/pnpm", ".cache/pip", ".cache/uv", ".cargo/registry", ".cache/ms-playwright", ".cache/node"}
var systemToolchains = []string{"/home/linuxbrew/.linuxbrew"}

// agentState is each agent tool's own state, read-write: its login and its
// sessions.
var agentState = map[string][]string{
	"claude-code": {".claude", ".claude.json", ".cache/claude", ".local/share/claude", ".local/state/claude"},
	"codex":       {".codex"},
	"chimera":     {".chimera", ".config/chimera", ".local/share/chimera"},
}

// secretVars are left out of a sandbox's environment whatever the pane holds.
var secretVars = []string{
	"OP_SERVICE_ACCOUNT_TOKEN", "LEGATO_SECRETS_PASSPHRASE", "LEGATO_CONFIG", "GITHUB_TOKEN", "GITLAB_TOKEN",
	"LEGATO_JIRA_TOKEN", "JIRA_API_TOKEN", "LEGATO_GITHUB_PAT", "DBUS_SESSION_BUS_ADDRESS", "DOCKER_HOST",
	"CONTAINER_HOST", "TMUX", "TMUX_PANE", "TMUX_TMPDIR", "GNOME_KEYRING_CONTROL", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN", "NPM_TOKEN", "NODE_AUTH_TOKEN",
}

// forbidden are host paths a sandbox never sees: the person's credentials,
// Legato's own state, and sockets that hand out the host.
func forbidden(in SandboxPlanInput) []string {
	home := in.Home
	paths := []string{
		// The configuration file itself: its folder may be the person's home.
		in.DataDir, in.ConfigPath,
		filepath.Join(home, ".ssh"), filepath.Join(home, ".config/gh"), filepath.Join(home, ".gitconfig"),
		filepath.Join(home, ".git-credentials"), filepath.Join(home, ".config/git/credentials"), filepath.Join(home, ".netrc"),
		filepath.Join(home, ".1password"), filepath.Join(home, ".config/op"), filepath.Join(home, ".config/legato"),
		filepath.Join(home, ".local/share/legato"), filepath.Join(home, ".docker"), filepath.Join(home, ".aws"),
		filepath.Join(home, ".kube"), filepath.Join(home, ".gnupg"), filepath.Join(home, ".password-store"),
		filepath.Join(home, ".local/share/keyrings"), filepath.Join(home, ".config/containers/auth.json"),
		filepath.Join(home, ".pypirc"), filepath.Join(home, ".npmrc"),
		"/var/run/docker.sock", "/run/docker.sock", "/run/podman",
	}
	if in.RuntimeDir != "" {
		// The runtime folder holds every Legato's IPC socket, and the
		// person's (the session bus, Podman); only the broker's folder and
		// the session's own SSH socket are mounted. Without
		// XDG_RUNTIME_DIR it is /tmp/legato-<uid>, and /tmp is not forbidden.
		runtimeRoot := filepath.Dir(in.RuntimeDir)
		if runtimeRoot == "/tmp" {
			runtimeRoot = in.RuntimeDir
		}
		paths = append(paths, runtimeRoot)
	}
	out := paths[:0]
	for _, p := range paths {
		if p != "" && p != "." && p != "/" {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// allowedInside are mounts that may sit inside a forbidden folder: the parts
// of Legato's runtime and data folders a session needs.
func allowedInside(in SandboxPlanInput, path string) bool {
	for _, ok := range []string{filepath.Dir(in.BrokerSocket), in.SSHSocket, in.CredentialWrapperDir, in.GHConfigDir,
		filepath.Join(in.DataDir, "claude-mod"), filepath.Join(in.DataDir, "repos"), filepath.Join(in.RuntimeDir, "sandbox", in.ExecutionID)} {
		if ok != "" && ok != "." && within(path, ok) {
			return true
		}
	}
	return false
}

// BuildSandboxPlan makes the plan of one agent's sandbox. It refuses a
// working directory or a mount that is, or is inside, a forbidden path, and
// hides each forbidden path that falls inside a mount.
func BuildSandboxPlan(in SandboxPlanInput) (sandbox.Plan, error) {
	if in.FS == nil {
		in.FS = HostFS{}
	}
	// Every path is checked and mounted where it really is, so a link (a
	// /home that is var/home) can neither hide a credential from the check
	// nor clash with the system's own links.
	if resolver, ok := in.FS.(interface{ Real(string) string }); ok {
		real := func(p string) string {
			if p == "" {
				return p
			}
			return resolver.Real(p)
		}
		in.Home, in.WorkDir, in.DataDir, in.ConfigPath = real(in.Home), real(in.WorkDir), real(in.DataDir), real(in.ConfigPath)
		in.BinaryPath, in.GitCommonDir, in.AgentFilesDir, in.PastedImagesDir = real(in.BinaryPath), real(in.GitCommonDir), real(in.AgentFilesDir), real(in.PastedImagesDir)
		in.CredentialWrapperDir, in.GHConfigDir = real(in.CredentialWrapperDir), real(in.GHConfigDir)
		in.SSHConfigDir, in.KnownHosts = real(in.SSHConfigDir), real(in.KnownHosts)
		installs := make(map[string][]string, len(in.ToolInstalls))
		for kind, paths := range in.ToolInstalls {
			for _, p := range paths {
				installs[kind] = append(installs[kind], real(p))
			}
		}
		in.ToolInstalls = installs
		state := make([]string, len(in.ToolState))
		for i, p := range in.ToolState {
			state[i] = real(p)
		}
		in.ToolState = state
		in.GH.Wrapper, in.GH.Real, in.GH.RealMount = real(in.GH.Wrapper), real(in.GH.Real), real(in.GH.RealMount)
		overlays := make([]string, len(in.GH.Overlays))
		for i, p := range in.GH.Overlays {
			overlays[i] = real(p)
		}
		in.GH.Overlays = overlays
		extra := make([]SandboxPath, len(in.Extra))
		for i, e := range in.Extra {
			extra[i] = SandboxPath{Path: real(e.Path), Access: e.Access}
		}
		in.Extra = extra
	}
	no := forbidden(in)
	check := func(path string) error {
		if allowedInside(in, path) {
			return nil
		}
		for _, f := range no {
			if within(path, f) {
				return fmt.Errorf("%s holds credentials or Legato's own state, which an agent in the sandbox must not see", path)
			}
		}
		return nil
	}
	plan := sandbox.Plan{Home: in.Home, Chdir: in.WorkDir, Setenv: map[string]string{"LEGATO_SANDBOXED": "1", "TMPDIR": "/tmp"}}
	var mounts []sandbox.Mount
	add := func(path string, kind sandbox.Kind) {
		info := in.FS.Probe(path)
		if !info.Exists {
			return
		}
		if info.Link != "" {
			mounts = append(mounts, sandbox.Mount{Kind: sandbox.Symlink, Path: path, Target: info.Link})
			return
		}
		mounts = append(mounts, sandbox.Mount{Kind: kind, Path: path})
	}
	for _, p := range systemPaths {
		add(p, sandbox.ReadOnly)
	}
	mounts = append(mounts, sshConfigMounts(in)...)
	// /tmp is the execution's own, on the host, so the host can read what an
	// agent writes there (a screenshot it shows).
	mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadWrite, Path: filepath.Join(in.RuntimeDir, "sandbox", in.ExecutionID, "tmp"), Dest: "/tmp"})
	if in.DefaultMounts {
		for _, p := range systemToolchains {
			add(p, sandbox.ReadOnly)
		}
		for _, rel := range toolchainsRO {
			add(filepath.Join(in.Home, rel), sandbox.ReadOnly)
		}
		for _, rel := range toolchainsRW {
			add(filepath.Join(in.Home, rel), sandbox.ReadWrite)
		}
	}
	for _, rel := range agentState[in.AgentKind] {
		add(filepath.Join(in.Home, rel), sandbox.ReadWrite)
	}
	for _, path := range in.ToolState {
		path = filepath.Clean(path)
		usual := false
		for _, rel := range agentState[in.AgentKind] {
			usual = usual || path == filepath.Join(in.Home, rel)
		}
		if usual {
			continue
		}
		if err := check(path); err != nil {
			return sandbox.Plan{}, err
		}
		add(path, sandbox.ReadWrite)
	}
	for _, path := range in.ToolInstalls[in.AgentKind] {
		path = filepath.Clean(path)
		if err := check(path); err != nil {
			return sandbox.Plan{}, err
		}
		add(path, sandbox.ReadOnly)
	}
	if in.DataDir != "" {
		add(filepath.Join(in.DataDir, "claude-mod"), sandbox.ReadOnly)
		add(filepath.Join(in.DataDir, "repos"), sandbox.ReadOnly)
	}
	for _, extra := range in.Extra {
		path := filepath.Clean(extra.Path)
		if err := check(path); err != nil {
			return sandbox.Plan{}, err
		}
		kind := sandbox.ReadOnly
		if extra.Access == "write" {
			kind = sandbox.ReadWrite
		}
		add(path, kind)
	}
	if err := check(filepath.Clean(in.WorkDir)); err != nil {
		return sandbox.Plan{}, err
	}
	add(in.WorkDir, sandbox.ReadWrite)
	if in.GitCommonDir != "" && !within(in.GitCommonDir, in.WorkDir) {
		if err := check(in.GitCommonDir); err != nil {
			return sandbox.Plan{}, err
		}
		add(in.GitCommonDir, sandbox.ReadWrite)
	}
	for _, p := range []string{in.AgentFilesDir, in.PastedImagesDir, in.CredentialWrapperDir, in.GHConfigDir, in.BinaryPath} {
		if p != "" {
			add(p, sandbox.ReadOnly)
		}
	}
	if in.BrokerSocket != "" {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: filepath.Dir(in.BrokerSocket)})
	}
	if in.SSHSocket != "" {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.SSHSocket})
	}
	if gh := in.GH; gh.Wrapper != "" && gh.Real != "" && gh.RealMount != "" && in.FS.Probe(gh.Wrapper).Exists && in.FS.Probe(gh.Real).Exists {
		// The real gh first, before its own path is covered by the wrapper.
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: gh.Real, Dest: gh.RealMount})
		for _, path := range gh.Overlays {
			if in.FS.Probe(path).Exists {
				mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: gh.Wrapper, Dest: path})
			}
		}
		plan.Setenv["LEGATO_GH_REAL"] = gh.RealMount
	}
	// Hide each forbidden path that a mount above holds, after that mount.
	var hides []sandbox.Mount
	for _, f := range no {
		info := in.FS.Probe(f)
		if !info.Exists || allowedInside(in, f) {
			continue
		}
		for _, m := range mounts {
			if m.Kind != sandbox.ReadOnly && m.Kind != sandbox.ReadWrite {
				continue
			}
			if m.Dest == "" && within(f, m.Path) && f != m.Path {
				kind := sandbox.Hide
				if info.Dir {
					kind = sandbox.HideDir
				}
				hides = append(hides, sandbox.Mount{Kind: kind, Path: f})
				break
			}
		}
	}
	plan.Mounts = append(mounts, hides...)
	if in.GitIdentity[0] != "" {
		plan.Setenv["GIT_AUTHOR_NAME"], plan.Setenv["GIT_COMMITTER_NAME"] = in.GitIdentity[0], in.GitIdentity[0]
	}
	if in.GitIdentity[1] != "" {
		plan.Setenv["GIT_AUTHOR_EMAIL"], plan.Setenv["GIT_COMMITTER_EMAIL"] = in.GitIdentity[1], in.GitIdentity[1]
	}
	plan.Unsetenv = append([]string{}, secretVars...)
	return plan, nil
}

// sshConfigMounts put the copies of the system ssh configuration over the
// originals, which the sandbox's user namespace shows as owned by nobody, so
// ssh would refuse them. The known_hosts file is mounted where the copies
// name it.
func sshConfigMounts(in SandboxPlanInput) []sandbox.Mount {
	if in.SSHConfigDir == "" || len(in.SSHConfig) == 0 {
		return nil
	}
	mounts := []sandbox.Mount{{Kind: sandbox.ReadOnly, Path: in.SSHConfigDir}}
	for _, c := range in.SSHConfig {
		if in.FS.Probe(c.Original).Exists {
			mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: c.Copy, Dest: c.Original})
		}
	}
	if in.KnownHosts != "" && in.FS.Probe(in.KnownHosts).Exists {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.KnownHosts, Dest: filepath.Join(in.SSHConfigDir, "known_hosts")})
	}
	return mounts
}
