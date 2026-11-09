package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// A host command runs with no way to gain privileges: sudo fails.
func TestAHostCommandCannotGainPrivileges(t *testing.T) {
	if testing.Short() || runtime.GOOS != "linux" {
		t.Skip("builds the binary; no_new_privs is Linux's")
	}
	bin := filepath.Join(t.TempDir(), "legato")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	old := launchExecutable
	launchExecutable = bin
	defer func() { launchExecutable = old }()

	exit, out, err := runHostCommand(context.Background(), service.HostCommand{Argv: []string{"sh", "-c", "grep NoNewPrivs /proc/self/status; echo HOME=$HOME; echo LEGATO=$LEGATO_CONFIG"}}, time.Minute)
	if err != nil || exit != 0 {
		t.Fatalf("exit %d, %v: %s", exit, err, out)
	}
	text := string(out)
	home, _ := os.UserHomeDir()
	if !strings.Contains(text, "NoNewPrivs:\t1") || !strings.Contains(text, "HOME="+home) || !strings.Contains(text, "LEGATO=\n") {
		t.Fatalf("output:\n%s", text)
	}

	exit, _, _ = runHostCommand(context.Background(), service.HostCommand{Argv: []string{"sh", "-c", "sleep 5"}}, 200*time.Millisecond)
	if exit != -1 {
		t.Fatalf("a command past its timeout: exit %d", exit)
	}
}
