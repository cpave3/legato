package service

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// These pin whole plans, in order, for the account-scoped Claude state, the
// protected Claude accounts and NuGet's variable, beside a plan of each agent
// tool without them, so that moving that code cannot change what a sandbox
// sees, or what it refuses and why.

var kindNames = map[sandbox.Kind]string{
	sandbox.ReadOnly: "ro", sandbox.ReadWrite: "rw", sandbox.Symlink: "link", sandbox.Hide: "hide", sandbox.HideDir: "hidedir", sandbox.Overlay: "overlay",
}

// planLines writes each mount as "kind path[ -> dest][ @target]", in order.
func planLines(plan sandbox.Plan) []string {
	out := make([]string, 0, len(plan.Mounts))
	for _, m := range plan.Mounts {
		line := kindNames[m.Kind] + " " + m.Path
		if m.Dest != "" {
			line += " -> " + m.Dest
		}
		if m.Target != "" {
			line += " @" + m.Target
		}
		out = append(out, line)
	}
	return out
}

func envLines(plan sandbox.Plan) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(plan.Setenv)) {
		out = append(out, fmt.Sprintf("%s=%s", k, plan.Setenv[k]))
	}
	return out
}

func goLiteral(lines []string) string {
	quoted := make([]string, len(lines))
	for i, l := range lines {
		quoted[i] = fmt.Sprintf("%q", l)
	}
	return "{\n\t" + strings.Join(quoted, ",\n\t") + ",\n}"
}

// claudeHostFS is hostFS with every Claude state path, a second account,
// Chimera's state and NuGet's cache.
func claudeHostFS() fakeFS {
	fs := hostFS()
	for _, p := range []string{"/var/home/me/.cache/claude", "/var/home/me/.local/share/claude", "/var/home/me/.local/state/claude",
		"/accounts", "/accounts/work", "/accounts/other", "/var/home/me/.claude-work", "/var/home/me/.nuget/packages",
		"/var/home/me/.chimera", "/var/home/me/.chimera/mods", "/var/home/me/.chimera/hooks",
		"/var/home/me/.local/share/legato/repos", "/var/home/me/.local/share/legato/repos/acct"} {
		fs[p] = "dir"
	}
	return fs
}

var claudeBaseEnv = []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1", "LEGATO_SANDBOXED=1", "NUGET_PACKAGES=/var/home/me/.nuget/packages", "TMPDIR=/tmp"}

func claudePlanInput(kind string) SandboxPlanInput {
	in := baseInput()
	in.FS = claudeHostFS()
	in.AgentKind = kind
	return in
}

func TestSandboxPlansAreExactlyAsBeforeAroundClaudeAccounts(t *testing.T) {
	type scenario struct {
		in     func() SandboxPlanInput
		mounts []string
		env    []string // nil: claudeBaseEnv
	}
	cases := map[string]scenario{
		"claude-code without an account": {
			in: func() SandboxPlanInput { return claudePlanInput("claude-code") },
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.claude",
				"rw /var/home/me/.claude.json",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/.cache/claude",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"rw /var/home/me/.local/share/claude",
				"rw /var/home/me/.local/state/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		"codex": {
			in: func() SandboxPlanInput { return claudePlanInput("codex") },
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.codex",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		"chimera": {
			in: func() SandboxPlanInput { return claudePlanInput("chimera") },
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.chimera",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"ro /var/home/me/.chimera/mods",
				"ro /var/home/me/.chimera/hooks",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// The default account lives in the home: its config folder, and the
		// home's other Claude files, writable; the installation read-only.
		"claude-code on the default account": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("claude-code")
				in.ClaudeConfigDir = "/var/home/me/.claude"
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
				in.ToolState = []string{"/var/home/me/.claude"}
				return in
			},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.claude",
				"rw /var/home/me/.claude.json",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/.cache/claude",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"rw /var/home/me/.local/state/claude",
				"ro /var/home/me/.local/share/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// A named account: its folder alone, none of the home's files.
		"claude-code on a named account": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("claude-code")
				in.ClaudeConfigDir = "/accounts/work"
				in.ClaudeConfigExplicit = true
				in.ProtectedClaudeAccounts = []string{"/accounts/other", "/var/home/me/.claude", "/var/home/me/.claude.json"}
				in.ToolState = []string{"/accounts/work"}
				in.Extra = []SandboxPath{{Path: "/accounts", Access: "read"}}
				return in
			},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /accounts",
				"rw /accounts/work",
				"hidedir /accounts/other",
				"ro /run/systemd/resolve",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// Through /home, a link: the account and the protected ones are
		// resolved before anything is checked or mounted.
		"claude-code on an account named through a link": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("claude-code")
				in.FS = linkedFS{claudeHostFS()}
				in.Home, in.WorkDir = "/home/me", "/home/me/Projects/app"
				in.ClaudeConfigDir = "/home/me/.claude-work"
				in.ProtectedClaudeAccounts = []string{"/home/me/.claude"}
				in.ToolState = []string{"/home/me/.claude-work"}
				return in
			},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.claude-work",
				"rw /var/home/me/.claude.json",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/.cache/claude",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"rw /var/home/me/.local/state/claude",
				"ro /var/home/me/.local/share/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// Another tool ignores the Claude account, but not the protected ones.
		"codex given a Claude account": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("codex")
				in.ClaudeConfigDir = "/accounts/work"
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
				in.Extra = []SandboxPath{{Path: "/accounts", Access: "read"}}
				return in
			},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /accounts",
				"hidedir /accounts/other",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.codex",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// A protected account is covered even inside a mount that may sit in
		// a forbidden folder (the repository cache).
		"a protected account inside the repository cache": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("claude-code")
				in.ClaudeConfigDir = "/accounts/work"
				in.ClaudeConfigExplicit = true
				in.ProtectedClaudeAccounts = []string{"/var/home/me/.local/share/legato/repos/acct"}
				return in
			},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"rw /accounts/work",
				"ro /run/systemd/resolve",
				"ro /var/home/me/.local/bin",
				"rw /var/home/me/.nuget/packages",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
				"hidedir /var/home/me/.local/share/legato/repos/acct",
			},
		},
		// A linked NuGet cache is made again as the link, and is not pinned.
		"a linked NuGet cache": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("codex")
				in.FS.(fakeFS)["/var/home/me/.nuget/packages"] = "link:/srv/nuget"
				return in
			},
			env: []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1", "LEGATO_SANDBOXED=1", "TMPDIR=/tmp"},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.codex",
				"ro /var/home/me/.local/bin",
				"link /var/home/me/.nuget/packages @/srv/nuget",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/go/pkg/mod",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
		// Without the default mounts there is no cache, and no pin.
		"no default mounts": {
			in: func() SandboxPlanInput {
				in := claudePlanInput("claude-code")
				in.DefaultMounts = false
				in.ClaudeConfigDir = "/var/home/me/.claude"
				return in
			},
			env: []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1", "LEGATO_SANDBOXED=1", "TMPDIR=/tmp"},
			mounts: []string{
				"ro /usr",
				"link /bin @usr/bin",
				"link /lib64 @usr/lib64",
				"ro /etc",
				"link /home @var/home",
				"rw /var/home/me/.local/share/legato/sandbox/ex-1/tmp -> /tmp",
				"ro /run/systemd/resolve",
				"rw /var/home/me/.claude",
				"rw /var/home/me/.claude.json",
				"rw /var/home/me/.cache/claude",
				"rw /var/home/me/Projects/app",
				"ro /run/user/1000/legato/broker",
				"rw /var/home/me/.local/state/claude",
				"ro /var/home/me/.local/share/claude",
				"ro /var/home/me/.local/share/legato/claude-mod",
				"ro /var/home/me/.local/share/legato/repos",
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			plan, err := BuildSandboxPlan(c.in())
			if err != nil {
				t.Fatal(err)
			}
			env := c.env
			if env == nil {
				env = claudeBaseEnv
			}
			if got := planLines(plan); !slices.Equal(got, c.mounts) {
				t.Errorf("mounts = %s", goLiteral(got))
			}
			if got := envLines(plan); !slices.Equal(got, env) {
				t.Errorf("env = %s", goLiteral(got))
			}
		})
	}
}

// What the plan refuses around Claude accounts, and the words it uses: a
// protected account is refused before the scratch folder and the exemptions
// are looked at, and the selected account goes through the same check as
// any other mount.
func TestSandboxPlanRefusalsAroundClaudeAccountsAreExactlyAsBefore(t *testing.T) {
	cases := map[string]struct {
		in   func(*SandboxPlanInput)
		want string // "" when the plan is made
	}{
		"the account inside a protected one": {
			in: func(in *SandboxPlanInput) {
				in.ClaudeConfigDir = "/accounts/other/sub"
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
			},
			want: "/accounts/other/sub is another Claude account's private state",
		},
		"the account at a protected one": {
			in: func(in *SandboxPlanInput) {
				in.ClaudeConfigDir = "/accounts/other"
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
			},
			want: "/accounts/other is another Claude account's private state",
		},
		"the account in a credential folder": {
			in: func(in *SandboxPlanInput) {
				in.ClaudeConfigDir = "/var/home/me/.ssh/claude"
			},
			want: "/var/home/me/.ssh/claude holds credentials or Legato's own state, which an agent in the sandbox must not see",
		},
		"the account in the scratch folder": {
			in: func(in *SandboxPlanInput) {
				in.ClaudeConfigDir = "/var/home/me/.local/share/legato/sandbox/ex-2/home/.claude"
			},
			want: "/var/home/me/.local/share/legato/sandbox/ex-2/home/.claude is in the sandboxes' scratch folder, which holds other agents' /tmp and home",
		},
		"a protected account in the scratch folder": {
			in: func(in *SandboxPlanInput) {
				in.ClaudeConfigDir = "/var/home/me/.local/share/legato/sandbox/ex-2/home/.claude"
				in.ProtectedClaudeAccounts = []string{"/var/home/me/.local/share/legato/sandbox/ex-2/home"}
			},
			want: "/var/home/me/.local/share/legato/sandbox/ex-2/home/.claude is another Claude account's private state",
		},
		"an extra path in a protected account inside the repository cache": {
			in: func(in *SandboxPlanInput) {
				in.ProtectedClaudeAccounts = []string{"/var/home/me/.local/share/legato/repos/acct"}
				in.Extra = []SandboxPath{{Path: "/var/home/me/.local/share/legato/repos/acct/x", Access: "read"}}
			},
			want: "/var/home/me/.local/share/legato/repos/acct/x is another Claude account's private state",
		},
		"tool state in a protected account": {
			in: func(in *SandboxPlanInput) {
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
				in.ToolState = []string{"/accounts/other/projects"}
			},
			want: "/accounts/other/projects is another Claude account's private state",
		},
		"the work in a protected account": {
			in: func(in *SandboxPlanInput) {
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
				in.WorkDir = "/accounts/other"
			},
			want: "/accounts/other is another Claude account's private state",
		},
		"a protected account named through a link": {
			in: func(in *SandboxPlanInput) {
				in.FS = linkedFS{claudeHostFS()}
				in.ProtectedClaudeAccounts = []string{"/home/me/.claude"}
				in.Extra = []SandboxPath{{Path: "/var/home/me/.claude/projects", Access: "read"}}
			},
			want: "/var/home/me/.claude/projects is another Claude account's private state",
		},
		"an account named through a link into a protected one": {
			in: func(in *SandboxPlanInput) {
				in.FS = linkedFS{claudeHostFS()}
				in.ClaudeConfigDir = "/home/me/.claude"
				in.ProtectedClaudeAccounts = []string{"/var/home/me/.claude"}
			},
			want: "/var/home/me/.claude is another Claude account's private state",
		},
		// Only Claude Code mounts the account, so only Claude Code checks it.
		"codex given an account inside a protected one": {
			in: func(in *SandboxPlanInput) {
				in.AgentKind = "codex"
				in.ClaudeConfigDir = "/accounts/other/sub"
				in.ProtectedClaudeAccounts = []string{"/accounts/other"}
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := claudePlanInput("claude-code")
			c.in(&in)
			_, err := BuildSandboxPlan(in)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != c.want {
				t.Fatalf("error = %q, want %q", got, c.want)
			}
		})
	}
}
