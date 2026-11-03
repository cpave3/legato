package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// claudeHome gives the test its own home with a Claude Code configuration
// file holding config, and returns the file's path.
func claudeHome(t *testing.T, config string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	file := filepath.Join(home, ".claude.json")
	if config != "" {
		if err := os.WriteFile(file, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return file
}

func readClaudeConfig(t *testing.T, file string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("config is not JSON: %v\n%s", err, data)
	}
	return config
}

func trusted(config map[string]any, dir string) bool {
	projects, _ := config["projects"].(map[string]any)
	project, _ := projects[dir].(map[string]any)
	return project["hasTrustDialogAccepted"] == true
}

func TestTrustWorkDir_TrustsAnUntrustedFolderAndKeepsTheRest(t *testing.T) {
	file := claudeHome(t, `{
  "oauthAccount": {"emailAddress": "me@example.com"},
  "numStartups": 12,
  "projects": {
    "/elsewhere": {"allowedTools": ["Bash"], "hasTrustDialogAccepted": true}
  }
}`)
	dir := t.TempDir()

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
		t.Fatal(err)
	}

	config := readClaudeConfig(t, file)
	if !trusted(config, dir) {
		t.Fatalf("%s not trusted: %v", dir, config["projects"])
	}
	if config["oauthAccount"].(map[string]any)["emailAddress"] != "me@example.com" || config["numStartups"] != 12.0 {
		t.Errorf("other settings changed: %v", config)
	}
	other := config["projects"].(map[string]any)["/elsewhere"].(map[string]any)
	if tools, _ := other["allowedTools"].([]any); len(tools) != 1 || tools[0] != "Bash" {
		t.Errorf("another project's settings changed: %v", other)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestTrustWorkDir_KeysALinkedFolderByItsRealPath(t *testing.T) {
	file := claudeHome(t, `{}`)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, link); err != nil {
		t.Fatal(err)
	}

	config := readClaudeConfig(t, file)
	if !trusted(config, real) || trusted(config, link) {
		t.Errorf("want only %s trusted, got %v", real, config["projects"])
	}
}

func TestTrustWorkDir_LeavesTheFileAloneWhenAlreadyTrusted(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "a", "b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]string{"the folder": dir, "a parent outside git": parent} {
		t.Run(name, func(t *testing.T) {
			config := `{"projects": {` + mustJSON(t, key) + `: {"hasTrustDialogAccepted": true}}}`
			file := claudeHome(t, config)

			if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
				t.Fatal(err)
			}

			if data, _ := os.ReadFile(file); string(data) != config {
				t.Errorf("file rewritten:\n%s", data)
			}
		})
	}
}

func TestTrustWorkDir_ATrustedParentAboveTheRepositoryDoesNotCount(t *testing.T) {
	// Claude Code looks for a trusted parent only up to the git root.
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	dir := filepath.Join(repo, "sub")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := claudeHome(t, `{"projects": {`+mustJSON(t, parent)+`: {"hasTrustDialogAccepted": true}}}`)

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
		t.Fatal(err)
	}

	if !trusted(readClaudeConfig(t, file), dir) {
		t.Errorf("%s not trusted", dir)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTrustWorkDir_UsesTheSessionsConfigDir(t *testing.T) {
	home := claudeHome(t, `{}`)
	configDir := t.TempDir()
	file := filepath.Join(configDir, ".claude.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	env := map[string]string{"CLAUDE_CONFIG_DIR": configDir}
	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(env, dir); err != nil {
		t.Fatal(err)
	}

	if !trusted(readClaudeConfig(t, file), dir) {
		t.Errorf("%s not trusted in the session's config", dir)
	}
	if trusted(readClaudeConfig(t, home), dir) {
		t.Errorf("home config changed too")
	}
}

func TestTrustWorkDir_LeavesAMissingConfigMissing(t *testing.T) {
	// Claude Code that never ran has its onboarding to do; Legato does not
	// start a configuration for it.
	file := claudeHome(t, "")

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("config created: %v", err)
	}
}

func TestTrustWorkDir_WritesThroughALinkedConfig(t *testing.T) {
	link := claudeHome(t, "")
	target := filepath.Join(t.TempDir(), "claude.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v %v", info, err)
	}
	if !trusted(readClaudeConfig(t, target), dir) {
		t.Errorf("%s not trusted in the link's target", dir)
	}
}

func TestTrustWorkDir_WaitsForClaudeCodesLock(t *testing.T) {
	file := claudeHome(t, `{}`)
	lock := file + ".lock"
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	released := make(chan string)
	go func() {
		time.Sleep(300 * time.Millisecond)
		data, _ := os.ReadFile(file)
		os.Remove(lock)
		released <- string(data)
	}()

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
		t.Fatal(err)
	}

	if whileLocked := <-released; whileLocked != `{}` {
		t.Errorf("written while Claude Code held the lock: %s", whileLocked)
	}
	if !trusted(readClaudeConfig(t, file), dir) {
		t.Errorf("%s not trusted", dir)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock left behind: %v", err)
	}
}

func TestTrustWorkDir_TakesOverAStaleLock(t *testing.T) {
	file := claudeHome(t, `{}`)
	lock := file + ".lock"
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(nil, dir); err != nil {
		t.Fatal(err)
	}

	if !trusted(readClaudeConfig(t, file), dir) {
		t.Errorf("%s not trusted", dir)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock left behind: %v", err)
	}
}

func TestTrustWorkDir_ExpandsATildeConfigDir(t *testing.T) {
	file := claudeHome(t, "")
	home := filepath.Dir(file)
	if err := os.MkdirAll(filepath.Join(home, "cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	inCfg := filepath.Join(home, "cfg", ".claude.json")
	if err := os.WriteFile(inCfg, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	env := map[string]string{"CLAUDE_CONFIG_DIR": "~/cfg"}
	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").TrustWorkDir(env, dir); err != nil {
		t.Fatal(err)
	}

	if !trusted(readClaudeConfig(t, inCfg), dir) {
		t.Errorf("%s not trusted in ~/cfg", dir)
	}
}
