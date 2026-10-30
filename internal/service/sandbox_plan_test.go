package service

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// fakeFS answers the plan's questions about the host: which paths exist,
// which are folders, and which are symbolic links.
type fakeFS map[string]string // path -> "dir", "file", or "link:<target>"

func (f fakeFS) Probe(path string) PathInfo {
	kind, ok := f[path]
	switch {
	case !ok:
		return PathInfo{}
	case strings.HasPrefix(kind, "link:"):
		return PathInfo{Exists: true, Link: strings.TrimPrefix(kind, "link:")}
	default:
		return PathInfo{Exists: true, Dir: kind == "dir"}
	}
}

func hostFS() fakeFS {
	return fakeFS{
		"/usr": "dir", "/etc": "dir", "/bin": "link:usr/bin", "/lib64": "link:usr/lib64", "/home": "link:var/home",
		"/run/systemd/resolve": "dir",
		"/var/home/me":         "dir", "/var/home/me/.claude": "dir", "/var/home/me/.claude.json": "file",
		"/var/home/me/.codex": "dir", "/var/home/me/.ssh": "dir", "/var/home/me/.config/gh": "dir",
		"/var/home/me/.gitconfig": "file", "/var/home/me/.local/bin": "dir", "/var/home/me/go/pkg/mod": "dir",
		"/var/home/me/Projects/app": "dir", "/var/home/me/Projects": "dir",
		"/var/home/me/.config/legato": "dir", "/var/home/me/.local/share/legato": "dir",
		"/var/home/me/.local/share/legato/claude-mod": "dir",
	}
}

func baseInput() SandboxPlanInput {
	return SandboxPlanInput{
		Home: "/var/home/me", WorkDir: "/var/home/me/Projects/app", ExecutionID: "ex-1", TaskID: "t1", AgentKind: "claude-code",
		RuntimeDir: "/run/user/1000/legato", DataDir: "/var/home/me/.local/share/legato", ConfigPath: "/var/home/me/.config/legato/config.yaml",
		BinaryPath: "/var/home/me/.local/bin/legato", BrokerSocket: "/run/user/1000/legato/broker/broker.sock",
		DefaultMounts: true, FS: hostFS(),
	}
}

func mounted(plan sandbox.Plan) map[string]sandbox.Mount {
	out := map[string]sandbox.Mount{}
	for _, m := range plan.Mounts {
		dest := m.Dest
		if dest == "" {
			dest = m.Path
		}
		out[dest] = m
	}
	return out
}

func TestTheSandboxSeesItsWorkAndToolsAndNoCredentials(t *testing.T) {
	plan, err := BuildSandboxPlan(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	want := map[string]sandbox.Kind{
		"/usr": sandbox.ReadOnly, "/etc": sandbox.ReadOnly, "/bin": sandbox.Symlink, "/home": sandbox.Symlink,
		"/var/home/me/Projects/app": sandbox.ReadWrite,
		"/var/home/me/.claude":      sandbox.ReadWrite, "/var/home/me/.claude.json": sandbox.ReadWrite,
		"/var/home/me/.local/bin": sandbox.ReadOnly, "/var/home/me/go/pkg/mod": sandbox.ReadWrite,
		"/run/user/1000/legato/broker":                sandbox.ReadOnly,
		"/var/home/me/.local/share/legato/claude-mod": sandbox.ReadOnly,
		"/tmp": sandbox.ReadWrite,
	}
	for path, kind := range want {
		if got, ok := m[path]; !ok || got.Kind != kind {
			t.Errorf("%s: %+v, want kind %v", path, got, kind)
		}
	}
	if m["/bin"].Target != "usr/bin" || m["/tmp"].Path != "/run/user/1000/legato/sandbox/ex-1/tmp" {
		t.Errorf("bin = %+v, tmp = %+v", m["/bin"], m["/tmp"])
	}
	for _, never := range []string{"/var/home/me/.ssh", "/var/home/me/.config/gh", "/var/home/me/.gitconfig", "/var/home/me/.config/legato",
		"/var/home/me/.local/share/legato", "/var/home/me/.codex", "/run/user/1000/legato"} {
		if _, ok := m[never]; ok {
			t.Errorf("%s is mounted", never)
		}
	}
	if plan.Home != "/var/home/me" || plan.Chdir != "/var/home/me/Projects/app" || plan.Setenv["LEGATO_SANDBOXED"] != "1" || plan.Setenv["TMPDIR"] != "/tmp" {
		t.Errorf("home %q chdir %q env %v", plan.Home, plan.Chdir, plan.Setenv)
	}
	unset := strings.Join(plan.Unsetenv, " ")
	for _, v := range []string{"OP_SERVICE_ACCOUNT_TOKEN", "GITHUB_TOKEN", "DBUS_SESSION_BUS_ADDRESS", "TMUX", "DOCKER_HOST"} {
		if !strings.Contains(unset, v) {
			t.Errorf("%s is not unset", v)
		}
	}
}

func TestEachAgentToolSeesItsOwnState(t *testing.T) {
	in := baseInput()
	in.AgentKind = "codex"
	plan, _ := BuildSandboxPlan(in)
	m := mounted(plan)
	if m["/var/home/me/.codex"].Kind != sandbox.ReadWrite {
		t.Error("codex does not see ~/.codex")
	}
	if _, ok := m["/var/home/me/.claude"]; ok {
		t.Error("codex sees ~/.claude")
	}
}

func TestAMountThatHoldsACredentialHidesIt(t *testing.T) {
	in := baseInput()
	in.Extra = []SandboxPath{{Path: "/var/home/me", Access: "read"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if m["/var/home/me/.ssh"].Kind != sandbox.HideDir || m["/var/home/me/.gitconfig"].Kind != sandbox.Hide || m["/var/home/me/.config/legato"].Kind != sandbox.HideDir {
		t.Fatalf("ssh %+v gitconfig %+v legato %+v", m["/var/home/me/.ssh"], m["/var/home/me/.gitconfig"], m["/var/home/me/.config/legato"])
	}
	// A hide comes after the mount it covers.
	var home, ssh int
	for i, mount := range plan.Mounts {
		if mount.Path == "/var/home/me" && mount.Kind == sandbox.ReadOnly {
			home = i
		}
		if mount.Path == "/var/home/me/.ssh" {
			ssh = i
		}
	}
	if ssh < home {
		t.Error("~/.ssh is hidden before home is mounted")
	}
}

func TestAMountInsideACredentialIsRefused(t *testing.T) {
	for _, path := range []string{"/var/home/me/.ssh", "/var/home/me/.config/gh/hosts.yml", "/var/home/me/.local/share/legato"} {
		in := baseInput()
		in.Extra = []SandboxPath{{Path: path, Access: "read"}}
		if _, err := BuildSandboxPlan(in); err == nil {
			t.Errorf("%s was mounted", path)
		}
	}
	in := baseInput()
	in.WorkDir = "/var/home/me/.ssh"
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Error("an agent was given ~/.ssh as its working directory")
	}
}

func TestToolchainsCanBeTurnedOff(t *testing.T) {
	in := baseInput()
	in.DefaultMounts = false
	plan, _ := BuildSandboxPlan(in)
	if _, ok := mounted(plan)["/var/home/me/.local/bin"]; ok {
		t.Error("toolchains are mounted with default_mounts off")
	}
}

func TestTheSessionsOwnSocketsAndGitIdentityReachIn(t *testing.T) {
	in := baseInput()
	in.SSHSocket = "/run/user/1000/legato/ssh/ex-1.sock"
	in.GitCommonDir = "/var/home/me/Projects/app-main/.git"
	in.GitIdentity = [2]string{"Ada", "ada@example.com"}
	fs := hostFS()
	fs["/var/home/me/Projects/app-main/.git"] = "dir"
	fs["/run/user/1000/legato/ssh/ex-1.sock"] = "file"
	in.FS = fs
	plan, _ := BuildSandboxPlan(in)
	m := mounted(plan)
	if m["/run/user/1000/legato/ssh/ex-1.sock"].Kind != sandbox.ReadOnly || m["/var/home/me/Projects/app-main/.git"].Kind != sandbox.ReadWrite {
		t.Fatalf("ssh %+v git %+v", m["/run/user/1000/legato/ssh/ex-1.sock"], m["/var/home/me/Projects/app-main/.git"])
	}
	if plan.Setenv["GIT_AUTHOR_NAME"] != "Ada" || plan.Setenv["GIT_COMMITTER_EMAIL"] != "ada@example.com" {
		t.Fatalf("env = %v", plan.Setenv)
	}
}

// linkedFS is a host whose /home is a link to var/home, as on Fedora
// Silverblue: every path is checked and mounted where it really is.
type linkedFS struct{ fakeFS }

func (l linkedFS) Real(path string) string {
	if path == "/home" || strings.HasPrefix(path, "/home/") {
		return "/var" + path
	}
	return path
}

func TestPathsThroughALinkAreCheckedAndMountedWhereTheyReallyAre(t *testing.T) {
	in := baseInput()
	in.FS = linkedFS{hostFS()}
	in.Home = "/home/me"
	in.WorkDir = "/home/me/Projects/app"
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if plan.Home != "/var/home/me" || m["/var/home/me/Projects/app"].Kind != sandbox.ReadWrite || m["/home"].Kind != sandbox.Symlink {
		t.Fatalf("home %q, mounts %+v", plan.Home, plan.Mounts)
	}
	in.Extra = []SandboxPath{{Path: "/home/me/.ssh"}}
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Fatal("~/.ssh through the link was mounted")
	}
}

func TestAConfigFileBesideTheWorkIsHiddenNotItsFolder(t *testing.T) {
	in := baseInput()
	fs := hostFS()
	fs["/var/home/me/legato.yaml"] = "file"
	in.FS = fs
	in.ConfigPath = "/var/home/me/legato.yaml"
	in.Extra = []SandboxPath{{Path: "/var/home/me", Access: "read"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatalf("a config file in home refused the whole home: %v", err)
	}
	if mounted(plan)["/var/home/me/legato.yaml"].Kind != sandbox.Hide {
		t.Fatal("the config file is not hidden")
	}
}

func TestAnAgentSeesOnlyItsOwnToolsInstallation(t *testing.T) {
	in := baseInput()
	fs := hostFS()
	fs["/var/home/me/Projects/chimera"] = "dir"
	fs["/var/home/me/.local/share/claude/versions"] = "dir"
	in.FS = fs
	in.ToolInstalls = map[string][]string{
		"chimera":     {"/var/home/me/Projects/chimera"},
		"claude-code": {"/var/home/me/.local/share/claude/versions"},
	}
	for kind, want := range map[string]string{"claude-code": "/var/home/me/.local/share/claude/versions", "chimera": "/var/home/me/Projects/chimera"} {
		in.AgentKind = kind
		plan, err := BuildSandboxPlan(in)
		if err != nil {
			t.Fatal(err)
		}
		m := mounted(plan)
		if got, ok := m[want]; !ok || got.Kind != sandbox.ReadOnly {
			t.Errorf("%s: its installation %s = %+v", kind, want, got)
		}
		for other, paths := range in.ToolInstalls {
			if other == kind {
				continue
			}
			if _, ok := m[paths[0]]; ok {
				t.Errorf("%s sees %s's installation %s", kind, other, paths[0])
			}
		}
	}
}

func TestNuGetsPackageCacheIsMountedButNotItsConfiguration(t *testing.T) {
	in := baseInput()
	fs := hostFS()
	fs["/var/home/me/.nuget/packages"] = "dir"
	fs["/var/home/me/.nuget/NuGet"] = "dir"
	in.FS = fs
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if got, ok := m["/var/home/me/.nuget/packages"]; !ok || got.Kind != sandbox.ReadWrite {
		t.Fatalf("the NuGet cache = %+v, %v", got, ok)
	}
	if _, ok := m["/var/home/me/.nuget/NuGet"]; ok {
		t.Fatal("NuGet's configuration, which can hold feed passwords, is mounted")
	}
}
