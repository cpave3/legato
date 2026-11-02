package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/gitrepo"
)

// git runs legato as its askpass helper for the repository cache: it must
// answer with the token from the pipe it inherits, before anything else.
func TestLegatoAnswersAsGitAskPass(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "legato")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.WriteString("ghs_fromthepipe\n"); err != nil {
		t.Fatal(err)
	}
	write.Close()
	cmd := exec.Command(bin, "Password for 'https://x-access-token@github.com': ")
	cmd.Env = []string{gitrepo.AskPassEnv + "=3", "HOME=" + t.TempDir()}
	cmd.ExtraFiles = []*os.File{read}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("askpass: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ghs_fromthepipe" {
		t.Fatalf("askpass answered %q", out)
	}
}
