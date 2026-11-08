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
	if sb.Enabled || sb.Backend != "bwrap" || !sb.DefaultMountsOn() || len(sb.Mounts) != 0 {
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
		{"backend", "agents:\n  sandbox:\n    backend: podman\n", "bwrap"},
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
	for _, dir := range []string{"/srv/legato/repos", "/srv/legato/repos/scratch", "/srv/legato/claude-mod", "/srv/legato/jobs/x"} {
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
