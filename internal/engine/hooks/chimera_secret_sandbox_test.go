package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestChimeraConsumesSecretBeforeEnteringSandbox(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretFile, []byte("private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	fakeSandbox := filepath.Join(dir, "sandbox")
	script := "#!/bin/sh\nif test -e \"$LEGATO_CHIMERA_SECRET_FILE\"; then echo 'bootstrap still readable'; exit 9; fi\nprintf 'sandbox:%s:' \"$LEGATO_TEST_PREFIX\"\nexec \"$@\"\n"
	if err := os.WriteFile(fakeSandbox, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chimera"), []byte("#!/bin/sh\nprintf '%s:%s' \"$CHIMERA_SERVER_SECRET\" \"$*\""), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LEGATO_CHIMERA_SECRET_FILE", secretFile)
	adapter := hooks.NewChimeraAdapter("legato")
	cmd := adapter.LaunchCommandWithSandbox(map[string]string{"LEGATO_CHIMERA_SECRET_FILE": secretFile, "LEGATO_SANDBOXED": "1"}, "", "", "", "LEGATO_TEST_PREFIX=1 "+fakeSandbox)
	if !strings.HasPrefix(cmd, "sh -c ") || strings.Contains(cmd, "private-value") {
		t.Fatalf("secret preamble is not outside sandbox: %s", cmd)
	}
	out, err := exec.Command("sh", "-c", cmd+" --late-flag").CombinedOutput()
	if err != nil || string(out) != "sandbox:1:private-value:--late-flag" {
		t.Fatalf("output=%q error=%v", out, err)
	}
	if _, err := os.Stat(secretFile); !os.IsNotExist(err) {
		t.Fatalf("bootstrap remains: %v", err)
	}
}
