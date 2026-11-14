package service

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

var updateBwrapGolden = flag.Bool("update-bwrap", false, "rewrite testdata/bwrap with the bwrap arguments of today's plans")

// bwrapGoldenInputs are plans of several shapes: a worktree with the usual
// mounts, a Debian home, a job clone with the gh wrapper, ssh copies,
// Docker and resources, and a reviewer's throwaway checkout.
func bwrapGoldenInputs() map[string]SandboxPlanInput {
	inputs := map[string]SandboxPlanInput{"worktree": baseInput(), "debian": debianInput()}

	full := baseInput()
	fs := hostFS()
	for path, kind := range map[string]string{
		"/run/user/1000/legato/bin/gh": "file", "/usr/bin/gh": "file", "/home/linuxbrew/.linuxbrew": "dir",
		"/etc/ssh/ssh_config": "file", "/run/user/1000/legato/sandbox/ex-1/ssh": "dir",
		"/run/user/1000/legato/sandbox/ex-1/ssh/ssh_config": "file", "/var/home/me/.config/legato/known_hosts": "file",
		"/var/home/me/Notes": "dir", "/var/home/me/Notes/.npmrc": "file", "/var/home/me/Projects/app/.git": "dir",
		"/var/home/me/.local/share/legato/repos": "dir", "/run/user/1000/legato/sandbox/ex-1/docker": "dir",
	} {
		fs[path] = kind
	}
	full.FS = fs
	full.CredentialWrapperDir, full.GHConfigDir = "/run/user/1000/legato/bin", "/run/user/1000/legato/gh-config"
	full.GH = SandboxWrapper{Wrapper: "/run/user/1000/legato/bin/gh", Real: "/usr/bin/gh", RealMount: "/run/user/1000/legato/sandbox/ex-1/gh-real", Overlays: []string{"/usr/bin/gh"}}
	full.SSHConfigDir = "/run/user/1000/legato/sandbox/ex-1/ssh"
	full.SSHConfig = []SSHConfigCopy{{Original: "/etc/ssh/ssh_config", Copy: "/run/user/1000/legato/sandbox/ex-1/ssh/ssh_config"}}
	full.KnownHosts = "/var/home/me/.config/legato/known_hosts"
	full.SSHSocket = "/run/user/1000/legato/ssh/ex-1.sock"
	full.DockerSocket = "/run/user/1000/legato/sandbox/ex-1/docker/docker.sock"
	full.Extra = []SandboxPath{{Path: "/var/home/me/Notes", Access: "write"}}
	full.GitIdentity = [2]string{"Me", "me@example.com"}
	inputs["full"] = full

	reviewer := baseInput()
	reviewer.ReadOnlyCheckout, reviewer.ThrowawayCheckout = true, true
	reviewer.ScratchDir = "/var/home/me/.local/share/legato/sandbox"
	inputs["reviewer"] = reviewer
	return inputs
}

// The bwrap arguments of a plan are what agents on Linux run in: a change to
// how plans are made or rendered must not change them by accident. Run with
// -update-bwrap after a change that means to.
func TestBwrapArgumentsOfKnownPlansDoNotChange(t *testing.T) {
	for name, in := range bwrapGoldenInputs() {
		t.Run(name, func(t *testing.T) {
			plan, err := BuildSandboxPlan(in)
			if err != nil {
				t.Fatal(err)
			}
			args, err := sandbox.Bwrap{Path: "/usr/bin/bwrap"}.Wrap(plan, []string{"agent", "--flag"})
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(args, "\n") + "\n"
			file := filepath.Join("testdata", "bwrap", name+".args")
			if *updateBwrapGolden {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run with -update-bwrap to write it)", err)
			}
			if got != string(want) {
				t.Errorf("bwrap arguments of %s changed:\n%s", name, argsDiff(string(want), got))
			}
		})
	}
}

// argsDiff lists the lines only one side has, in order.
func argsDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	i, j := 0, 0
	for i < len(w) || j < len(g) {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i++
			j++
		case j < len(g) && (i >= len(w) || !slices.Contains(w[i:], g[j])):
			b.WriteString("+ " + g[j] + "\n")
			j++
		default:
			b.WriteString("- " + w[i] + "\n")
			i++
		}
	}
	return b.String()
}
