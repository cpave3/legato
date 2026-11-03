package hooks_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestClaudeCodeAdapter_Name(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")

	if adapter.Name() != "claude-code" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "claude-code")
	}
}

func TestClaudeCodeAdapter_EnvVars(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")

	vars := adapter.EnvVars("task123", "/tmp/legato.sock")

	if vars["LEGATO_TASK_ID"] != "task123" {
		t.Errorf("LEGATO_TASK_ID = %q, want %q", vars["LEGATO_TASK_ID"], "task123")
	}
	if _, ok := vars["LEGATO_SOCKET"]; ok {
		t.Error("LEGATO_SOCKET should not be set (CLI uses broadcast)")
	}
}

func TestClaudeCodeAdapter_InstallHooks(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")

	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	// Check hook scripts exist and are executable.
	for _, name := range []string{"legato-prompt-submit.sh", "legato-post-tool-use.sh", "legato-stop.sh", "legato-permission-request.sh", "legato-session-end.sh"} {
		path := filepath.Join(projectDir, ".claude", "hooks", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("hook script %s not found: %v", name, err)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("hook script %s is not executable", name)
		}
	}

	// Check settings.json has hook entries.
	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}

	hooksMap, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("settings.json missing or invalid 'hooks' key")
	}

	for _, event := range []string{"UserPromptSubmit", "PostToolUse", "Stop", "PermissionRequest", "SessionEnd"} {
		if _, ok := hooksMap[event]; !ok {
			t.Errorf("settings.json missing %s hook", event)
		}
	}
}

func TestClaudeCodeAdapter_InstallHooks_PostToolUseConsumesStdin(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	payload := bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))
	script := filepath.Join(projectDir, ".claude", "hooks", "legato-post-tool-use.sh")
	cmd := exec.Command(script)
	cmd.Env = envWithoutLegatoSession()
	cmd.Stdin = payload
	if err := cmd.Run(); err != nil {
		t.Fatalf("running PostToolUse hook: %v", err)
	}
	if payload.Len() != 0 {
		t.Fatalf("PostToolUse hook left %d of %d stdin bytes unread", payload.Len(), 1<<20)
	}
}

func TestClaudeCodeAdapter_InstallHooksPreservesExisting(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "my-validator.sh"},
					},
				},
			},
		},
		"permissions": map[string]interface{}{"allow": []string{"Read"}},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(raw, &settings)

	hooksMap := settings["hooks"].(map[string]interface{})
	if _, ok := hooksMap["PreToolUse"]; !ok {
		t.Error("user's PreToolUse hook was removed")
	}
	if _, ok := hooksMap["Stop"]; !ok {
		t.Error("Legato Stop hook not added")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions setting was removed")
	}
}

func TestClaudeCodeAdapter_InstallHooksFailsWithoutClaudeDir(t *testing.T) {
	projectDir := t.TempDir()

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	err := adapter.InstallHooks(projectDir)
	if err == nil {
		t.Fatal("expected error when .claude/ doesn't exist")
	}
}

func TestClaudeCodeAdapter_UninstallHooksPreservesUserHooks(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)

	// Add a user hook.
	raw, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(raw, &settings)
	hooksMap := settings["hooks"].(map[string]interface{})
	hooksMap["PreToolUse"] = []interface{}{
		map[string]interface{}{"matcher": "Bash", "hooks": []interface{}{
			map[string]interface{}{"type": "command", "command": "user-hook.sh"},
		}},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644)

	if err := adapter.UninstallHooks(projectDir); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}

	// Hook scripts should be gone.
	for _, name := range []string{"legato-prompt-submit.sh", "legato-post-tool-use.sh", "legato-stop.sh", "legato-permission-request.sh", "legato-session-end.sh"} {
		path := filepath.Join(claudeDir, "hooks", name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("hook script %s should have been removed", name)
		}
	}

	// Settings should have user's hook but not Legato's.
	raw, _ = os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	json.Unmarshal(raw, &settings)
	hooksMap = settings["hooks"].(map[string]interface{})

	for _, event := range []string{"UserPromptSubmit", "PostToolUse", "Stop", "PermissionRequest", "SessionEnd"} {
		if _, ok := hooksMap[event]; ok {
			t.Errorf("Legato %s hook should have been removed", event)
		}
	}
	if _, ok := hooksMap["PreToolUse"]; !ok {
		t.Error("user's PreToolUse hook was removed during uninstall")
	}
}

func TestClaudeCodeAdapter_ReinstallOverwrites(t *testing.T) {
	projectDir := t.TempDir()
	os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("second install: %v", err)
	}

	path := filepath.Join(projectDir, ".claude", "hooks", "legato-stop.sh")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("hook script missing after reinstall: %v", err)
	}
}

func TestClaudeCodeAdapter_HookScriptChecksEnvVar(t *testing.T) {
	projectDir := t.TempDir()
	os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)

	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "hooks", "legato-stop.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !strings.Contains(script, "LEGATO_TASK_ID") {
		t.Error("hook script doesn't check LEGATO_TASK_ID")
	}
	if !strings.Contains(script, "legato agent stopped") {
		t.Error("the Stop hook should hand its payload to 'legato agent stopped'")
	}
	if strings.Contains(script, "cat >/dev/null\n\n# Only act") {
		t.Error("the Stop hook must not drain the payload it hands on")
	}
	if !strings.Contains(script, `--execution-id "$LEGATO_EXECUTION_ID"`) || !strings.Contains(script, `[ -z "$LEGATO_EXECUTION_ID" ] && exit 1`) {
		t.Error("hook script must require and report the owning execution ID")
	}
}

func TestClaudeCodeAdapter_HookScriptActivities(t *testing.T) {
	projectDir := t.TempDir()
	os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)

	tests := []struct {
		script   string
		activity string
	}{
		{"legato-prompt-submit.sh", "--activity \"working\""},
		{"legato-post-tool-use.sh", "--activity \"working\""},
		// Stop hands its payload on: it says whether a shell is still running.
		{"legato-stop.sh", "agent stopped \"$LEGATO_TASK_ID\""},
		{"legato-permission-request.sh", "--activity \"waiting\""},
		{"legato-session-end.sh", "--activity \"\""},
	}

	for _, tt := range tests {
		data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "hooks", tt.script))
		if err != nil {
			t.Errorf("reading %s: %v", tt.script, err)
			continue
		}
		if !strings.Contains(string(data), tt.activity) {
			t.Errorf("%s should contain %q", tt.script, tt.activity)
		}
	}
}

func TestClaudeCodeAdapter_InterruptKeys(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	keys := adapter.InterruptKeys()
	if len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("InterruptKeys() = %v, want [Escape]", keys)
	}
}

func TestClaudeCodeAdapter_ImplementsInterruptAdapter(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if _, ok := interface{}(adapter).(interface{ InterruptKeys() []string }); !ok {
		t.Error("ClaudeCodeAdapter does not implement InterruptAdapter")
	}
}

// eventCommands lists the commands settings.json runs for an event, in order.
func eventCommands(t *testing.T, claudeDir, event string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	var commands []string
	for _, group := range settings.Hooks[event] {
		for _, hook := range group.Hooks {
			commands = append(commands, hook.Command)
		}
	}
	return commands
}

// A person's own hooks on the events Legato also uses: Stop runs a sound,
// UserPromptSubmit a logger with a timeout of its own.
const userHooksOnLegatoEvents = `{
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "play-done-sound.sh"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "log-prompt.sh", "timeout": 5}]}]
  }
}`

func TestClaudeCodeAdapter_InstallKeepsUserHooksOnLegatoEvents(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(userHooksOnLegatoEvents), 0o644)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	for range 2 { // installing again changes nothing
		if err := adapter.InstallHooks(projectDir); err != nil {
			t.Fatalf("InstallHooks: %v", err)
		}
	}

	stop := filepath.Join(claudeDir, "hooks", "legato-stop.sh")
	if got := eventCommands(t, claudeDir, "Stop"); !slices.Equal(got, []string{"play-done-sound.sh", stop}) {
		t.Errorf("Stop runs %q, want the user's hook and Legato's once", got)
	}
	submit := filepath.Join(claudeDir, "hooks", "legato-prompt-submit.sh")
	if got := eventCommands(t, claudeDir, "UserPromptSubmit"); !slices.Equal(got, []string{"log-prompt.sh", submit}) {
		t.Errorf("UserPromptSubmit runs %q, want the user's hook and Legato's once", got)
	}
	raw, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if !strings.Contains(string(raw), `"timeout": 5`) {
		t.Errorf("the user's hook lost its timeout:\n%s", raw)
	}
}

func TestClaudeCodeAdapter_InstallReplacesLegatoHooksFromAnotherPath(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	// Installed before the project moved: the same script at its old path.
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"hooks": {"Stop": [
	  {"hooks": [{"type": "command", "command": "/old/place/.claude/hooks/legato-stop.sh"}]}
	]}}`), 0o644)

	if err := hooks.NewClaudeCodeAdapter("/usr/bin/legato").InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	want := []string{filepath.Join(claudeDir, "hooks", "legato-stop.sh")}
	if got := eventCommands(t, claudeDir, "Stop"); !slices.Equal(got, want) {
		t.Errorf("Stop runs %q, want only %q", got, want)
	}
}

func TestClaudeCodeAdapter_UninstallKeepsUserHooksOnLegatoEvents(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(userHooksOnLegatoEvents), 0o644)

	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if err := adapter.UninstallHooks(projectDir); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}

	if got := eventCommands(t, claudeDir, "Stop"); !slices.Equal(got, []string{"play-done-sound.sh"}) {
		t.Errorf("Stop runs %q after uninstall, want only the user's hook", got)
	}
	if got := eventCommands(t, claudeDir, "UserPromptSubmit"); !slices.Equal(got, []string{"log-prompt.sh"}) {
		t.Errorf("UserPromptSubmit runs %q after uninstall, want only the user's hook", got)
	}
	if got := eventCommands(t, claudeDir, "SessionEnd"); len(got) != 0 {
		t.Errorf("SessionEnd still runs %q after uninstall", got)
	}
}
