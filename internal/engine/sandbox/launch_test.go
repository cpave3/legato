//go:build unix

package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The launcher, `legato sandbox exec <spec> -- <command>`, becomes
// sandbox-exec with the spec's profile and parameters, in the spec's
// folder, with its environment: here a script stands in for sandbox-exec
// and says what it was given.
func TestTheLauncherBecomesSandboxExecWithTheSpec(t *testing.T) {
	if spec := os.Getenv("LEGATO_TEST_LAUNCH_SPEC"); spec != "" {
		err := ExecLaunchSpec(spec, []string{"agent", "--flag"})
		t.Fatalf("ExecLaunchSpec returned: %v", err)
	}
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "sandbox-exec")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"args: $*\"\necho \"pwd: $(pwd -P)\"\necho \"home: $HOME token: ${GITHUB_TOKEN:-none}\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := LaunchSpec{Exec: fake, Profile: filepath.Join(dir, "seatbelt.sb"), Params: []string{"P0=/a path"},
		Setenv: map[string]string{"HOME": "/scratch/home"}, Unsetenv: []string{"GITHUB_TOKEN"}, Chdir: work}
	data, _ := json.Marshal(spec)
	file := filepath.Join(dir, "seatbelt.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTheLauncherBecomesSandboxExecWithTheSpec$")
	cmd.Env = append(os.Environ(), "LEGATO_TEST_LAUNCH_SPEC="+file, "GITHUB_TOKEN=ghp_secret", "HOME=/Users/me")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	realWork, _ := filepath.EvalSymlinks(work)
	for _, want := range []string{"args: -f " + spec.Profile + " -DP0=/a path -DTTY=", " -- agent --flag", "pwd: " + realWork, "home: /scratch/home token: none"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTheLauncherRefusesASpecItCannotRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "seatbelt.json")
	if err := ExecLaunchSpec(file, []string{"agent"}); err == nil {
		t.Fatal("ran without a spec")
	}
	if err := os.WriteFile(file, []byte(`{"exec": ""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExecLaunchSpec(file, []string{"agent"}); err == nil {
		t.Fatal("ran a spec with no sandbox-exec")
	}
}
