package hooks

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// codexScripts pairs each Codex hook event with its installed script name
// and the activity state it reports. Empty activity means "clear".
var codexScripts = []struct {
	event    string
	script   string
	activity string
}{
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PostToolUse", "legato-post-tool-use.sh", "working"},
	{"PermissionRequest", "legato-permission-request.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
}

// CodexAdapter implements the AIToolAdapter interface for the OpenAI Codex CLI.
type CodexAdapter struct {
	legatoBin     string
	roleOverrides RolePromptOverrides
	launchArgs    []string
	// tiers maps tier names to extra launch args (typically the model
	// selector). Layered after launchArgs at LaunchCommand time so tier
	// args win on conflicts.
	tiers map[string][]string
}

// NewCodexAdapter creates a Codex adapter.
// legatoBin is the absolute path to the legato binary (resolved at install time).
func NewCodexAdapter(legatoBin string) *CodexAdapter {
	return &CodexAdapter{legatoBin: legatoBin}
}

// SetRoleOverrides configures user-supplied role prompts that take precedence
// over the built-in prompts.
func (a *CodexAdapter) SetRoleOverrides(overrides RolePromptOverrides) {
	a.roleOverrides = overrides
}

// SetLaunchArgs configures extra CLI flags appended to the `codex` invocation
// in LaunchCommand. Use to opt into Codex modes/flags consistently
// across all swarm participants using this adapter.
func (a *CodexAdapter) SetLaunchArgs(args []string) {
	a.launchArgs = args
}

// SetTiers configures named launch profiles that LaunchCommand appends based
// on the per-spawn tier argument. Args layer after the adapter's base
// launchArgs so a tier-specified flag (typically `--model`) wins.
func (a *CodexAdapter) SetTiers(tiers map[string][]string) {
	a.tiers = tiers
}

// RoleSystemPrompt returns the system prompt for a swarm role.
func (a *CodexAdapter) RoleSystemPrompt(role string) string {
	return resolveRolePrompt(a.roleOverrides, role)
}

// InterruptKeys implements the InterruptAdapter interface. Sending Escape
// to a Codex session aborts the agent's current turn so the urgent message
// can be processed immediately.
func (a *CodexAdapter) InterruptKeys() []string { return []string{"Escape"} }

// LaunchCommand returns the shell command that starts an interactive Codex
// session. Codex does not document a system-prompt injection flag, so the
// role prompt file is not referenced here — the brief is delivered via
// post-launch send-keys by the agent service.
func (a *CodexAdapter) LaunchCommand(env map[string]string, brief, tier string) string {
	_ = env
	_ = brief
	cmd := "codex"
	for _, arg := range a.launchArgs {
		cmd += " " + shellQuote(arg)
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

func (a *CodexAdapter) Name() string { return "codex" }

func (a *CodexAdapter) EnvVars(taskID, socketPath string) map[string]string {
	return map[string]string{
		"LEGATO_TASK_ID": taskID,
	}
}

// InstallHooks writes the Legato activity-update scripts into the Codex hooks
// directory and merges entries into the project's hooks.json.
func (a *CodexAdapter) InstallHooks(projectDir string) error {
	codexDir := filepath.Join(projectDir, ".codex")
	hooksDir := filepath.Join(codexDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating codex hooks directory: %w", err)
	}

	// Write hook scripts.
	for _, s := range codexScripts {
		path := filepath.Join(hooksDir, s.script)
		if err := os.WriteFile(path, []byte(agentStateScript(a.legatoBin, s.activity)), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", s.script, err)
		}
	}

	// Merge into hooks.json.
	return a.mergeHooksJSON(codexDir, hooksDir)
}

// UninstallHooks removes the Legato-managed scripts from the Codex hooks
// directory and surgically removes only Legato entries from hooks.json.
func (a *CodexAdapter) UninstallHooks(projectDir string) error {
	codexDir := filepath.Join(projectDir, ".codex")
	hooksDir := filepath.Join(codexDir, "hooks")

	// Remove hook scripts.
	for _, s := range codexScripts {
		path := filepath.Join(hooksDir, s.script)
		_ = os.Remove(path)
	}

	// Clean up hooks.json.
	return a.unmergeHooksJSON(codexDir, hooksDir)
}

// codexHooksConfig is the top-level shape of Codex's hooks.json.
type codexHooksConfig struct {
	Hooks map[string][]codexMatcherGroup `json:"hooks"`
}

type codexMatcherGroup struct {
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []codexHookEntry  `json:"hooks,omitempty"`
}

type codexHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// mergeHooksJSON reads an existing hooks.json (if any), merges Legato entries
// into the "hooks" object, and writes it back. Existing non-Legato entries
// are preserved.
func (a *CodexAdapter) mergeHooksJSON(codexDir, hooksDir string) error {
	settingsPath := filepath.Join(codexDir, "hooks.json")

	var cfg codexHooksConfig
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing existing hooks.json: %w", err)
		}
	}
	if cfg.Hooks == nil {
		cfg.Hooks = make(map[string][]codexMatcherGroup)
	}

	for _, s := range codexScripts {
		scriptPath := filepath.Join(hooksDir, s.script)
		entry := codexHookEntry{Type: "command", Command: scriptPath}
		group := codexMatcherGroup{Hooks: []codexHookEntry{entry}}

		existing := cfg.Hooks[s.event]
		// Check if this exact script path is already present.
		found := false
		for i := range existing {
			for j := range existing[i].Hooks {
				if existing[i].Hooks[j].Command == scriptPath {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			cfg.Hooks[s.event] = append(existing, group)
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling hooks.json: %w", err)
	}
	return os.WriteFile(settingsPath, data, 0o644)
}

// unmergeHooksJSON reads hooks.json and removes only Legato-managed entries
// (matched by script path). If the "hooks" object becomes empty, the file
// is deleted.
func (a *CodexAdapter) unmergeHooksJSON(codexDir, hooksDir string) error {
	settingsPath := filepath.Join(codexDir, "hooks.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading hooks.json: %w", err)
	}

	var cfg codexHooksConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing hooks.json: %w", err)
	}
	if cfg.Hooks == nil {
		return nil
	}

	for _, s := range codexScripts {
		scriptPath := filepath.Join(hooksDir, s.script)
		existing := cfg.Hooks[s.event]
		var filtered []codexMatcherGroup
		for _, g := range existing {
			var hookFiltered []codexHookEntry
			for _, h := range g.Hooks {
				if h.Command != scriptPath {
					hookFiltered = append(hookFiltered, h)
				}
			}
			if len(hookFiltered) > 0 {
				filtered = append(filtered, codexMatcherGroup{
					Matcher: g.Matcher,
					Hooks:   hookFiltered,
				})
			}
		}
		if len(filtered) > 0 {
			cfg.Hooks[s.event] = filtered
		} else {
			delete(cfg.Hooks, s.event)
		}
	}

	if len(cfg.Hooks) == 0 {
		_ = os.Remove(settingsPath)
		return nil
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil
	}
	return os.WriteFile(settingsPath, out, 0o644)
}
