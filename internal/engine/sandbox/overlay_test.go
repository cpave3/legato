package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnOverlayMountIsAThrowawayLayerOverTheHostPath(t *testing.T) {
	got := argsOf(t, Plan{Mounts: []Mount{{Kind: Overlay, Path: "/work/app"}}}, "true")
	if !strings.Contains(got, "--overlay-src /work/app --tmp-overlay /work/app") {
		t.Fatalf("args = %s", got)
	}
}

func TestOverlaySupportIsReadFromBwrapsHelp(t *testing.T) {
	for _, tc := range []struct {
		help string
		err  error
		want bool
	}{
		{"usage: bwrap [OPTIONS...]\n    --tmp-overlay DEST    Mount overlayfs on DEST, with a tmpfs upper\n", nil, true},
		{"usage: bwrap [OPTIONS...]\n    --ro-bind SRC DEST\n", nil, false},
		{"", errors.New("no bwrap"), false},
	} {
		b := Bwrap{Path: "/usr/bin/bwrap", Help: func(string) (string, error) { return tc.help, tc.err }}
		if got := b.SupportsOverlay(); got != tc.want {
			t.Errorf("help %q: SupportsOverlay = %v, want %v", tc.help, got, tc.want)
		}
	}
}

// With a real bwrap: what is written into an overlaid checkout is seen
// inside and gone afterwards; without overlay support, the plain read-only
// mount refuses the write. Either way the host checkout is untouched.
func TestWritesIntoAThrowawayCheckoutNeverReachTheHost(t *testing.T) {
	bin, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap is not installed")
	}
	b := Bwrap{Path: bin}
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := ReadOnly
	if b.SupportsOverlay() {
		kind = Overlay
	}
	plan := Plan{Mounts: []Mount{
		{Kind: ReadOnly, Path: "/usr"}, {Kind: Symlink, Path: "/bin", Target: "usr/bin"},
		{Kind: Symlink, Path: "/lib", Target: "usr/lib"}, {Kind: Symlink, Path: "/lib64", Target: "usr/lib64"},
		{Kind: kind, Path: checkout},
	}, Chdir: checkout}
	args, err := b.Wrap(plan, []string{"sh", "-c", "echo built > out.txt && echo edit >> main.go && cat out.txt"})
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := exec.Command(args[0], args[1:]...).CombinedOutput()
	if strings.Contains(string(out), "No permissions to create new namespace") {
		t.Skipf("this machine does not let bwrap make namespaces: %s", out)
	}
	if kind == Overlay {
		if runErr != nil || strings.TrimSpace(string(out)) != "built" {
			t.Fatalf("a write inside the overlay failed: %v: %s", runErr, out)
		}
	} else if runErr == nil || !strings.Contains(string(out), "Read-only file system") {
		t.Fatalf("a write into the read-only checkout: %v: %s; want it refused as read-only", runErr, out)
	}
	if _, err := os.Stat(filepath.Join(checkout, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("out.txt reached the host checkout: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(checkout, "main.go")); string(data) != "package main\n" {
		t.Fatalf("main.go on the host = %q", data)
	}
}

// An overlay with its layer on disk keeps its writes out of memory.
func TestAnOverlayWithALayerOnDiskUsesIt(t *testing.T) {
	got := argsOf(t, Plan{Mounts: []Mount{{Kind: Overlay, Path: "/work/app", Upper: "/scratch/0/upper", Work: "/scratch/0/work"}}}, "true")
	if !strings.Contains(got, "--overlay-src /work/app --overlay /scratch/0/upper /scratch/0/work /work/app") || strings.Contains(got, "--tmp-overlay") {
		t.Fatalf("args = %s", got)
	}
}
