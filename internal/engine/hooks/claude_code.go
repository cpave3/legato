package hooks

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// legatoScriptNames are the hook scripts earlier versions of `legato hooks
// install --tool claude-code` wrote, which install and uninstall now remove.
var legatoScriptNames = []string{
	"legato-prompt-submit.sh",
	"legato-post-tool-use.sh",
	"legato-stop.sh",
	"legato-permission-request.sh",
	"legato-session-end.sh",
}

// ClaudeCodeAdapter implements the AIToolAdapter interface for Claude Code.
type ClaudeCodeAdapter struct {
	// reviewsOff leaves Legato's review packet out of its prompts.
	reviewsOff    bool
	legatoBin     string // absolute path to the legato binary
	roleOverrides RolePromptOverrides
	launchArgs    []string            // appended to `claude` invocation in LaunchCommand
	tiers         map[string][]string // tier name → extra launch args (typically `--model <id>`)
	modDir        string              // installed Legato mod (see InstallClaudeMod); "" leaves sessions tmux-only
	toolsModDir   string              // installed tools plugin (see InstallClaudeToolsMod); "" gives sessions no Legato tools
	stateModDir   string              // installed state plugin (see InstallClaudeStateMod); "" leaves the state to installed hooks
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

// SetStateModDir names the installed state plugin's directory. Sessions the
// adapter launches then load it, and it reports their state to Legato.
func (a *ClaudeCodeAdapter) SetStateModDir(pluginDir string) {
	a.stateModDir = pluginDir
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
	return resolveRolePrompt(a.roleOverrides, role, !a.reviewsOff)
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
	if inSeatbelt(env) {
		// A process in a Seatbelt sandbox cannot apply one of its own, so
		// Claude Code's sandbox would fail every command: Legato's holds it.
		cmd += ` --settings '{"sandbox":{"enabled":false}}'`
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

// PromptsOffArgs launches Claude Code without its permission prompts, for an
// agent nobody watches. Legato adds them only for such an agent, which runs
// in Legato's sandbox.
func (a *ClaudeCodeAdapter) PromptsOffArgs() []string {
	return []string{"--dangerously-skip-permissions"}
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
	if a.stateModDir != "" {
		dirs = append(dirs, a.stateModDir)
		// The plugin's hooks run `legato agent hook` by this path.
		vars["LEGATO_BIN"] = a.legatoBin
	}
	if a.modDir != "" || a.usage.modDir != "" || a.toolsModDir != "" || a.stateModDir != "" {
		vars["CLAUDE_CODE_PLUGIN_DIRS"] = strings.Join(dirs, string(os.PathListSeparator))
	}
	return vars
}

// InstallHooks takes out the state hooks an earlier `legato hooks install
// --tool claude-code` wrote, and writes none. Claude Code's state is now
// reported by the plugin Legato loads into each session it launches (see
// InstallClaudeStateMod); a script installed by hand only falls behind the
// binary and reports beside it.
func (a *ClaudeCodeAdapter) InstallHooks(projectDir string) error {
	return a.UninstallHooks(projectDir)
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

// removeSettings takes Legato's hooks out of settings.json and leaves
// everything else, a person's own hooks on the same events included.
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
	withoutLegatoHooks(hooksMap)

	if len(hooksMap) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooksMap
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(settingsPath, out, 0o644)
}

// withoutLegatoHooks removes Legato's commands from every event of a
// settings.json hooks object: a command is Legato's when it runs one of
// Legato's scripts, wherever that script is. A group left with no command,
// and an event left with no group, go too. Anything it does not recognise
// is kept as it is.
func withoutLegatoHooks(hooksMap map[string]interface{}) {
	for event, value := range hooksMap {
		groups, ok := value.([]interface{})
		if !ok {
			continue
		}
		var keptGroups []interface{}
		for _, g := range groups {
			group, ok := g.(map[string]interface{})
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			entries, ok := group["hooks"].([]interface{})
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			var kept []interface{}
			for _, e := range entries {
				if !isLegatoHook(e) {
					kept = append(kept, e)
				}
			}
			if len(kept) == 0 {
				continue
			}
			group["hooks"] = kept
			keptGroups = append(keptGroups, group)
		}
		if len(keptGroups) == 0 {
			delete(hooksMap, event)
		} else {
			hooksMap[event] = keptGroups
		}
	}
}

func isLegatoHook(entry interface{}) bool {
	hook, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	command, _ := hook["command"].(string)
	return slices.Contains(legatoScriptNames, filepath.Base(command))
}

// SetReviews says whether agents are told to capture Legato's review tours
// and handoff packets (agents.reviews). On by default.
func (a *ClaudeCodeAdapter) SetReviews(on bool) { a.reviewsOff = !on }

// NeedsReviewsPrompt implements service.ReviewsPromptAdapter: nothing in
// this tool's own prompts asks for a review tour, so the launch prompt does.
func (a *ClaudeCodeAdapter) NeedsReviewsPrompt() bool { return true }

// inSeatbelt says whether the tool will run in Legato's macOS sandbox
// (Seatbelt), inside which it cannot start a sandbox of its own.
func inSeatbelt(env map[string]string) bool {
	return env["LEGATO_SANDBOXED"] == "1" && env["LEGATO_SANDBOX_BACKEND"] == "seatbelt"
}
