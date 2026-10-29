package sandbox

import (
	"strings"
	"testing"
)

func argsOf(t *testing.T, plan Plan, command ...string) string {
	t.Helper()
	args, err := Bwrap{Path: "/usr/bin/bwrap"}.Wrap(plan, command)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(args, " ")
}

func TestBwrapArgumentsFollowThePlan(t *testing.T) {
	got := argsOf(t, Plan{
		Home: "/var/home/me",
		Mounts: []Mount{
			{Kind: ReadOnly, Path: "/usr"},
			{Kind: Symlink, Path: "/bin", Target: "usr/bin"},
			{Kind: ReadWrite, Path: "/var/home/me/Projects/app"},
			{Kind: ReadOnly, Path: "/var/home/me/.local/bin", Optional: true},
			{Kind: Hide, Path: "/var/home/me/Projects/app/.env"},
			{Kind: HideDir, Path: "/var/home/me/Projects/app/secrets"},
			{Kind: ReadWrite, Path: "/run/user/1000/legato/sandbox/ex-1/tmp", Dest: "/tmp"},
		},
		Setenv:   map[string]string{"LEGATO_SANDBOXED": "1"},
		Unsetenv: []string{"GH_TOKEN"},
		Chdir:    "/var/home/me/Projects/app",
	}, "claude", "--model", "opus")

	for _, want := range []string{
		"/usr/bin/bwrap --die-with-parent --unshare-pid --unshare-ipc --unshare-uts --unshare-cgroup-try --proc /proc --dev /dev",
		"--tmpfs /var/home/me",
		"--ro-bind /usr /usr",
		"--symlink usr/bin /bin",
		"--bind /var/home/me/Projects/app /var/home/me/Projects/app",
		"--ro-bind-try /var/home/me/.local/bin /var/home/me/.local/bin",
		"--ro-bind /dev/null /var/home/me/Projects/app/.env",
		"--tmpfs /var/home/me/Projects/app/secrets",
		"--bind /run/user/1000/legato/sandbox/ex-1/tmp /tmp",
		"--setenv LEGATO_SANDBOXED 1",
		"--unsetenv GH_TOKEN",
		"--chdir /var/home/me/Projects/app",
		"-- claude --model opus",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// Mounts keep their order: a later one covers an earlier one.
	if strings.Index(got, "--bind /var/home/me/Projects/app ") > strings.Index(got, "--ro-bind /dev/null /var/home/me/Projects/app/.env") {
		t.Error("a hidden file was mounted before the folder it hides in")
	}
	// The network is shared: bridges on loopback stay reachable.
	if strings.Contains(got, "--unshare-net") || strings.Contains(got, "--unshare-all") {
		t.Error("the network was unshared")
	}
}

func TestAPlanIsRefusedWhenItCannotBeRendered(t *testing.T) {
	for _, plan := range []Plan{
		{Mounts: []Mount{{Kind: ReadOnly, Path: "relative/path"}}},
		{Mounts: []Mount{{Kind: Symlink, Path: "/bin"}}},
		{Chdir: "nowhere"},
	} {
		if _, err := (Bwrap{Path: "/usr/bin/bwrap"}).Wrap(plan, []string{"true"}); err == nil {
			t.Errorf("accepted %+v", plan)
		}
	}
	if _, err := (Bwrap{Path: "/usr/bin/bwrap"}).Wrap(Plan{}, nil); err == nil {
		t.Error("accepted no command")
	}
}

func TestTheBackendSaysWhenBwrapIsMissing(t *testing.T) {
	b := Bwrap{LookPath: func(string) (string, error) { return "", errNotFound }}
	if err := b.Available(); err == nil || !strings.Contains(err.Error(), "bwrap") {
		t.Fatalf("missing: %v", err)
	}
}
