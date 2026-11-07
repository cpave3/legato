package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// The release builds a binary for each platform `task release:dist` lists.
// This builds them all, so code that compiles only on Linux fails here and
// not in the release workflow.
func TestLegatoBuildsForEveryReleasePlatform(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary for each platform")
	}
	for _, platform := range releasePlatforms(t) {
		goos, goarch, _ := strings.Cut(platform, "/")
		t.Run(goos+"-"+goarch, func(t *testing.T) {
			cmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "legato"), ".")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("legato does not build for %s: %v\n%s", platform, err, out)
			}
		})
	}
}

// releasePlatforms reads the platforms from the release:dist task.
func releasePlatforms(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, task, ok := strings.Cut(string(data), "\n  release:dist:")
	if !ok {
		t.Fatal("Taskfile.yml has no release:dist task")
	}
	m := regexp.MustCompile(`- for: \[([^\]]+)\]`).FindStringSubmatch(task)
	if m == nil {
		t.Fatal("release:dist lists no platforms")
	}
	var platforms []string
	for _, p := range strings.Split(m[1], ",") {
		p = strings.TrimSpace(p)
		if !strings.Contains(p, "/") {
			t.Fatalf("release:dist platform %q is not GOOS/GOARCH", p)
		}
		platforms = append(platforms, p)
	}
	return platforms
}
