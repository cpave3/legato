package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheSandboxIsOffWithToolchainMountsUnlessConfigured(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := cfg.Agents.Sandbox
	if sb.Enabled || sb.Backend != DefaultSandboxBackend(sandboxGOOS) || !sb.DefaultMountsOn() || len(sb.Mounts) != 0 {
		t.Fatalf("sandbox = %+v", sb)
	}
}

func TestTheSandboxBlockIsParsed(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	writeTestConfig(t, `
agents:
  sandbox:
    enabled: true
    default_mounts: false
    mounts:
      - path: ~/Projects
      - path: /srv/data
        access: write
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := cfg.Agents.Sandbox
	if !sb.Enabled || sb.DefaultMountsOn() || len(sb.Mounts) != 2 {
		t.Fatalf("sandbox = %+v", sb)
	}
	if sb.Mounts[0] != (ResourcePathConfig{Path: "/home/me/Projects", Access: "read"}) || sb.Mounts[1] != (ResourcePathConfig{Path: "/srv/data", Access: "write"}) {
		t.Fatalf("mounts = %+v", sb.Mounts)
	}
}

func TestASandboxThatCannotWorkFailsToLoad(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"backend", "agents:\n  sandbox:\n    backend: podman\n", "bwrap or seatbelt"},
		{"relative mount", "agents:\n  sandbox:\n    mounts: [{path: notes}]\n", "absolute"},
		{"access", "agents:\n  sandbox:\n    mounts: [{path: /a, access: admin}]\n", "read or write"},
		{"relative scratch", "agents:\n  sandbox:\n    scratch_dir: scratch\n", "absolute"},
		{"scratch is the home", "agents:\n  sandbox:\n    scratch_dir: \"~\"\n", "home"},
		{"scratch holds the home", "agents:\n  sandbox:\n    scratch_dir: /\n", "home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTheSandboxsScratchIsInTheDataFolderUnlessConfigured(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	writeTestConfig(t, "data_dir: /srv/legato\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveSandboxScratchDir(cfg); got != "/srv/legato/sandbox" {
		t.Fatalf("scratch = %s", got)
	}
	writeTestConfig(t, "agents:\n  sandbox:\n    scratch_dir: ~/scratch\n")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSandboxScratchDir(cfg); got != "/home/me/scratch" {
		t.Fatalf("scratch = %s", got)
	}
}

// The data folder's shared folders, which every sandbox is shown, cannot
// hold the scratch: the sandbox would have to hide them.
func TestAScratchFolderInsideTheDataFoldersSharedFoldersFailsToLoad(t *testing.T) {
	// At, inside, or holding them: the data folder itself, or one above it.
	for _, dir := range []string{"/srv/legato/repos", "/srv/legato/repos/scratch", "/srv/legato/claude-mod", "/srv/legato/jobs/x", "/srv/legato", "/srv"} {
		writeTestConfig(t, "data_dir: /srv/legato\nagents:\n  sandbox:\n    scratch_dir: "+dir+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "scratch_dir") {
			t.Errorf("%s: err = %v", dir, err)
		}
	}
	writeTestConfig(t, "data_dir: /srv/legato\nagents:\n  sandbox:\n    scratch_dir: /srv/legato/scratch\n")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

// The check is made on the real path: a link to the repository cache is the
// repository cache.
func TestAScratchFolderLinkedIntoTheDataFolderFailsToLoad(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "repos", "scratch"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "scratch")
	if err := os.Symlink(filepath.Join(data, "repos", "scratch"), link); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, "data_dir: "+data+"\nagents:\n  sandbox:\n    scratch_dir: "+link+"\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "scratch_dir") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheSandboxsDockerProxyIsOffByDefault(t *testing.T) {
	cfg, err := loadAgentsConfig(t, "agents:\n  sandbox:\n    enabled: true\n")
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Agents.Sandbox.Docker
	if d.Enabled || d.Publish != "loopback" || len(d.CapAdd) != 0 || d.Host != "" {
		t.Fatalf("docker = %+v, want off, publishing on loopback, no capabilities", d)
	}
}

func TestTheSandboxsDockerProxySettings(t *testing.T) {
	cfg, err := loadAgentsConfig(t, `agents:
  sandbox:
    docker:
      enabled: true
      host: unix:///run/user/1000/docker.sock
      cap_add: [sys_ptrace, CAP_NET_ADMIN, SYS_PTRACE]
      publish: any
`)
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Agents.Sandbox.Docker
	if !d.Enabled || d.Host != "unix:///run/user/1000/docker.sock" || d.Publish != "any" || strings.Join(d.CapAdd, ",") != "SYS_PTRACE,NET_ADMIN" {
		t.Fatalf("docker = %+v", d)
	}
	for _, tc := range []struct{ yaml, want string }{
		{"agents:\n  sandbox:\n    docker:\n      publish: everywhere\n", "publish"},
		{"agents:\n  sandbox:\n    docker:\n      cap_add: [ALL]\n", "ALL"},
		{"agents:\n  sandbox:\n    docker:\n      cap_add: [SYS_ADMIN]\n", "SYS_ADMIN"},
		{"agents:\n  sandbox:\n    docker:\n      host: ssh://box\n", "unix://"},
	} {
		if _, err := loadAgentsConfig(t, tc.yaml); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error = %v, want one naming %q", tc.yaml, err, tc.want)
		}
	}
}

// The backend is the host's own unless one is named: bubblewrap on Linux,
// Seatbelt on macOS. One that cannot run on this host fails to load.
func TestTheSandboxBackendIsTheHostsOwn(t *testing.T) {
	for _, tc := range []struct{ goos, yaml, want, refused string }{
		{"linux", "", "bwrap", ""},
		{"darwin", "", "seatbelt", ""},
		{"darwin", "agents:\n  sandbox:\n    backend: seatbelt\n", "seatbelt", ""},
		{"linux", "agents:\n  sandbox:\n    backend: seatbelt\n", "", "macOS only"},
		{"darwin", "agents:\n  sandbox:\n    backend: bwrap\n", "", "Linux only"},
	} {
		t.Run(tc.goos+" "+tc.want+tc.refused, func(t *testing.T) {
			old := sandboxGOOS
			sandboxGOOS = tc.goos
			t.Cleanup(func() { sandboxGOOS = old })
			writeTestConfig(t, tc.yaml)
			cfg, err := Load()
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) {
					t.Fatalf("err = %v, want %q", err, tc.refused)
				}
				return
			}
			if err != nil || cfg.Agents.Sandbox.Backend != tc.want {
				t.Fatalf("backend %q, %v; want %q", cfg.Agents.Sandbox.Backend, err, tc.want)
			}
		})
	}
}
