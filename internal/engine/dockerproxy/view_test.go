package dockerproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxed is a host laid out like an agent's: a home with .ssh, a
// worktree in it, the toolchains read-only, a scratch /tmp and home.
type sandboxed struct {
	root, home, ssh, work, tools, scratchHome, scratchTmp, feed string
	// tmp is where the sandbox shows its /tmp: under the test's folder, as
	// that folder is itself in the host's /tmp.
	tmp  string
	view View
}

func newSandboxed(t *testing.T) sandboxed {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := sandboxed{root: root, home: filepath.Join(root, "home/me")}
	s.ssh = filepath.Join(s.home, ".ssh")
	s.work = filepath.Join(s.home, "work/repo")
	s.tools = filepath.Join(s.home, ".local/bin")
	s.feed = filepath.Join(s.home, ".local/share/MicrosoftCredentialProvider")
	s.scratchHome = filepath.Join(root, "scratch/ex-1/home")
	s.scratchTmp = filepath.Join(root, "scratch/ex-1/tmp")
	s.tmp = filepath.Join(root, "tmp")
	for _, dir := range []string{s.ssh, s.work, s.tools, s.feed, s.scratchHome, s.scratchTmp, filepath.Join(s.work, "secrets")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s.view = View{Mounts: []Mount{
		{Kind: ReadWrite, Source: s.scratchHome, Dest: s.home},
		{Kind: ReadWrite, Source: s.scratchTmp, Dest: s.tmp},
		{Kind: Symlink, Dest: filepath.Join(root, "link-to-home"), Target: "home"},
		{Kind: ReadOnly, Source: s.tools, Dest: s.tools},
		{Kind: ReadOnly, Source: s.feed, Dest: s.feed},
		{Kind: ReadWrite, Source: s.work, Dest: s.work},
		{Kind: HideDir, Dest: filepath.Join(s.work, "secrets")},
	}}
	return s
}

func TestAContainerMountsWhatTheSandboxSeesWithNoMoreAccess(t *testing.T) {
	s := newSandboxed(t)
	for _, tc := range []struct {
		name, path, host string
		readOnly         bool
	}{
		{"the worktree", s.work, s.work, false},
		{"inside the worktree", filepath.Join(s.work, "src"), filepath.Join(s.work, "src"), false},
		{"a read-only toolchain", s.tools, s.tools, true},
		{"a read-only resource", s.feed, s.feed, true},
		{"the sandbox's /tmp is its scratch folder", s.tmp, s.scratchTmp, false},
		{"the sandbox's home is its scratch folder", s.home, s.scratchHome, false},
		{"~/.ssh made in the sandbox is the scratch one", s.ssh, filepath.Join(s.scratchHome, ".ssh"), false},
		{"through a link the sandbox makes", filepath.Join(s.root, "link-to-home/me/work/repo"), s.work, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(tc.host, 0o700); err != nil {
				t.Fatal(err)
			}
			host, readOnly, err := s.view.Resolve(tc.path)
			if err != nil || host != tc.host || readOnly != tc.readOnly {
				t.Fatalf("Resolve(%s) = %s, read-only %v, %v; want %s, read-only %v", tc.path, host, readOnly, err, tc.host, tc.readOnly)
			}
		})
	}
}

func TestAContainerCannotMountWhatTheSandboxHides(t *testing.T) {
	s := newSandboxed(t)
	link := filepath.Join(s.work, "keys")
	if err := os.Symlink(s.ssh, link); err != nil {
		t.Fatal(err)
	}
	// A link in /tmp is a link in the scratch folder on the host.
	if err := os.Symlink(s.ssh, filepath.Join(s.scratchTmp, "keys")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, want string }{
		// The sandbox's ~ is its scratch home, where there is no .ssh.
		{"~/.ssh", s.ssh, "does not exist"},
		{"the real ~/.ssh by its host path", filepath.Join(s.root, "home/me/.ssh"), "does not exist"},
		{"the root", "/", "not in this agent's sandbox"},
		{"the Docker socket", "/var/run/docker.sock", "not in this agent's sandbox"},
		{"a link in the worktree to ~/.ssh", link, "not in this agent's sandbox"},
		{"a link in /tmp to ~/.ssh", filepath.Join(s.tmp, "keys"), "not in this agent's sandbox"},
		{"a hidden folder", filepath.Join(s.work, "secrets"), "hidden"},
		{"a relative path", "work", "absolute"},
		{"a path that does not exist", filepath.Join(s.work, "missing"), "does not exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, _, err := s.view.Resolve(tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve(%s) = %s, %v; want an error saying %q", tc.path, host, err, tc.want)
			}
		})
	}
}
