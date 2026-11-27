package handofftest

import (
	"os"
	"path/filepath"
	"testing"
)

// bridgeStorage is the storage root a session's bridge reports when it saves
// the conversation, as cli's nativeSessionStorageDir finds it: the tool's
// variable, else its folder in the home. The worker launches a Claude Code
// agent with the account its execution recorded, so its bridge reports that
// account's root, and the store refuses any other.
func bridgeStorage(t *testing.T, kind string) string {
	t.Helper()
	key, folder := "CODEX_HOME", ".codex"
	if kind == "claude-code" {
		key, folder = "CLAUDE_CONFIG_DIR", ".claude"
	}
	dir := os.Getenv(key)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		dir = filepath.Join(home, folder)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
