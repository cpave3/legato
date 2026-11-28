package claudebridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostShellReaderCannotEscapeExecutionTmp(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tasks", "safe.output"), []byte("safe-output-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(t.TempDir(), "tasks")
	if err := os.MkdirAll(secretDir, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "host.output")
	if err := os.WriteFile(secret, []byte("host-key-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "tasks", "evil.output")); err != nil {
		t.Fatal(err)
	}
	h := shellHarness(t, root)
	h.mod(hello(nil))
	events := h.subscribe("")
	startBackgroundShell(h, "safe-call", "safe", "/tmp/tasks/safe.output")
	output := nextOfType(t, events, "background_shell_output")
	if output["chunk"] != "safe-output-canary" {
		t.Fatalf("mapped execution output=%v", output)
	}
	startBackgroundShell(h, "evil-call", "evil", "/tmp/tasks/evil.output")
	startBackgroundShell(h, "host-call", "host", secret)
	// Give the real polling path time to read a symlink; a snapshot must never
	// expose those bytes even though telemetry names it as a shell's output.
	time.Sleep(50 * time.Millisecond)
	shells := h.shells().BackgroundShells
	if len(shells) != 3 {
		t.Fatalf("shell records=%v", shells)
	}
	for _, shell := range shells {
		if shell.Output != nil && strings.Contains(shell.Output.Text, "host-key-canary") {
			t.Fatal("agent telemetry exposed a host file through shell output")
		}
	}
}
