package cli

import (
	"os"
	"path/filepath"
)

// Pin the provider's storage root so a later launch does not select another
// configuration directory when the parent process's environment changes.
func nativeSessionStorageDir(kind string) (string, error) {
	key, directory := "CODEX_HOME", ".codex"
	if kind == "claude-code" {
		key, directory = "CLAUDE_CONFIG_DIR", ".claude"
	}
	if path := os.Getenv(key); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(home, directory))
}
