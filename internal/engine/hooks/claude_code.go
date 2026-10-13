package hooks

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// legatoHookEvents lists the Claude Code hook events managed by Legato.
var legatoHookEvents = []string{"UserPromptSubmit", "PostToolUse", "Stop", "PermissionRequest", "SessionEnd"}

// legatoScriptNames maps hook events to their script filenames.
var legatoScriptNames = []string{
	"legato-prompt-submit.sh",
	"legato-post-tool-use.sh",
	"legato-stop.sh",
	"legato-permission-request.sh",
	"legato-session-end.sh",
}

// ClaudeCodeAdapter implements the AIToolAdapter interface for Claude Code.
type ClaudeCodeAdapter struct {
	legatoBin     string // absolute path to the legato binary
	roleOverrides RolePromptOverrides
	launchArgs    []string            // appended to `claude` invocation in LaunchCommand
	tiers         map[string][]string // tier name → extra launch args (typically `--model <id>`)
	modDir        string              // installed Legato mod (see InstallClaudeMod); "" leaves sessions tmux-only
	toolsModDir   string              // installed tools plugin (see InstallClaudeToolsMod); "" gives sessions no Legato tools
	// usage names the usage mod and the Manticore instance it reports to;
	// zero when no instance is configured.
	usage claudeUsageReporting
}

// NewClaudeCodeAdapter creates a Claude Code adapter.
// legatoBin is the absolute path to the legato binary (resolved at install time).
func NewClaudeCodeAdapter(legatoBin string) *ClaudeCodeAdapter {
	return &ClaudeCodeAdapter{legatoBin: legatoBin}
}

// SetRoleOverrides configures user-supplied role prompts that take precedence
// over the built-in prompts.
func (a *ClaudeCodeAdapter) SetRoleOverrides(overrides RolePromptOverrides) {
	a.roleOverrides = overrides
}

// SetLaunchArgs configures extra CLI flags appended to the `claude` invocation
// in LaunchCommand. Use to opt into Claude Code modes/flags consistently
// across all swarm participants using this adapter.
func (a *ClaudeCodeAdapter) SetLaunchArgs(args []string) {
	a.launchArgs = args
}

// SetTiers configures named launch profiles that LaunchCommand can append
// based on the per-spawn tier argument. Each tier's args layer after the
// base launch_args, so a tier-specified flag (typically `--model`) wins.
func (a *ClaudeCodeAdapter) SetTiers(tiers map[string][]string) {
	a.tiers = tiers
}

// SetModDir names the installed Legato mod's plugin directory. Sessions the
// adapter launches then load it, which gives Legato direct messages,
// interrupts, permission decisions and the native panel for them.
func (a *ClaudeCodeAdapter) SetModDir(pluginDir string) {
	a.modDir = pluginDir
}

// SetToolsModDir names the installed tools plugin's directory. Sessions the
// adapter launches then load it, which gives their agent Legato's own tools
// through `legato mcp serve`. It works with or without the bridge mod.
func (a *ClaudeCodeAdapter) SetToolsModDir(pluginDir string) {
	a.toolsModDir = pluginDir
}

// claudeUsageReporting is what a session needs to report its usage: the
// usage mod's plugin directory, and where and as whom it reports.
type claudeUsageReporting struct {
	modDir string
	url    string
	apiKey string
}

// SetUsageReporting names the installed usage mod's plugin directory and the
// Manticore instance it reports to. Sessions the adapter launches then load
// the mod beside the bridge mod, and each model request's token counts are
// reported to that instance. It is independent of the bridge mod: a session
// can report usage and stay tmux-driven.
func (a *ClaudeCodeAdapter) SetUsageReporting(pluginDir, url, apiKey string) {
	a.usage = claudeUsageReporting{modDir: pluginDir, url: url, apiKey: apiKey}
}

// BindsNativeEndpoint implements service.NativeBindingAdapter: with the mod
// loaded, a session's bridge binds itself moments after Claude Code starts,
// so messages sent at launch wait for it rather than being typed.
func (a *ClaudeCodeAdapter) BindsNativeEndpoint() bool {
	return a.modDir != ""
}

// RoleSystemPrompt returns the system prompt for a swarm role.
// Adapters opt into prompt injection by implementing this method.
func (a *ClaudeCodeAdapter) RoleSystemPrompt(role string) string {
	return resolveRolePrompt(a.roleOverrides, role)
}

// InterruptKeys implements the InterruptAdapter interface. Sending Escape
// to a Claude Code session aborts the agent's current turn so the urgent
// message can be processed immediately.
func (a *ClaudeCodeAdapter) InterruptKeys() []string { return []string{"Escape"} }

// LaunchCommand returns the shell command that starts an interactive Claude
// Code session. When LEGATO_ROLE_PROMPT_FILE is set, the role's system
// prompt is appended via --append-system-prompt, substituted at shell-
// expansion time to sidestep quoting issues. The brief is delivered
// separately by the agent service via a short send-keys pointer to the
// brief file — see agentService.SpawnAgent.
func (a *ClaudeCodeAdapter) LaunchCommand(env map[string]string, brief, model, tier string) string {
	if env == nil {
		return ""
	}
	cmd := "claude"
	if env["LEGATO_SESSION_POLICY"] == "resume" {
		cmd += " --resume " + shellQuote(env["LEGATO_RESUME_SESSION_ID"])
	}
	if _, ok := env["LEGATO_ROLE_PROMPT_FILE"]; ok && env["LEGATO_SESSION_POLICY"] != "resume" {
		cmd += ` --append-system-prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`
	}
	for _, arg := range a.launchArgs {
		cmd += " " + shellQuote(arg)
	}
	if model != "" {
		cmd += " --model " + shellQuote(model)
	}
	if tier != "" {
		args, ok := a.tiers[tier]
		if !ok || len(args) == 0 {
			// Steady-state validation rejects unknown tiers at propose-plan time;
			// this branch only triggers if a config rotation removed a tier still
			// referenced by a queued sub-task. Log so the silent fallback shows up
			// in operator logs.
			log.Printf("warn: adapter %q has no tier %q configured; spawning with base launch_args only", a.Name(), tier)
		}
		for _, arg := range args {
			cmd += " " + shellQuote(arg)
		}
	}
	return cmd
}

func (a *ClaudeCodeAdapter) Name() string { return "claude-code" }

func (a *ClaudeCodeAdapter) EnvVars(taskID, socketPath string) map[string]string {
	vars := map[string]string{
		"LEGATO_TASK_ID": taskID,
	}
	// Claude Code loads every folder this variable lists as a plugin. Set on
	// the session rather than on the launch command, it also covers a
	// `claude` the person starts by hand in the same pane.
	var dirs []string
	if existing := os.Getenv("CLAUDE_CODE_PLUGIN_DIRS"); existing != "" {
		dirs = append(dirs, existing)
	}
	if a.modDir != "" {
		dirs = append(dirs, a.modDir)
		// The mod spawns the bridge by this path, so it runs the same binary
		// that launched the session whatever the pane's PATH holds.
		vars["LEGATO_BIN"] = a.legatoBin
	}
	if a.usage.modDir != "" {
		dirs = append(dirs, a.usage.modDir)
		vars["LEGATO_MANTICORE_URL"] = a.usage.url
		vars["LEGATO_MANTICORE_API_KEY"] = a.usage.apiKey
	}
	if a.toolsModDir != "" {
		dirs = append(dirs, a.toolsModDir)
		// The plugin starts `legato mcp serve` by this path.
		vars["LEGATO_BIN"] = a.legatoBin
	}
	if a.modDir != "" || a.usage.modDir != "" || a.toolsModDir != "" {
		vars["CLAUDE_CODE_PLUGIN_DIRS"] = strings.Join(dirs, string(os.PathListSeparator))
	}
	return vars
}

func (a *ClaudeCodeAdapter) InstallHooks(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return fmt.Errorf("Claude Code is not configured for this project (no .claude/ directory)")
	}

	hooksDir := filepath.Join(claudeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}

	// Write hook scripts.
	scripts := map[string]string{
		"legato-prompt-submit.sh":      agentStateScript(a.legatoBin, "working"),
		"legato-post-tool-use.sh":      agentStateScript(a.legatoBin, "working"),
		"legato-stop.sh":               claudeStopScript(a.legatoBin),
		"legato-permission-request.sh": agentStateScript(a.legatoBin, "waiting"),
		"legato-session-end.sh":        agentStateScript(a.legatoBin, ""),
	}
	for name, content := range scripts {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}

	// Merge into settings.json.
	return a.mergeSettings(claudeDir)
}

func (a *ClaudeCodeAdapter) UninstallHooks(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks")

	// Remove hook scripts.
	for _, name := range legatoScriptNames {
		os.Remove(filepath.Join(hooksDir, name))
	}

	// Remove Legato entries from settings.json.
	return a.removeSettings(claudeDir)
}

func (a *ClaudeCodeAdapter) mergeSettings(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parsing existing settings.json: %w", err)
		}
	}

	hooksMap, _ := settings["hooks"].(map[string]interface{})
	if hooksMap == nil {
		hooksMap = make(map[string]interface{})
	}

	hooksDir := filepath.Join(claudeDir, "hooks")

	// Add Legato hook entries.
	for i, event := range legatoHookEvents {
		hooksMap[event] = hookEntry(filepath.Join(hooksDir, legatoScriptNames[i]))
	}

	settings["hooks"] = hooksMap

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling settings: %w", err)
	}
	return os.WriteFile(settingsPath, data, 0o644)
}

func (a *ClaudeCodeAdapter) removeSettings(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil // no settings file, nothing to remove
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}

	hooksMap, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, event := range legatoHookEvents {
		delete(hooksMap, event)
	}

	if len(hooksMap) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooksMap
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(settingsPath, out, 0o644)
}

func hookEntry(scriptPath string) []interface{} {
	return []interface{}{
		map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": scriptPath,
				},
			},
		},
	}
}

// claudeStopScript reports the end of a turn. The Stop payload says whether
// work the turn started (a shell, a subagent) is still running and will wake
// the session, so the script hands it on instead of clearing the activity
// itself.
func claudeStopScript(legatoBin string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by legato — do not edit manually.\n")
	b.WriteString("# Called by Claude Code's Stop hook to update agent activity state.\n\n")
	b.WriteString("# Consume the hook payload before the script can exit.\n")
	b.WriteString("payload=$(cat)\n\n")
	b.WriteString("# Only act inside Legato-spawned tmux sessions.\n")
	b.WriteString("[ -z \"$LEGATO_TASK_ID\" ] && exit 0\n")
	b.WriteString("[ -z \"$LEGATO_EXECUTION_ID\" ] && exit 1\n\n")
	b.WriteString(fmt.Sprintf("printf '%%s' \"$payload\" | %s agent stopped \"$LEGATO_TASK_ID\" --execution-id \"$LEGATO_EXECUTION_ID\" --working-dir \"$PWD\"\n", legatoBin))
	b.WriteString("exit 0\n")
	return b.String()
}

func agentStateScript(legatoBin, activity string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by legato — do not edit manually.\n")
	b.WriteString("# Called by an AI tool's hook system to update agent activity state.\n\n")
	b.WriteString("# Consume the hook payload before the script can exit.\n")
	b.WriteString("cat >/dev/null\n\n")
	b.WriteString("# Only act inside Legato-spawned tmux sessions.\n")
	b.WriteString("[ -z \"$LEGATO_TASK_ID\" ] && exit 0\n")
	b.WriteString("[ -z \"$LEGATO_EXECUTION_ID\" ] && exit 1\n\n")
	b.WriteString(fmt.Sprintf("%s agent state \"$LEGATO_TASK_ID\" --execution-id \"$LEGATO_EXECUTION_ID\" --activity \"%s\" --working-dir \"$PWD\"\n", legatoBin, activity))
	b.WriteString("exit 0\n")
	return b.String()
}
