package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The release workflow calls this command; fixtures exercise its real git
// comparisons and GitHub command boundary without publishing anything.
func TestAndroidReleaseCommands(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for Android release command tests")
	}
	cmd := exec.Command(python, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_android_release.py")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Android release command tests: %v\n%s", err, out)
	}
}
