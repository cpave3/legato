package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// versionLDFlag is how a release build stamps its version; the release task
// in Taskfile.yml must use the same.
const versionLDFlag = "-X github.com/cpave3/legato/internal/engine/version.stamped="

func TestAReleaseBuildReportsItsVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "legato")
	build := exec.Command("go", "build", "-ldflags", versionLDFlag+"v9.8.7", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	// No configuration or data: an updater runs a new binary this way
	// before it trusts it.
	env := []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}

	text := exec.Command(bin, "version")
	text.Env = env
	out, err := text.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "legato v9.8.7" {
		t.Errorf("legato version = %q (%v), want legato v9.8.7", out, err)
	}

	asJSON := exec.Command(bin, "version", "--json")
	asJSON.Env = env
	out, err = asJSON.Output()
	var envelope struct {
		Data struct {
			Version string `json:"version"`
			Release bool   `json:"release"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(out, &envelope) != nil || envelope.Data.Version != "v9.8.7" || !envelope.Data.Release {
		t.Errorf("legato version --json = %s (%v)", out, err)
	}
}

func TestTheReleaseTaskStampsTheVersion(t *testing.T) {
	taskfile, err := os.ReadFile(filepath.Join("..", "..", "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(taskfile), versionLDFlag+"{{.VERSION}}") {
		t.Errorf("Taskfile.yml has no release build that stamps %s{{.VERSION}}", versionLDFlag)
	}
}
