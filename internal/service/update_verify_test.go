package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeBinary(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legato")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestABinaryIsTrustedWhenItRunsAsTheRelease(t *testing.T) {
	bin := fakeBinary(t, `[ "$1 $2" = "version --json" ] && echo '{"ok":true,"command":"version","data":{"version":"v1.2.0","release":true}}'`)

	if err := verifyLegatoBinary("v1.2.0")(context.Background(), bin); err != nil {
		t.Errorf("refused: %v", err)
	}
}

func TestABinaryIsRefused(t *testing.T) {
	cases := map[string]string{
		"another version":   `echo '{"ok":true,"data":{"version":"v1.1.0","release":true}}'`,
		"that fails to run": `exit 3`,
		"that says nothing": `true`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			err := verifyLegatoBinary("v1.2.0")(context.Background(), fakeBinary(t, script))
			if err == nil {
				t.Fatal("trusted")
			}
			if name == "another version" && !strings.Contains(err.Error(), "v1.1.0") {
				t.Errorf("err = %v, want it to name the version the binary gave", err)
			}
		})
	}
}
