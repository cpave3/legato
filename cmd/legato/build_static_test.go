package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Legato builds without C (cgo), so it cross-compiles with GOOS and GOARCH
// alone and is one static binary that runs on any Linux. `task build` sets
// CGO_ENABLED=0. This fails when a dependency starts to need C.
func TestLegatoBuildsWithoutC(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "legato"), ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legato does not build without C: %v\n%s", err, out)
	}
}
