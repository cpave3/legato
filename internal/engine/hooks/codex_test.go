package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// codexEvents lists the (event-name, script-name, expected --activity) tuples
// for every hook the adapter installs. Kept here so tests verify the behavior
// the adapter exposes, not the constants it uses internally.
var codexEvents = []struct {
	event    string
	script   string
	activity string // "" means clear
}{
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PostToolUse", "legato-post-tool-use.sh", "working"},
	{"PermissionRequest", "legato-permission-request.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
}

func TestCodexAdapter_Name(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	if adapter.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "codex")
	}
}

func TestCodexAdapter_EnvVars(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	vars := adapter.EnvVars("TASK-123", "/tmp/sock")
	if vars["LEGATO_TASK_ID"] != "TASK-123" {
		t.Errorf("LEGATO_TASK_ID = %q, want TASK-123", vars["LEGATO_TASK_ID"])
	}
	if _, ok := vars["LEGATO_SOCKET"]; ok {
		t.Error("LEGATO_SOCKET should not be set (CLI uses broadcast)")
	}
}

func TestCodexAdapter_InstallHooks_CreatesDirectoryAndFiles(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	hooksDir := filepath.Join(projectDir, ".codex", "hooks")
	for _, s := range codexEvents {
		path := filepath.Join(hooksDir, s.script)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", path)
		}
	}
}

func TestCodexAdapter_InstallHooks_CreatesHooksJSON(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	settingsPath := filepath.Join(projectDir, ".codex", "hooks.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading hooks.json: %v", err)
	}

	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks,omitempty"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing hooks.json: %v", err)
	}

	for _, s := range codexEvents {
		groups, ok := cfg.Hooks[s.event]
		if !ok {
			t.Errorf("expected event %q in hooks.json", s.event)
			continue
		}
		found := false
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, s.script) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("expected %q script for event %q", s.script, s.event)
		}
	}
}

func TestCodexAdapter_InstallHooks_MergesWithExisting(t *testing.T) {
	projectDir := t.TempDir()
	codexDir := filepath.Join(projectDir, ".codex")
	os.MkdirAll(codexDir, 0o755)

	existing := `{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/bin/python3 /home/user/.codex/hooks/pre_tool_use.py"
          }
        ]
      }
    ]
  }
}`
	os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(existing), 0o644)

	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	var cfg struct {
		Hooks map[string]interface{} `json:"hooks"`
	}
	json.Unmarshal(data, &cfg)

	// PreToolUse should still exist.
	if _, ok := cfg.Hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse hook should be preserved")
	}
	// Stop should now exist too.
	if _, ok := cfg.Hooks["Stop"]; !ok {
		t.Error("Stop hook should be added")
	}
}

func TestCodexAdapter_InstallHooks_Idempotent(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")

	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks first: %v", err)
	}
	if err := adapter.InstallHooks(projectDir); err != nil {
		t.Fatalf("InstallHooks second: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(projectDir, ".codex", "hooks.json"))
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks,omitempty"`
		} `json:"hooks"`
	}
	json.Unmarshal(data, &cfg)

	// Should have exactly one entry per event, not duplicated.
	for _, s := range codexEvents {
		groups := cfg.Hooks[s.event]
		count := 0
		for _, g := range groups {
			count += len(g.Hooks)
		}
		if count != 1 {
			t.Errorf("event %q should have 1 hook entry, got %d", s.event, count)
		}
	}
}

func TestCodexAdapter_InstallHooks_MalformedJSONRejected(t *testing.T) {
	projectDir := t.TempDir()
	codexDir := filepath.Join(projectDir, ".codex")
	os.MkdirAll(codexDir, 0o755)
	os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte("not-json"), 0o644)

	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	err := adapter.InstallHooks(projectDir)
	if err == nil {
		t.Fatal("expected error for malformed hooks.json")
	}
	if !strings.Contains(err.Error(), "parsing existing hooks.json") {
		t.Errorf("expected 'parsing existing hooks.json' error, got: %v", err)
	}
}

func TestCodexAdapter_UninstallHooks_RemovesScriptsAndJSON(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)

	if err := adapter.UninstallHooks(projectDir); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}

	hooksDir := filepath.Join(projectDir, ".codex", "hooks")
	for _, s := range codexEvents {
		path := filepath.Join(hooksDir, s.script)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", path)
		}
	}

	settingsPath := filepath.Join(projectDir, ".codex", "hooks.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Error("expected hooks.json to be removed when empty")
	}
}

func TestCodexAdapter_UninstallHooks_PreservesOtherHooks(t *testing.T) {
	projectDir := t.TempDir()
	codexDir := filepath.Join(projectDir, ".codex")
	os.MkdirAll(codexDir, 0o755)

	existing := `{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/bin/python3 /home/user/.codex/hooks/pre_tool_use.py"
          }
        ]
      }
    ]
  }
}`
	os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(existing), 0o644)

	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)
	adapter.UninstallHooks(projectDir)

	data, _ := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	var cfg struct {
		Hooks map[string]interface{} `json:"hooks"`
	}
	json.Unmarshal(data, &cfg)

	if _, ok := cfg.Hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse hook should be preserved after uninstall")
	}
	for _, s := range codexEvents {
		if _, ok := cfg.Hooks[s.event]; ok {
			t.Errorf("event %q should be removed after uninstall", s.event)
		}
	}
}

func TestCodexAdapter_UninstallHooks_CorruptJSONReturnsError(t *testing.T) {
	projectDir := t.TempDir()
	codexDir := filepath.Join(projectDir, ".codex")
	os.MkdirAll(codexDir, 0o755)
	os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte("not-json"), 0o644)

	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	err := adapter.UninstallHooks(projectDir)
	if err == nil {
		t.Fatal("expected error for corrupt hooks.json during uninstall")
	}
	if !strings.Contains(err.Error(), "parsing hooks.json") {
		t.Errorf("expected 'parsing hooks.json' error, got: %v", err)
	}
}

func TestCodexAdapter_UninstallHooks_NoOpWhenMissing(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	if err := adapter.UninstallHooks(projectDir); err != nil {
		t.Fatalf("UninstallHooks should be no-op when .codex missing: %v", err)
	}
}

func TestCodexAdapter_LaunchCommand_NoArgs(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	got := adapter.LaunchCommand(nil, "", "")
	if got != "codex" {
		t.Errorf("LaunchCommand() = %q, want %q", got, "codex")
	}
}

func TestCodexAdapter_LaunchCommand_WithArgs(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetLaunchArgs([]string{"--model", "gpt-4o"})
	got := adapter.LaunchCommand(nil, "", "")
	want := "codex --model gpt-4o"
	if got != want {
		t.Errorf("LaunchCommand() = %q, want %q", got, want)
	}
}

func TestCodexAdapter_LaunchCommand_WithRolePromptFile(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	got := adapter.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	want := `codex -c developer_instructions="$(cat $LEGATO_ROLE_PROMPT_FILE)"`
	if got != want {
		t.Errorf("LaunchCommand() = %q, want %q", got, want)
	}
}

func TestCodexAdapter_LaunchCommand_WithRolePromptFileAndArgs(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetLaunchArgs([]string{"--model", "gpt-4o"})
	got := adapter.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	want := `codex -c developer_instructions="$(cat $LEGATO_ROLE_PROMPT_FILE)" --model gpt-4o`
	if got != want {
		t.Errorf("LaunchCommand() = %q, want %q", got, want)
	}
}

func TestCodexAdapter_LaunchCommand_WithTier(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetLaunchArgs([]string{"--model", "gpt-4o"})
	adapter.SetTiers(map[string][]string{
		"large": {"--model", "o3-mini"},
	})
	got := adapter.LaunchCommand(nil, "", "large")
	want := "codex --model gpt-4o --model o3-mini"
	if got != want {
		t.Errorf("LaunchCommand() = %q, want %q", got, want)
	}
}

func TestCodexAdapter_LaunchCommand_WithRolePromptFileAndTier(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetLaunchArgs([]string{"--model", "gpt-4o"})
	adapter.SetTiers(map[string][]string{
		"large": {"--model", "o3-mini"},
	})
	got := adapter.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "large")
	want := `codex -c developer_instructions="$(cat $LEGATO_ROLE_PROMPT_FILE)" --model gpt-4o --model o3-mini`
	if got != want {
		t.Errorf("LaunchCommand() = %q, want %q", got, want)
	}
}

func TestCodexAdapter_LaunchCommand_UnknownTier(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	got := adapter.LaunchCommand(nil, "", "ghost")
	if got != "codex" {
		t.Errorf("LaunchCommand() with unknown tier = %q, want %q", got, "codex")
	}
}

func TestCodexAdapter_RoleSystemPrompt_Override(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.SetRoleOverrides(hooks.RolePromptOverrides{"conductor": "custom prompt"})
	got := adapter.RoleSystemPrompt("conductor")
	if got != "custom prompt" {
		t.Errorf("RoleSystemPrompt() = %q, want %q", got, "custom prompt")
	}
}

func TestCodexAdapter_RoleSystemPrompt_BuiltinConductor(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	got := adapter.RoleSystemPrompt("conductor")
	if got == "" {
		t.Error("RoleSystemPrompt('conductor') should return non-empty builtin prompt")
	}
}

func TestCodexAdapter_RoleSystemPrompt_BuiltinWorker(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	got := adapter.RoleSystemPrompt("backend")
	if got == "" {
		t.Error("RoleSystemPrompt('backend') should return non-empty worker prompt")
	}
}

func TestCodexAdapter_InterruptKeys(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	keys := adapter.InterruptKeys()
	if len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("InterruptKeys() = %v, want [Escape]", keys)
	}
}

func TestCodexAdapter_VoiceTrailingEnters(t *testing.T) {
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	va, ok := any(adapter).(interface{ VoiceTrailingEnters() int })
	if !ok {
		t.Fatal("CodexAdapter should implement VoiceTrailingEnters")
	}
	if got := va.VoiceTrailingEnters(); got != 2 {
		t.Errorf("VoiceTrailingEnters() = %d, want 2 (codex needs double Enter)", got)
	}
}

func TestClaudeCodeAdapter_NoVoiceTrailingEnters(t *testing.T) {
	adapter := hooks.NewClaudeCodeAdapter("/usr/bin/legato")
	// Claude Code should NOT implement VoiceDeliveryAdapter — it uses the
	// default of 1 Enter, so no override is needed.
	if _, ok := any(adapter).(interface{ VoiceTrailingEnters() int }); ok {
		t.Error("ClaudeCodeAdapter should not implement VoiceTrailingEnters (uses default 1)")
	}
}

func TestCodexAdapter_InstallHooks_ScriptActivities(t *testing.T) {
	projectDir := t.TempDir()
	adapter := hooks.NewCodexAdapter("/usr/bin/legato")
	adapter.InstallHooks(projectDir)

	for _, ev := range codexEvents {
		path := filepath.Join(projectDir, ".codex", "hooks", ev.script)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", ev.script, err)
		}
		content := string(data)
		if !strings.Contains(content, "LEGATO_TASK_ID") {
			t.Errorf("%s should gate on LEGATO_TASK_ID", ev.script)
		}
		want := fmt.Sprintf(`--activity "%s"`, ev.activity)
		if ev.activity == "" {
			want = `--activity ""`
		}
		if !strings.Contains(content, want) {
			t.Errorf("%s should contain %q", ev.script, want)
		}
	}
}
