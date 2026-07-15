package hooks

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// chimeraScripts pairs each Chimera hook event with its installed script name
// and the activity state it reports. Empty activity means "clear".
var chimeraScripts = []struct {
	event    string
	script   string
	activity string
}{
	{"SessionCreated", "legato-session-created.sh", "capture"},
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PostToolUse", "legato-post-tool-use.sh", "working"},
	{"PermissionRequest", "legato-permission.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
	{"SessionEnd", "legato-session-end.sh", ""},
	{"Interrupt", "legato-interrupt.sh", ""},
	{"Timeout", "legato-timeout.sh", ""},
}

// ChimeraAdapter implements the AIToolAdapter interface for the Chimera coding agent.
type ChimeraAdapter struct {
	legatoBin     string
	roleOverrides RolePromptOverrides
	launchArgs    []string
	// modes maps swarm role labels to Chimera mode names. nil or empty
	// means "no mode injection" — Chimera launches in its own default
	// mode. legato does NOT ship default mode files (those would be
	// project-specific files on the user's filesystem); users opt into
	// per-role modes explicitly via cfg.Adapters.chimera.modes.
	modes map[string]string
	// tiers maps tier names to extra launch args (typically the model
	// selector). Layered after launchArgs at LaunchCommand time so tier
	// args win on conflicts.
	tiers map[string][]string
}

// NewChimeraAdapter creates a Chimera adapter.
func NewChimeraAdapter(legatoBin string) *ChimeraAdapter {
	return &ChimeraAdapter{legatoBin: legatoBin}
}

// SetRoleOverrides configures user-supplied role prompts.
func (a *ChimeraAdapter) SetRoleOverrides(overrides RolePromptOverrides) {
	a.roleOverrides = overrides
}

// SetLaunchArgs configures extra CLI flags appended to the `chimera` invocation
// in LaunchCommand. Use to opt into Chimera modes/flags (e.g. --sandbox,
// --mode agent) consistently across all swarm participants using this adapter.
func (a *ChimeraAdapter) SetLaunchArgs(args []string) {
	a.launchArgs = args
}

// SetModes configures the role → Chimera mode mapping used by LaunchCommand
// to inject `--mode <name>` based on the agent's role. The user opts in
// explicitly — legato does not ship default mode files, so unconfigured
// behavior is "no --mode flag passed" (Chimera uses its own default mode).
func (a *ChimeraAdapter) SetModes(modes map[string]string) {
	a.modes = modes
}

// SetTiers configures named launch profiles that LaunchCommand appends based
// on the per-spawn tier argument. Args layer after the adapter's base
// launchArgs so a tier-specified flag (typically `--model`) wins.
func (a *ChimeraAdapter) SetTiers(tiers map[string][]string) {
	a.tiers = tiers
}

// resolveMode picks the Chimera mode name for the given role from user
// configuration. Returns empty string when no mapping exists — in that
// case LaunchCommand omits `--mode`.
func (a *ChimeraAdapter) resolveMode(role string) string {
	if a.modes == nil {
		return ""
	}
	if m, ok := a.modes[role]; ok {
		return m
	}
	if role != "conductor" && role != "" {
		return a.modes["worker"]
	}
	return ""
}

// RoleSystemPrompt returns the system prompt for a swarm role.
func (a *ChimeraAdapter) RoleSystemPrompt(role string) string {
	return resolveRolePrompt(a.roleOverrides, role)
}

// LaunchIsSelfKickoff reports that Chimera's `--prompt` flag treats its value
// as the initial user turn — so the launch itself starts the agent. The agent
// service skips the post-launch "read your brief" send-keys for Chimera to
// avoid a redundant second user turn. The role prompt content already
// includes the brief-reading instructions.
func (a *ChimeraAdapter) LaunchIsSelfKickoff() bool { return true }

// RolePromptPreamble returns Chimera-specific guidance prepended to every
// role prompt. Chimera's sandbox mode isolates tool calls from the host
// filesystem and PATH, so any attempt to invoke `legato` or read files
// pointed to by LEGATO_* env vars from a sandboxed tool call will fail.
// The agent must run those specific tool calls in host mode.
func (a *ChimeraAdapter) RolePromptPreamble() string {
	return chimeraSandboxPreamble
}

// GeneralPrompt returns the first user turn for a plain (non-swarm) Chimera
// spawn. It is intentionally short and swarm-free: the agent is a standalone
// legato-spawned session, not a swarm participant, so it must not look for a
// brief or sub-task context that doesn't exist.
func (a *ChimeraAdapter) GeneralPrompt() string {
	return chimeraGeneralPrompt
}

const chimeraGeneralPrompt = "You are a standalone coding agent launched by Legato. " +
	"No task brief, sub-task, or swarm context was provided — work on whatever " +
	"the user asks directly. If your session is sandboxed and you need to run the " +
	"`legato` CLI or read host-side paths, switch that tool call to host mode; " +
	"otherwise sandboxed calls will return \"command not found\" / \"no such file\".\n" +
	"\n" +
	"## Review capture\n" +
	"\n" +
	"The user reviews your work as a guided tour. Narrate as you go:\n" +
	"\n" +
	"- **Make reasonable semantic commits as you work** — keep commits logically " +
	"coherent, with bodies explaining *why* when decisions or tradeoffs need context.\n" +
	"- **Build a granular reading order with chapters.** Group related hunks with " +
	"`legato review chapter \"<title>\" [\"<narration>\"] --include <path>:<1-based-hunk>` " +
	"and repeat `--include` for every hunk in that chapter. Use `--risk " +
	"high|medium|low|unsure` and `--order N` where useful; inspect `legato review " +
	"show` or the diff to choose hunk numbers. Chapters should guide the reviewer " +
	"through the change more precisely than commit boundaries.\n" +
	"- **Flag the risky parts.** Keep `legato review annotate` compatibility for " +
	"extra commit or file context. Run " +
	"`legato review annotate [<sha>] \"<extra context>\" --risk high|medium|low|unsure` " +
	"on any commit the reviewer should scrutinize (defaults to your latest " +
	"commit). Use `--order N` to suggest a reading order when it differs from " +
	"commit order, and `--file <path> \"<note>\"` for context that isn't tied " +
	"to one commit. When individual hunks need context, use " +
	"`legato review annotate [sha] \"text\" --file <path> --hunk <1-based N>`; " +
	"inspect `legato review show` or the diff to choose the hunk number.\n" +
	"- **Signal when you're done.** Run `legato review ready \"<one-line summary>\"` " +
	"when your work is ready for human review.\n" +
	"- **Answer review questions.** Messages prefixed `[legato review]` are " +
	"reviewer questions about a specific step; each includes the exact " +
	"`legato review answer <step-id> \"...\"` command to reply with. Answer " +
	"through that command (not just chat) so the reply lands in the review record.\n" +
	"\n" +
	"### Named review tours\n" +
	"\n" +
	"Every `legato review` verb (annotate, chapter, ready, show, sync, answer) " +
	"accepts `--name <review-name>` to scope its packet. If you are working on " +
	"multiple distinct features in a single session, name each review tour " +
	"(`--name auth`, `--name search`, …) so the packets stay separate and the " +
	"reviewer gets one tour per feature. For a single-feature session the " +
	"default (no `--name`) is fine. `LEGATO_REVIEW_NAME` is used as a fallback " +
	"when `--name` is omitted, so you can set it once at the start of a " +
	"multi-feature session and skip the flag on every call.\n"

const chimeraSandboxPreamble = "## Chimera-specific guidance for legato\n" +
	"\n" +
	"You are running inside Chimera. If your session is sandboxed (e.g. " +
	"started with `--sandbox`), tool calls execute in an isolated environment " +
	"that does NOT have access to:\n" +
	"\n" +
	"- the `legato` binary on the host's PATH\n" +
	"- files referenced by `$LEGATO_BRIEF_FILE`, `$LEGATO_ROLE_PROMPT_FILE`, " +
	"or any other host-side path\n" +
	"- the host's environment variables (LEGATO_TASK_ID, LEGATO_SUBTASK_ID, " +
	"LEGATO_PARENT_TASK_ID, etc.)\n" +
	"\n" +
	"**You MUST run any legato CLI invocation or any read of LEGATO_* paths in " +
	"host mode**, not sandbox mode. Sandboxed calls to `legato swarm progress`, " +
	"`legato swarm built`, `legato review annotate`, `cat $LEGATO_BRIEF_FILE`, " +
	"etc. will silently fail or return \"command not found\" / \"no such file\".\n" +
	"\n" +
	"To fetch the current task context directly, run `legato task show " +
	"$LEGATO_TASK_ID` in host mode. Use `legato task show $LEGATO_TASK_ID " +
	"--format full` when you need structured metadata as well as the " +
	"description.\n" +
	"\n" +
	"If you see those errors when trying to interact with legato, switch the " +
	"specific tool call to host mode and retry. Code edits, greps, and other " +
	"work that's confined to the project directory can stay sandboxed.\n"

// LaunchCommand returns the shell command that starts an interactive Chimera
// session. The role system prompt is read from the file referenced by
// LEGATO_ROLE_PROMPT_FILE and substituted into the `--prompt` flag at shell-
// expansion time. The role prompt content (which embeds the read-your-brief
// instruction via the conductor.md / worker.md templates) becomes Chimera's
// initial user turn — no separate kickoff send-keys is needed.
//
// Returns empty string when no role prompt file is set.
func (a *ChimeraAdapter) LaunchCommand(env map[string]string, brief, tier string) string {
	if env == nil {
		return ""
	}
	cmd := "chimera"
	if name := env["LEGATO_CHIMERA_SESSION_NAME"]; name != "" {
		cmd += " --session-name " + shellQuote(name)
	}
	if id := env["LEGATO_CHIMERA_SESSION_ID"]; id != "" {
		cmd += " --session-id " + shellQuote(id)
	}
	if policy := env["LEGATO_CHIMERA_SESSION_EXISTS"]; policy != "" {
		cmd += " --session-exists " + shellQuote(policy)
	}
	if _, ok := env["LEGATO_ROLE_PROMPT_FILE"]; ok && env["LEGATO_CHIMERA_SESSION_EXISTS"] != "resume" {
		cmd += ` --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`
	}

	// Auto-activate a Chimera mode based on the agent's role. Mapping comes
	// from cfg.Adapters.chimera.modes (via SetModes) with fallback to the
	// built-in defaults. Skipped if the user has already specified --mode
	// in launch_args (their explicit override wins).
	if !launchArgsContainsMode(a.launchArgs) {
		if mode := a.resolveMode(env["LEGATO_AGENT_ROLE"]); mode != "" {
			cmd += " --mode " + shellQuote(mode)
		}
	}

	for _, arg := range a.launchArgs {
		cmd += " " + shellQuote(arg)
	}
	if tier != "" {
		args, ok := a.tiers[tier]
		if !ok || len(args) == 0 {
			// See claude_code.go for why this branch is logged rather than
			// errored — config rotations can leave persisted sub-tasks
			// referencing tiers that no longer exist.
			log.Printf("warn: adapter %q has no tier %q configured; spawning with base launch_args only", a.Name(), tier)
		}
		for _, arg := range args {
			cmd += " " + shellQuote(arg)
		}
	}
	return cmd
}

// launchArgsContainsMode reports whether the user-supplied launch args
// already include a `--mode` flag. When true, the adapter skips its
// automatic mode injection so the user override wins.
func launchArgsContainsMode(args []string) bool {
	for _, a := range args {
		if a == "--mode" || strings.HasPrefix(a, "--mode=") {
			return true
		}
	}
	return false
}

// InterruptKeys implements the InterruptAdapter interface. Sending Escape
// to a Chimera session aborts the agent's current turn so the urgent message
// can be processed immediately.
func (a *ChimeraAdapter) InterruptKeys() []string { return []string{"Escape"} }

func (a *ChimeraAdapter) Name() string { return "chimera" }

func (a *ChimeraAdapter) EnvVars(taskID, socketPath string) map[string]string {
	// LEGATO_TASK_ID is the gating env var for the Chimera hook scripts at
	// ~/.chimera/hooks/<event>/legato-*.sh — without it, those scripts
	// exit early and activity never gets reported back to legato. We set
	// it ourselves so swarms running Chimera as their only adapter still
	// see RUNNING / WAITING / IDLE state changes on the agents view.
	return map[string]string{
		"LEGATO_TASK_ID": taskID,
	}
}

// InstallHooks writes the Legato activity-update scripts into Chimera's global
// hook directories. projectDir is unused: Chimera hooks are per-user, not per-project.
func (a *ChimeraAdapter) InstallHooks(projectDir string) error {
	for _, s := range chimeraScripts {
		dir := chimeraHookDir(s.event)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating chimera hook dir %s: %w", dir, err)
		}
		path := filepath.Join(dir, s.script)
		content := agentStateScript(a.legatoBin, s.activity)
		if s.event == "SessionCreated" {
			content = chimeraSessionCreatedScript(a.legatoBin)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func chimeraSessionCreatedScript(legatoBin string) string {
	return fmt.Sprintf(`#!/bin/sh
# Generated by legato — do not edit manually.
[ -z "$LEGATO_TASK_ID" ] && exit 0
[ -z "$CHIMERA_SESSION_ID" ] && exit 0
%s agent session-created "$LEGATO_TASK_ID" "$CHIMERA_SESSION_ID"
`, legatoBin)
}

// UninstallHooks removes the Legato-managed scripts from Chimera's hook directories.
func (a *ChimeraAdapter) UninstallHooks(projectDir string) error {
	for _, s := range chimeraScripts {
		path := filepath.Join(chimeraHookDir(s.event), s.script)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}

// chimeraHookDir returns the global Chimera hook directory for a given event.
// Chimera scans ~/.chimera/hooks/<EventName>/ on every event firing.
func chimeraHookDir(event string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chimera", "hooks", event)
}
