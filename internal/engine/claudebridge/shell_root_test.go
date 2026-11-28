package claudebridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostShellReaderCannotEscapeExecutionTmp(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tasks", "safe.output"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(secret, []byte("host-key-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "tasks", "evil.output")); err != nil {
		t.Fatal(err)
	}
	data, _, _, _, err := readShellOutputInRoot(root, "/tmp/tasks/safe.output", 0)
	if err != nil || string(data) != "safe" {
		t.Fatalf("sandbox output=%q %v", data, err)
	}
	for _, path := range []string{"/tmp/tasks/evil.output", secret, "/tmp/../" + secret} {
		data, _, _, _, err = readShellOutputInRoot(root, path, 0)
		if err == nil || len(data) != 0 {
			t.Fatalf("host file disclosed for %s: %q %v", path, data, err)
		}
	}
}
