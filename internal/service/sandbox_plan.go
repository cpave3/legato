package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
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

// SandboxWrapper puts one of Legato's wrappers (gh, az) in the place of
// every real command of its name: a login shell reorders PATH, so the
// wrapper cannot rely on coming first in it.
type SandboxWrapper struct {
	Wrapper   string   // the wrapper script
	Real      string   // the real command, which the wrapper runs
	RealMount string   // where the real command is mounted inside; LEGATO_GH_REAL, LEGATO_AZ_REAL
	Overlays  []string // the real paths of every command of its name on the host's PATH
}

// SandboxPlanInput is everything a sandbox's plan is made from.
type SandboxPlanInput struct {
	Home, WorkDir       string
	ExecutionID, TaskID string
	AgentKind           string
	RuntimeDir          string // <runtime>/legato
	// ScratchDir holds each execution's /tmp, home and throwaway layers, on
	// disk (agents.sandbox.scratch_dir); <DataDir>/sandbox when empty.
	ScratchDir          string
	DataDir, ConfigPath string
	BinaryPath          string
	BrokerSocket        string
	SSHSocket           string // the execution's own, when it has one
	// DockerSocket is the execution's own Docker API socket, filtered by
	// Legato's proxy (agents.sandbox.docker); its folder is mounted and
	// DOCKER_HOST names it.
	DockerSocket    string
	PastedImagesDir string
	AgentFilesDir   string // ~/.legato/agents/<task>: role prompt and brief
	GitCommonDir    string // a worktree's main .git, written by commits
	// JobDir is the job clone Legato made for the session's task, in its
	// data folder: the one part of that folder the sandbox may see.
	JobDir string
	// ReadOnlyCheckout mounts the working directory and its git folder
	// read-only, for an agent that may look but not change (a find-out
	// delegation).
	ReadOnlyCheckout bool
	// ThrowawayCheckout, with ReadOnlyCheckout, shows the checkout and its
	// git folder beneath a throwaway layer instead (a reviewer's): builds
	// and tests can write, and none of it reaches the host. Only for a
	// backend with overlays.
	ThrowawayCheckout bool
	// CheckoutCopyOf is the author's checkout when WorkDir is a throwaway
	// copy of it (a backend without overlays): it, and a git folder
	// outside the copy, are seen read-only.
	CheckoutCopyOf       string
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
	GH        SandboxWrapper
	// Azure is the credential az signs in with (the azure profile's first);
	// with one, Az puts Legato's az wrapper over every real az.
	Azure SandboxAzure
	Az    SandboxWrapper
	// Engram is the engram an agent started as runs: its binary and its
	// store. Zero for a session with no engram.
	Engram SandboxEngram
	FS     FSProbe
	// Layout is where this host keeps its system, toolchains, caches and
	// secrets; LinuxLayout when nil.
	Layout *HostLayout
	// Capabilities are the backend's; a plan uses only what it can render.
	// All of them (bwrap's) when nil.
	Capabilities *sandbox.Capabilities
}

// SandboxEngram is what an engram session needs inside the sandbox: the
// real engram binary from `engram where` (never a shim), bound read-only
// at BinMount (an empty file made first, in a folder that goes first on
// the session's PATH), and the whole store, read-write. It does not depend
// on default_mounts.
type SandboxEngram struct {
	Binary   string
	BinMount string
	Store    string
}

// linkDirs are the folders of commands where a tool is often a link to a
// file elsewhere (st in ~/.local/bin, linked to its checkout). The targets of
// their links are mounted read-only, when the folder is.
var linkDirs = []string{".local/bin", "go/bin", ".cargo/bin", ".bun/bin", ".deno/bin", ".volta/bin"}

// skillDirs are an agent tool's folders of skills, commands and agents,
// whose entries are often links to a shared folder (~/.agents/skills) that
// links on again (a registry's checkout). Only the agent's own tool's.
var skillDirs = map[string][]string{
	"claude-code": {".claude/skills", ".claude/commands", ".claude/agents"},
	"codex":       {".codex/skills", ".codex/prompts"},
	"chimera":     {".chimera/skills", ".chimera/commands", ".chimera/agents"},
}

// linkStep is one link of a chain: the link at Path (its folder resolved)
// and its target as written.
type linkStep struct{ Path, Target string }

// linkChain is where a link leads: each link on the way, and the real file
// or folder at its end.
type linkChain struct {
	Steps []linkStep
	Real  string
	Dir   bool
}

// linkLister reads links; HostFS is one.
type linkLister interface {
	// Links lists the links directly in dir.
	Links(dir string) []string
	// Chain follows a link to the file or folder it ends at.
	Chain(link string) (linkChain, bool)
}

func (HostFS) Links(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out
}

func (HostFS) Chain(link string) (linkChain, bool) {
	var chain linkChain
	path := link
	for range 40 {
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return linkChain{}, false
		}
		at := filepath.Join(parent, filepath.Base(path))
		info, err := os.Lstat(at)
		if err != nil {
			return linkChain{}, false
		}
		if info.Mode()&os.ModeSymlink == 0 {
			chain.Real, chain.Dir = at, info.IsDir()
			return chain, info.IsDir() || info.Mode().IsRegular()
		}
		target, err := os.Readlink(at)
		if err != nil {
			return linkChain{}, false
		}
		chain.Steps = append(chain.Steps, linkStep{Path: at, Target: target})
		if !filepath.IsAbs(target) {
			target = filepath.Join(parent, target)
		}
		path = filepath.Clean(target)
	}
	return linkChain{}, false
}

// HostLayout is where a host keeps what a sandbox shows or hides: its
// system, the person's toolchains and caches, each agent tool's state, and
// the credentials no sandbox sees. Paths in a home are relative to it.
type HostLayout struct {
	// SystemPaths are read-only. A path that is a symbolic link on the host
	// (/bin -> usr/bin) is the same link inside.
	SystemPaths []string
	// SystemToolchains are read-only, with default mounts.
	SystemToolchains []string
	// ToolchainsRO are the person's tools, read-only, and ToolchainsRW their
	// package and build caches, read-write, with default mounts. None holds
	// a credential.
	ToolchainsRO, ToolchainsRW []string
	// AgentState is each agent tool's own state, read-write: its login and
	// its sessions.
	AgentState map[string][]string
	// ForbiddenHome and ForbiddenSystem are what a sandbox never sees: the
	// person's credentials and sockets that hand out the host.
	ForbiddenHome, ForbiddenSystem []string
}

// linuxForbiddenHome are credentials in a Linux home, and the folders of
// Legato's own state; macOS has them too.
var linuxForbiddenHome = []string{".ssh", ".config/gh", ".gitconfig", ".git-credentials", ".config/git/credentials", ".netrc",
	".1password", ".config/op", ".config/legato", ".local/share/legato", ".docker", ".aws", ".azure",
	".kube", ".gnupg", ".password-store", ".local/share/keyrings", ".config/containers/auth.json",
	".pypirc", ".npmrc",
	// Package managers' registry logins; their caches are mounted.
	".yarnrc", ".yarnrc.yml", ".gradle/gradle.properties", ".m2/settings.xml", ".config/composer/auth.json", ".composer/auth.json"}

var linuxToolchainsRO = []string{".local/bin", ".local/share/mise", ".config/mise", "go/bin", ".cargo/bin", ".rustup", ".bun", ".deno", ".volta", ".nvm", ".pyenv"}
var linuxToolchainsRW = []string{"go/pkg/mod", ".cache/go-build", ".npm", ".cache/pnpm", ".local/share/pnpm", ".cache/pip", ".cache/uv", ".cargo/registry", ".cache/sccache", ".cache/ms-playwright", ".cache/node", ".nuget/packages", ".bun/install/cache",
	".local/state/pnpm", ".cache/yarn", ".yarn/berry/cache", ".cache/composer", ".gradle/caches", ".gradle/wrapper", ".m2/repository",
	".cache/deno", ".cache/pypoetry", ".cargo/git", ".cache/bun", ".cache/typescript", ".cache/prisma"}

// LinuxLayout is a Linux host's.
func LinuxLayout() HostLayout {
	return HostLayout{
		SystemPaths:      []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/etc", "/opt", "/home", "/run/systemd/resolve", "/var/lib/alternatives"},
		SystemToolchains: []string{"/home/linuxbrew/.linuxbrew"},
		ToolchainsRO:     linuxToolchainsRO,
		ToolchainsRW:     linuxToolchainsRW,
		AgentState: map[string][]string{
			"claude-code": {".claude", ".claude.json", ".cache/claude", ".local/share/claude", ".local/state/claude"},
			"codex":       {".codex"},
			"chimera":     {".chimera", ".config/chimera", ".local/share/chimera"},
		},
		ForbiddenHome:   linuxForbiddenHome,
		ForbiddenSystem: []string{"/var/run/docker.sock", "/run/docker.sock", "/run/podman"},
	}
}

// DarwinLayout is a Mac's, for the user uid. /etc, /tmp and /var are links
// into /private there, and paths are checked where they really are. The
// whole of /System is not shown: /System/Volumes/Data is the data volume,
// where every home folder also is.
func DarwinLayout(uid int) HostLayout {
	forbidden := append(append([]string{}, linuxForbiddenHome...),
		// The Keychain's files; its items are read through securityd, which
		// the profile closes.
		"Library/Keychains",
		// Browsers, password managers, mail, messages and iCloud: TCC asks
		// about the terminal app, and one with Full Disk Access passes that
		// on to everything it starts.
		"Library/Cookies", "Library/Safari", "Library/Mail", "Library/Messages", "Library/Mobile Documents",
		"Library/Application Support/Google/Chrome", "Library/Application Support/BraveSoftware", "Library/Application Support/Firefox",
		"Library/Application Support/Microsoft Edge", "Library/Application Support/Arc", "Library/Application Support/1Password",
		"Library/Group Containers/2BUA8C4S2C.com.1password", "Library/Containers/com.1password.1password",
		// Container runtimes' sockets and settings.
		"Library/Containers/com.docker.docker", "Library/Group Containers/group.com.docker", ".orbstack", ".colima", ".lima", ".rd",
		".local/share/containers")
	return HostLayout{
		SystemPaths: []string{"/System/Library", "/System/Cryptexes", "/System/iOSSupport", "/System/DriverKit", "/System/Volumes/Preboot/Cryptexes",
			"/usr", "/bin", "/sbin", "/Library", "/Applications", "/opt", "/nix", "/private/etc", "/etc", "/tmp", "/var",
			"/private/var/db/timezone", "/private/var/db/dyld", "/private/var/select", "/private/var/db/xcode_select_link"},
		ToolchainsRO: linuxToolchainsRO,
		ToolchainsRW: append(append([]string{}, linuxToolchainsRW...),
			"Library/Caches/go-build", "Library/Caches/pip", "Library/Caches/pypoetry", "Library/Caches/ms-playwright", "Library/Caches/Yarn",
			"Library/pnpm", "Library/Caches/pnpm", "Library/Caches/deno", "Library/Caches/Mozilla.sccache", "Library/Caches/typescript",
			"Library/Caches/node-gyp", "Library/Caches/CocoaPods", "Library/Caches/org.swift.swiftpm", "Library/Developer/Xcode/DerivedData"),
		AgentState: map[string][]string{
			"claude-code": {".claude", ".claude.json"},
			"codex":       {".codex"},
			"chimera":     {".chimera", ".config/chimera", ".local/share/chimera", "Library/Application Support/chimera"},
		},
		ForbiddenHome: forbidden,
		ForbiddenSystem: []string{"/Library/Keychains", "/private/var/run/docker.sock", "/System/Volumes/Data",
			// The shared /tmp: the tmux server's socket, launchd's ssh-agent,
			// every Legato's runtime folder. A sandbox has its own TMPDIR.
			"/private/tmp", fmt.Sprintf("/private/tmp/tmux-%d", uid)},
	}
}

// LayoutFor is the layout of a host running goos.
func LayoutFor(goos string, uid int) HostLayout {
	if goos == "darwin" {
		return DarwinLayout(uid)
	}
	return LinuxLayout()
}

func layoutOf(in SandboxPlanInput) HostLayout {
	if in.Layout != nil {
		return *in.Layout
	}
	return LinuxLayout()
}

func capabilitiesOf(in SandboxPlanInput) sandbox.Capabilities {
	if in.Capabilities != nil {
		return *in.Capabilities
	}
	return sandbox.Full()
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
	layout := layoutOf(in)
	// The configuration file itself: its folder may be the person's home.
	paths := []string{in.DataDir, in.ConfigPath}
	for _, rel := range layout.ForbiddenHome {
		paths = append(paths, filepath.Join(in.Home, rel))
	}
	paths = append(paths, layout.ForbiddenSystem...)
	// Every execution's /tmp and home: a sandbox sees its own only where
	// they are bound, never through the scratch folder.
	if root := scratchFor(in).Root; root != "" {
		paths = append(paths, root)
	}
	if in.RuntimeDir != "" {
		// The runtime folder holds every Legato's IPC socket, and the
		// person's (the session bus, Podman); only the broker's folder and
		// the session's own SSH socket are mounted. Without
		// XDG_RUNTIME_DIR it is /tmp/legato-<uid>, and /tmp is not forbidden.
		runtimeRoot := filepath.Dir(in.RuntimeDir)
		if runtimeRoot == "/tmp" || runtimeRoot == "/private/tmp" {
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

// mountDepth is how many folders down a mount lands inside the sandbox.
func mountDepth(m sandbox.Mount) int {
	dest := m.Dest
	if dest == "" {
		dest = m.Path
	}
	dest = filepath.Clean(dest)
	if dest == "/" {
		return 0
	}
	return strings.Count(dest, "/")
}

// inOthersScratch says whether path is at or inside the scratch folder and
// not this execution's own runtime or scratch folder: what the sandbox must
// not see whatever else allows it, as it holds every execution's /tmp and
// home. A backend that cannot remap shows the execution's own scratch
// folders where they are.
func inOthersScratch(in SandboxPlanInput, root, path string) bool {
	if root == "" || !within(path, root) {
		return false
	}
	if in.RuntimeDir != "" && in.ExecutionID != "" && within(path, filepath.Join(in.RuntimeDir, "sandbox", in.ExecutionID)) {
		return false
	}
	if in.ExecutionID != "" && within(path, scratchFor(in).Dir(in.ExecutionID)) {
		return false
	}
	return true
}

// allowedInside are mounts that may sit inside a forbidden folder: the parts
// of Legato's runtime and data folders a session needs.
func allowedInside(in SandboxPlanInput, path string) bool {
	own := ""
	if in.ExecutionID != "" && scratchFor(in).Root != "" {
		own = scratchFor(in).Dir(in.ExecutionID)
	}
	for _, ok := range []string{own, filepath.Dir(in.BrokerSocket), in.SSHSocket, in.CredentialWrapperDir, in.GHConfigDir, in.JobDir,
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
		in.ScratchDir, in.RuntimeDir = real(in.ScratchDir), real(in.RuntimeDir)
		in.BrokerSocket, in.SSHSocket, in.DockerSocket = real(in.BrokerSocket), real(in.SSHSocket), real(in.DockerSocket)
		in.JobDir, in.CheckoutCopyOf = real(in.JobDir), real(in.CheckoutCopyOf)
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
		in.Engram.Binary, in.Engram.Store, in.Engram.BinMount = real(in.Engram.Binary), real(in.Engram.Store), real(in.Engram.BinMount)
		in.Az.Wrapper, in.Az.Real, in.Az.RealMount = real(in.Az.Wrapper), real(in.Az.Real), real(in.Az.RealMount)
		extra := make([]SandboxPath, len(in.Extra))
		for i, e := range in.Extra {
			extra[i] = SandboxPath{Path: real(e.Path), Access: e.Access}
		}
		in.Extra = extra
	}
	no := forbidden(in)
	layout, caps := layoutOf(in), capabilitiesOf(in)
	scratchRoot := scratchFor(in).Root
	check := func(path string) error {
		// The scratch folder before the exemptions: one inside the
		// repository cache is no less private.
		if inOthersScratch(in, scratchRoot, path) {
			return fmt.Errorf("%s is in the sandboxes' scratch folder, which holds other agents' /tmp and home", path)
		}
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
	scratch := scratchFor(in)
	plan := sandbox.Plan{Home: in.Home, Chdir: in.WorkDir, Setenv: map[string]string{"LEGATO_SANDBOXED": "1", "TMPDIR": "/tmp"}}
	if scratch.Root != "" {
		plan.HomeSource = scratch.Home(in.ExecutionID)
	}
	if !caps.Remap {
		// The scratch tmp cannot be shown at /tmp: TMPDIR names it.
		plan.Setenv["TMPDIR"] = scratch.Tmp(in.ExecutionID)
	}
	if !caps.EmptyHome {
		// Nor can the home be shown empty: HOME names the scratch home, which
		// links to what the plan shows of the real one (plan.Links).
		if scratch.Root == "" {
			return sandbox.Plan{}, errors.New("this sandbox needs a scratch folder for the agent's home (agents.sandbox.scratch_dir)")
		}
		plan.Setenv["HOME"] = plan.HomeSource
	}
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
	for _, p := range layout.SystemPaths {
		// The home folder starts empty. A /home that holds it is made again
		// as the link it is on a host where it is one (var/home); a /home
		// that is a folder is not mounted, or the whole home would show.
		if within(in.Home, p) && in.FS.Probe(p).Link == "" {
			continue
		}
		add(p, sandbox.ReadOnly)
	}
	mounts = append(mounts, sshConfigMounts(in, caps.Remap)...)
	// /tmp is the execution's own, on the host's disk, so the host can read
	// what an agent writes there (a screenshot it shows), and none of it is
	// held in memory.
	if caps.Remap {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadWrite, Path: scratch.Tmp(in.ExecutionID), Dest: "/tmp"})
	} else {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadWrite, Path: scratch.Tmp(in.ExecutionID)})
	}
	if in.DefaultMounts {
		for _, p := range layout.SystemToolchains {
			add(p, sandbox.ReadOnly)
		}
		for _, rel := range layout.ToolchainsRO {
			add(filepath.Join(in.Home, rel), sandbox.ReadOnly)
		}
		for _, rel := range layout.ToolchainsRW {
			add(filepath.Join(in.Home, rel), sandbox.ReadWrite)
		}
	}
	for _, rel := range layout.AgentState[in.AgentKind] {
		add(filepath.Join(in.Home, rel), sandbox.ReadWrite)
	}
	for _, path := range in.ToolState {
		path = filepath.Clean(path)
		usual := false
		for _, rel := range layout.AgentState[in.AgentKind] {
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
	// A linked worktree's files often link into its main checkout (an
	// untracked settings file), so the main checkout is seen read-only,
	// mounted first so the worktree and the main .git stay writable on top.
	if main := mainCheckout(in.GitCommonDir); main != "" && !within(in.GitCommonDir, in.WorkDir) && check(main) == nil {
		add(main, sandbox.ReadOnly)
	}
	checkout := sandbox.ReadWrite
	if in.ReadOnlyCheckout {
		checkout = sandbox.ReadOnly
		if in.ThrowawayCheckout {
			checkout = sandbox.Overlay
		}
	}
	gitKind := checkout
	if in.CheckoutCopyOf != "" {
		// The author's checkout, read-only beside the copy; so is a git
		// folder outside the copy (a worktree's main .git).
		if err := check(in.CheckoutCopyOf); err != nil {
			return sandbox.Plan{}, err
		}
		add(in.CheckoutCopyOf, sandbox.ReadOnly)
		gitKind = sandbox.ReadOnly
	}
	add(in.WorkDir, checkout)
	if in.GitCommonDir != "" && !within(in.GitCommonDir, in.WorkDir) {
		if err := check(in.GitCommonDir); err != nil {
			return sandbox.Plan{}, err
		}
		add(in.GitCommonDir, gitKind)
	}
	if checkout == sandbox.ReadWrite {
		// The person's own git runs the checkout's hooks, outside the
		// sandbox: they are read-only (its config is checked when the agent
		// ends, SandboxGitConfig).
		add(gitHooksDir(in.WorkDir, in.GitCommonDir), sandbox.ReadOnly)
	}
	for _, p := range []string{in.AgentFilesDir, in.PastedImagesDir, in.CredentialWrapperDir, in.GHConfigDir, in.BinaryPath} {
		if p != "" {
			add(p, sandbox.ReadOnly)
		}
	}
	if in.BrokerSocket != "" {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: filepath.Dir(in.BrokerSocket)})
		plan.Sockets = append(plan.Sockets, filepath.Dir(in.BrokerSocket))
	}
	if in.SSHSocket != "" {
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.SSHSocket})
		plan.Sockets = append(plan.Sockets, in.SSHSocket)
	}
	if in.DockerSocket != "" {
		// The folder, so a socket made again by another serving process is
		// seen without a reload.
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: filepath.Dir(in.DockerSocket)})
		plan.Sockets = append(plan.Sockets, filepath.Dir(in.DockerSocket))
		plan.Setenv["DOCKER_HOST"] = "unix://" + in.DockerSocket
	}
	for _, w := range []struct {
		SandboxWrapper
		env string
	}{{in.GH, "LEGATO_GH_REAL"}, {in.Az, "LEGATO_AZ_REAL"}} {
		if w.Wrapper == "" || w.Real == "" || w.RealMount == "" || !in.FS.Probe(w.Wrapper).Exists || !in.FS.Probe(w.Real).Exists {
			continue
		}
		if caps.Remap {
			// The real command first, before its own path is covered by the wrapper.
			mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: w.Real, Dest: w.RealMount})
			for _, path := range w.Overlays {
				if in.FS.Probe(path).Exists {
					mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: w.Wrapper, Dest: path})
				}
			}
			plan.Setenv[w.env] = w.RealMount
		} else {
			// Nothing can be put over the real command: the wrapper is first
			// on PATH (the session's, and its shells' startup files), and runs
			// the real one where it is.
			mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: w.Wrapper}, sandbox.Mount{Kind: sandbox.ReadOnly, Path: w.Real})
			plan.Setenv[w.env] = w.Real
			if w.env == "LEGATO_AZ_REAL" && in.RuntimeDir != "" {
				// gh's wrapper folder is first on the session's PATH; az's
				// is linked into the execution's bin, which the launch line
				// puts first.
				plan.Links = append(plan.Links, sandbox.Link{Path: filepath.Join(sandboxBinDir(in.RuntimeDir, in.ExecutionID), "az"), Target: w.Wrapper})
			}
		}
		if w.env == "LEGATO_AZ_REAL" {
			// az keeps its account in the sandbox's own ~/.azure.
			plan.Unsetenv = append(plan.Unsetenv, "AZURE_CONFIG_DIR")
		}
	}
	if in.Engram.Store != "" {
		store := filepath.Clean(in.Engram.Store)
		if !in.FS.Probe(store).Exists {
			return sandbox.Plan{}, fmt.Errorf("engram's store %s is missing", store)
		}
		if err := check(store); err != nil {
			return sandbox.Plan{}, err
		}
		mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadWrite, Path: store})
		if in.Engram.Binary != "" && in.Engram.BinMount != "" && in.FS.Probe(in.Engram.Binary).Exists {
			if caps.Remap {
				mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.Engram.Binary, Dest: in.Engram.BinMount})
			} else {
				mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.Engram.Binary})
				plan.Links = append(plan.Links, sandbox.Link{Path: in.Engram.BinMount, Target: in.Engram.Binary})
			}
		}
	}
	mounts = append(mounts, linkTargetMounts(in, mounts, no)...)
	// Nothing at or inside the scratch folder is mounted by its path but
	// this execution's own: a default mount that is the scratch folder (a
	// cache, /opt) would show every execution the others' /tmp and home.
	if root := scratch.Root; root != "" {
		kept := mounts[:0]
		for _, m := range mounts {
			if m.Dest == "" && inOthersScratch(in, root, m.Path) {
				continue
			}
			kept = append(kept, m)
		}
		mounts = kept
	}
	// Hide each forbidden path that a mount above holds.
	var hides []sandbox.Mount
	seen := map[string]bool{}
	for _, f := range no {
		info := in.FS.Probe(f)
		// The scratch folder is covered even inside an exempt mount (the
		// repository cache).
		if !info.Exists || (allowedInside(in, f) && f != scratchRoot) || seen[f] {
			continue
		}
		seen[f] = true
		for _, m := range mounts {
			if !binds(m.Kind) {
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
	// bwrap mounts in order and a later mount covers an earlier one, so
	// every mount and cover goes after everything that holds it: ordered by
	// the depth of its path inside, the order above kept between equals. A
	// cover then lands on top of every mount that holds what it hides,
	// however deep those are put back (the repository cache inside the
	// data folder, inside a mounted home), and before any mount inside it
	// (the job clone, Legato's mod and its repository cache are in the data
	// folder), which it would otherwise cover in turn.
	plan.Mounts = append(mounts, hides...)
	sort.SliceStable(plan.Mounts, func(i, j int) bool {
		return mountDepth(plan.Mounts[i]) < mountDepth(plan.Mounts[j])
	})
	// A throwaway layer is on disk too, one folder per overlay.
	n := 0
	for i := range plan.Mounts {
		if plan.Mounts[i].Kind == sandbox.Overlay && scratch.Root != "" {
			plan.Mounts[i].Upper, plan.Mounts[i].Work = scratch.overlay(in.ExecutionID, n)
			n++
		}
	}
	if !caps.Remap && in.SSHConfigDir != "" && len(in.SSHConfig) > 0 {
		if in.KnownHosts != "" && in.FS.Probe(in.KnownHosts).Exists {
			// The ssh configuration copies name known_hosts in their folder.
			plan.Links = append(plan.Links, sandbox.Link{Path: filepath.Join(in.SSHConfigDir, "known_hosts"), Target: in.KnownHosts})
		}
		// ssh finds ~/.ssh by the person's passwd entry, whatever HOME
		// says, and nothing covers /etc/ssh: git's ssh reads the copy of
		// the system configuration, which names that known_hosts.
		for _, c := range in.SSHConfig {
			if filepath.Base(c.Original) == "ssh_config" && filepath.Base(filepath.Dir(c.Original)) == "ssh" {
				plan.Setenv["GIT_SSH_COMMAND"] = "ssh -F " + shellWord(c.Copy)
			}
		}
	}
	if !caps.EmptyHome {
		plan.Links = append(plan.Links, homeLinks(plan)...)
	}
	if in.GitIdentity[0] != "" {
		plan.Setenv["GIT_AUTHOR_NAME"], plan.Setenv["GIT_COMMITTER_NAME"] = in.GitIdentity[0], in.GitIdentity[0]
	}
	if in.GitIdentity[1] != "" {
		plan.Setenv["GIT_AUTHOR_EMAIL"], plan.Setenv["GIT_COMMITTER_EMAIL"] = in.GitIdentity[1], in.GitIdentity[1]
	}
	plan.Unsetenv = append(append([]string{}, secretVars...), plan.Unsetenv...)
	if in.DockerSocket != "" {
		// The execution's own socket stays; what points the docker command
		// at another daemon, or at the person's contexts, goes.
		plan.Unsetenv = slices.DeleteFunc(plan.Unsetenv, func(name string) bool { return name == "DOCKER_HOST" })
		plan.Unsetenv = append(plan.Unsetenv, "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG")
	}
	return plan, nil
}

// sshConfigMounts put the copies of the system ssh configuration over the
// originals, which the sandbox's user namespace shows as owned by nobody, so
// ssh would refuse them. The known_hosts file is mounted where the copies
// name it.
//
// A backend that cannot remap has no user namespace, so the originals keep
// their owners: only the copies' folder is shown, and known_hosts is linked
// where the copies name it.
func sshConfigMounts(in SandboxPlanInput, remap bool) []sandbox.Mount {
	if in.SSHConfigDir == "" || len(in.SSHConfig) == 0 {
		return nil
	}
	mounts := []sandbox.Mount{{Kind: sandbox.ReadOnly, Path: in.SSHConfigDir}}
	if !remap {
		// knownHostsLink names it where the copies say.
		if in.KnownHosts != "" && in.FS.Probe(in.KnownHosts).Exists {
			mounts = append(mounts, sandbox.Mount{Kind: sandbox.ReadOnly, Path: in.KnownHosts})
		}
		return mounts
	}
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

// linkTargetMounts makes the links in the mounted command folders, and in the
// agent tool's folders of skills, work inside: each link on the way that no
// mount holds is made again, and the file or folder at the end is mounted
// read-only (a command folder's links count only when they end at a file).
// A chain that passes through a forbidden path is left out.
func linkTargetMounts(in SandboxPlanInput, mounts []sandbox.Mount, forbiddenPaths []string) []sandbox.Mount {
	lister, ok := in.FS.(linkLister)
	if !ok {
		return nil
	}
	var out []sandbox.Mount
	covered := func(path string) bool {
		for _, m := range append(mounts, out...) {
			if binds(m.Kind) && m.Dest == "" && within(path, m.Path) {
				return true
			}
			if m.Kind == sandbox.Symlink && m.Path == path {
				return true
			}
		}
		return false
	}
	refused := func(path string) bool {
		for _, f := range forbiddenPaths {
			if within(path, f) {
				return true
			}
		}
		return false
	}
	follow := func(dir string, folders bool) {
		if !covered(dir) {
			return
		}
		for _, link := range lister.Links(dir) {
			chain, ok := lister.Chain(link)
			if !ok || (chain.Dir && !folders) || refused(chain.Real) {
				continue
			}
			bad := false
			for _, step := range chain.Steps {
				bad = bad || refused(step.Path)
			}
			if bad {
				continue
			}
			for _, step := range chain.Steps {
				if !covered(step.Path) {
					out = append(out, sandbox.Mount{Kind: sandbox.Symlink, Path: step.Path, Target: step.Target})
				}
			}
			if !covered(chain.Real) {
				out = append(out, sandbox.Mount{Kind: sandbox.ReadOnly, Path: chain.Real})
			}
		}
	}
	for _, rel := range linkDirs {
		follow(filepath.Join(in.Home, rel), false)
	}
	for _, rel := range skillDirs[in.AgentKind] {
		follow(filepath.Join(in.Home, rel), true)
	}
	return out
}

// mainCheckout is the working tree a worktree's common .git belongs to, or
// "" for a bare repository.
// binds says whether a mount shows a host folder's contents, which may hold
// what must be hidden or links to follow.
func binds(kind sandbox.Kind) bool {
	return kind == sandbox.ReadOnly || kind == sandbox.ReadWrite || kind == sandbox.Overlay
}

func mainCheckout(gitCommonDir string) string {
	if gitCommonDir == "" || filepath.Base(gitCommonDir) != ".git" {
		return ""
	}
	return filepath.Dir(gitCommonDir)
}

// homeLinks are the links of the agent's own home (plan.HomeSource) to
// what the plan shows of the real home, for a backend that cannot show it
// empty: each mount in the home that no other such mount holds, but the
// execution's own scratch folders.
func homeLinks(plan sandbox.Plan) []sandbox.Link {
	own := filepath.Dir(plan.HomeSource)
	var shown []string
	for _, m := range plan.Mounts {
		if binds(m.Kind) && m.Dest == "" && m.Path != plan.Home && within(m.Path, plan.Home) && !within(m.Path, own) {
			shown = append(shown, m.Path)
		}
	}
	sort.Strings(shown)
	var links []sandbox.Link
	for i, path := range shown {
		held := false
		for _, other := range shown[:i] {
			held = held || within(path, other)
		}
		if held {
			continue
		}
		rel, err := filepath.Rel(plan.Home, path)
		if err != nil {
			continue
		}
		links = append(links, sandbox.Link{Path: filepath.Join(plan.HomeSource, rel), Target: path})
	}
	return links
}

// sandboxBinDir is the execution's own folder of commands, first on the
// launch line's PATH: the engram binary, and on macOS the az wrapper.
func sandboxBinDir(runtimeDir, executionID string) string {
	return filepath.Join(runtimeDir, "sandbox", executionID, "bin")
}

// gitHooksDir is the hooks folder of a checkout: in its common .git (a
// worktree's main one), else in its own .git.
func gitHooksDir(workDir, gitCommonDir string) string {
	if gitCommonDir != "" {
		return filepath.Join(gitCommonDir, "hooks")
	}
	return filepath.Join(workDir, ".git", "hooks")
}
