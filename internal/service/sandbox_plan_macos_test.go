package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// macFS is a Mac: /etc, /tmp and /var are links into /private, the runtime
// folder is in /private/tmp (no XDG_RUNTIME_DIR), Homebrew is in /opt.
func macFS() fakeFS {
	return fakeFS{
		"/System/Library": "dir", "/System/Cryptexes": "dir", "/usr": "dir", "/bin": "dir", "/sbin": "dir", "/Library": "dir",
		"/Library/Keychains": "dir", "/Applications": "dir", "/opt": "dir", "/private/etc": "dir", "/etc": "link:private/etc",
		"/tmp": "link:private/tmp", "/var": "link:private/var", "/private/var/db/timezone": "dir", "/private/var/select": "dir",
		"/Users/me": "dir", "/Users/me/.claude": "dir", "/Users/me/.claude.json": "file", "/Users/me/.ssh": "dir",
		"/Users/me/.config/gh": "dir", "/Users/me/.local/bin": "dir", "/Users/me/go/pkg/mod": "dir",
		"/Users/me/Library": "dir", "/Users/me/Library/Keychains": "dir", "/Users/me/Library/Caches/go-build": "dir",
		"/Users/me/Library/Application Support": "dir", "/Users/me/Library/Application Support/Google/Chrome": "dir",
		"/Users/me/Projects/app": "dir", "/Users/me/.config/legato": "dir", "/Users/me/.local/share/legato": "dir",
		"/Users/me/.local/share/legato/claude-mod": "dir", "/Users/me/.local/share/legato/sandbox": "dir",
		"/private/tmp/legato-501/broker": "dir", "/private/tmp/legato-501/sandbox/ex-1": "dir",
	}
}

// macInput is a sandboxed spawn on a Mac, for Seatbelt: it cannot show a
// path elsewhere, an empty home or a throwaway layer.
func macInput() SandboxPlanInput {
	layout := DarwinLayout(501)
	return SandboxPlanInput{
		Home: "/Users/me", WorkDir: "/Users/me/Projects/app", ExecutionID: "ex-1", TaskID: "t1", AgentKind: "claude-code",
		RuntimeDir: "/private/tmp/legato-501", DataDir: "/Users/me/.local/share/legato", ConfigPath: "/Users/me/.config/legato/config.yaml",
		BinaryPath: "/Users/me/.local/bin/legato", BrokerSocket: "/private/tmp/legato-501/broker/broker.sock",
		SSHSocket: "/private/tmp/legato-501/ssh/ex-1.sock", DefaultMounts: true, FS: macFS(),
		Layout: &layout, Capabilities: &sandbox.Capabilities{},
	}
}

func TestOnAMacThePlanShowsEveryPathWhereItIs(t *testing.T) {
	plan, err := BuildSandboxPlan(macInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan.Mounts {
		if m.Dest != "" && m.Dest != m.Path {
			t.Errorf("%s is shown at %s, which Seatbelt cannot do", m.Path, m.Dest)
		}
		if m.Kind == sandbox.Overlay {
			t.Errorf("%s is overlaid", m.Path)
		}
	}
	m := mounted(plan)
	for path, kind := range map[string]sandbox.Kind{
		"/System/Library": sandbox.ReadOnly, "/usr": sandbox.ReadOnly, "/private/etc": sandbox.ReadOnly, "/opt": sandbox.ReadOnly,
		"/Users/me/Projects/app": sandbox.ReadWrite, "/Users/me/.claude": sandbox.ReadWrite,
		"/Users/me/Library/Caches/go-build": sandbox.ReadWrite, "/Users/me/.local/bin": sandbox.ReadOnly,
	} {
		if got, ok := m[path]; !ok || got.Kind != kind {
			t.Errorf("%s = %+v, %v; want kind %d", path, got, ok, kind)
		}
	}
	if _, ok := m["/Users/me"]; ok {
		t.Error("the whole home folder is shown")
	}
}

// Seatbelt cannot put the scratch folders at /tmp and the home: the agent
// is told where they are instead, and its home holds links to what the
// plan shows of the real one.
func TestOnAMacTheAgentsTmpAndHomeAreNamedByItsEnvironment(t *testing.T) {
	in := macInput()
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	scratch := scratchFor(in)
	if plan.Setenv["TMPDIR"] != scratch.Tmp("ex-1") || plan.Setenv["HOME"] != scratch.Home("ex-1") {
		t.Fatalf("TMPDIR %q HOME %q", plan.Setenv["TMPDIR"], plan.Setenv["HOME"])
	}
	if got := mounted(plan)[scratch.Tmp("ex-1")]; got.Kind != sandbox.ReadWrite {
		t.Fatalf("the scratch tmp is %+v", got)
	}
	links := map[string]string{}
	for _, l := range plan.Links {
		links[l.Path] = l.Target
	}
	home := scratch.Home("ex-1")
	for rel, target := range map[string]string{
		".claude": "/Users/me/.claude", ".claude.json": "/Users/me/.claude.json", ".local/bin": "/Users/me/.local/bin",
		"Library/Caches/go-build": "/Users/me/Library/Caches/go-build", "Projects/app": "/Users/me/Projects/app",
	} {
		if got := links[filepath.Join(home, rel)]; got != target {
			t.Errorf("home link %s -> %q, want %s", rel, got, target)
		}
	}
	for path := range links {
		if !within(path, home) && path != filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "ssh", "known_hosts") {
			t.Errorf("a link outside the execution's own folders: %s", path)
		}
	}
	if _, ok := links[filepath.Join(home, ".ssh")]; ok {
		t.Error("~/.ssh is linked into the home")
	}
}

func TestOnAMacTheKeychainsAndBrowsersAreNeverShown(t *testing.T) {
	in := macInput()
	in.Extra = []SandboxPath{{Path: "/Users/me/Library/Application Support", Access: "read"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	covered := false
	for _, m := range plan.Mounts {
		covered = covered || ((m.Kind == sandbox.Hide || m.Kind == sandbox.HideDir) && m.Path == "/Users/me/Library/Application Support/Google/Chrome")
	}
	if !covered {
		t.Error("a resource that holds Chrome's profile does not hide it")
	}
	for _, path := range []string{"/Users/me/Library/Keychains", "/Library/Keychains/System.keychain", "/Users/me/Library"} {
		in := macInput()
		in.Extra = []SandboxPath{{Path: path, Access: "read"}}
		plan, err := BuildSandboxPlan(in)
		if err != nil {
			continue // refused
		}
		hidden := false
		for _, m := range plan.Mounts {
			hidden = hidden || ((m.Kind == sandbox.Hide || m.Kind == sandbox.HideDir) && within(m.Path, "/Users/me/Library/Keychains") || m.Path == "/Library/Keychains")
		}
		if !hidden {
			t.Errorf("a mount of %s shows a keychain", path)
		}
	}
}

// The Unix sockets the agent may connect to: the broker's folder, its own
// SSH agent socket, and its own Docker socket's folder.
func TestThePlanNamesTheSocketsTheAgentMayReach(t *testing.T) {
	in := macInput()
	in.DockerSocket = "/private/tmp/legato-501/sandbox/ex-1/docker/docker.sock"
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/private/tmp/legato-501/broker", "/private/tmp/legato-501/ssh/ex-1.sock", "/private/tmp/legato-501/sandbox/ex-1/docker"}
	if strings.Join(plan.Sockets, " ") != strings.Join(want, " ") {
		t.Fatalf("sockets %q, want %q", plan.Sockets, want)
	}
}

// Without remapping, the gh wrapper is not put over every real gh: it is
// first on PATH, and LEGATO_GH_REAL names the real one where it is.
func TestOnAMacTheGHWrapperRunsTheRealGHWhereItIs(t *testing.T) {
	in := macInput()
	fs := macFS()
	fs["/private/tmp/legato-501/bin/gh"] = "file"
	fs["/opt/homebrew/bin/gh"] = "file"
	in.FS = fs
	in.CredentialWrapperDir = "/private/tmp/legato-501/bin"
	in.GH = SandboxWrapper{Wrapper: "/private/tmp/legato-501/bin/gh", Real: "/opt/homebrew/bin/gh", RealMount: "/private/tmp/legato-501/sandbox/ex-1/gh-real", Overlays: []string{"/opt/homebrew/bin/gh"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Setenv["LEGATO_GH_REAL"] != "/opt/homebrew/bin/gh" {
		t.Fatalf("LEGATO_GH_REAL = %q", plan.Setenv["LEGATO_GH_REAL"])
	}
	if got := mounted(plan)["/private/tmp/legato-501/bin"]; got.Kind != sandbox.ReadOnly {
		t.Fatalf("the wrapper's folder is %+v", got)
	}
}

// A Mac's shared /tmp holds the tmux server's socket and every Legato's IPC
// sockets: nothing inside it is shown but the execution's own.
func TestOnAMacTheSharedTmpIsNeverShown(t *testing.T) {
	in := macInput()
	in.Extra = []SandboxPath{{Path: "/private/tmp/tmux-501", Access: "read"}}
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Fatal("the tmux server's folder was given to the sandbox")
	}
}

// A Mac's plan renders as a Seatbelt profile, and nothing it allows is a
// credential or Legato's own state, but the parts a session needs.
func TestAMacPlanRendersAsASeatbeltProfileThatAllowsNoCredential(t *testing.T) {
	in := macInput()
	fs := macFS()
	fs["/Users/me/.local/share/legato/repos"] = "dir"
	in.FS = fs
	in.Extra = []SandboxPath{{Path: "/Users/me/Library/Application Support", Access: "read"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	profile, params, err := sandbox.Seatbelt{}.Profile(plan, "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]string{}
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		value[k] = v
	}
	forbiddenPaths := forbidden(in)
	for _, line := range strings.Split(profile, "\n") {
		if !strings.HasPrefix(line, "(allow file-read*") {
			continue
		}
		for name, path := range value {
			if !strings.Contains(line, `(param "`+name+`")`) {
				continue
			}
			for _, f := range forbiddenPaths {
				if within(path, f) && !allowedInside(in, path) && !within(path, scratchFor(in).Dir("ex-1")) {
					t.Errorf("the profile allows %s, inside %s", path, f)
				}
			}
		}
	}
	if !strings.Contains(profile, "(deny file-read* file-write* (subpath (param") {
		t.Error("nothing is covered, though a resource holds Chrome's profile")
	}
}

// ssh reads the person's ~/.ssh by their passwd entry, whatever HOME says,
// and nothing can be put over /etc/ssh: git's ssh is given the copy of the
// system configuration, which names Legato's known_hosts.
func TestOnAMacGitsSSHReadsTheConfigurationCopy(t *testing.T) {
	in := macInput()
	fs := macFS()
	dir := "/private/tmp/legato-501/sandbox/ex-1/ssh"
	fs[dir] = "dir"
	fs[dir+"/ssh_config"] = "file"
	fs["/private/etc/ssh/ssh_config"] = "file"
	fs["/Users/me/.config/legato/known_hosts"] = "file"
	in.FS = fs
	in.SSHConfigDir, in.KnownHosts = dir, "/Users/me/.config/legato/known_hosts"
	in.SSHConfig = []SSHConfigCopy{{Original: "/private/etc/ssh/ssh_config", Copy: dir + "/ssh_config"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Setenv["GIT_SSH_COMMAND"]; got != "ssh -F "+dir+"/ssh_config" {
		t.Fatalf("GIT_SSH_COMMAND = %q", got)
	}
	linked := false
	for _, l := range plan.Links {
		linked = linked || (l.Path == dir+"/known_hosts" && l.Target == in.KnownHosts)
	}
	if !linked {
		t.Fatalf("known_hosts is not where the copy names it: %+v", plan.Links)
	}
}

// The az wrapper cannot cover the real az: it is linked into the
// execution's own bin folder, which goes first on PATH.
func TestOnAMacTheAzWrapperIsLinkedIntoTheExecutionsBin(t *testing.T) {
	in := macInput()
	fs := macFS()
	run := "/private/tmp/legato-501/sandbox/ex-1"
	fs[run+"/az"] = "file"
	fs["/opt/homebrew/bin/az"] = "file"
	in.FS = fs
	in.Az = SandboxWrapper{Wrapper: run + "/az", Real: "/opt/homebrew/bin/az", RealMount: run + "/az-real", Overlays: []string{"/opt/homebrew/bin/az"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, l := range plan.Links {
		linked = linked || (l.Path == run+"/bin/az" && l.Target == run+"/az")
	}
	if !linked || plan.Setenv["LEGATO_AZ_REAL"] != "/opt/homebrew/bin/az" {
		t.Fatalf("links %+v, LEGATO_AZ_REAL %q", plan.Links, plan.Setenv["LEGATO_AZ_REAL"])
	}
}
