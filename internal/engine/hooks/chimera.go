package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	{"PermissionRequest", "legato-permission.sh", "waiting"},
	{"PermissionResolved", "legato-permission-resolved.sh", "payload"},
	{"UserInputRequired", "legato-user-input-required.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
	{"OnError", "legato-on-error.sh", "error"},
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
	// mcpTools gives sessions Legato's MCP server, `legato mcp serve`, with
	// mcpTrustedTools callable without a prompt.
	mcpTools        bool
	mcpTrustedTools []string
	// toolsPrompt tells a session's agent about those tools. Chimera does
	// not pass an MCP server's own instructions to its model.
	toolsPrompt string
	// mcpConfigProbe reports whether the installed Chimera accepts
	// --mcp-config. It is asked once, at the first launch that needs it.
	mcpConfigProbe  func() bool
	mcpConfigOnce   sync.Once
	mcpConfigUsable bool
}

// NewChimeraAdapter creates a Chimera adapter.
func NewChimeraAdapter(legatoBin string) *ChimeraAdapter {
	return &ChimeraAdapter{legatoBin: legatoBin}
}

// SetMCPTools gives the sessions the adapter launches Legato's own tools:
// LaunchCommand then passes `legato mcp serve` to Chimera as a server for that
// session only (--mcp-config), so nothing is written to the person's Chimera
// config. trustedTools are the tool names Chimera runs without asking.
func (a *ChimeraAdapter) SetMCPTools(enabled bool, trustedTools []string) {
	a.mcpTools = enabled
	a.mcpTrustedTools = trustedTools
}

// SetToolsPrompt sets what a session's agent is told about Legato's tools.
func (a *ChimeraAdapter) SetToolsPrompt(prompt string) {
	a.toolsPrompt = prompt
}

// ToolsPrompt implements service.ToolsPromptAdapter: the guidance belongs in
// the launch prompt of a session whose launch adds the server, and of no
// other (the tools are off, Chimera is too old for the flag, or the person's
// own --mcp-config replaced Legato's).
func (a *ChimeraAdapter) ToolsPrompt() string {
	if a.mcpConfigFlag() == "" {
		return ""
	}
	return a.toolsPrompt
}

// SetMCPConfigProbe replaces how the adapter learns whether the installed
// Chimera accepts --mcp-config. The default reads `chimera --help`.
func (a *ChimeraAdapter) SetMCPConfigProbe(probe func() bool) {
	a.mcpConfigProbe = probe
}

// chimeraAcceptsMCPConfig reads `chimera --help` for the flag. A Chimera from
// before the flag existed exits on it as an unknown option, so a session
// would not start at all.
func chimeraAcceptsMCPConfig() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "chimera", "--help").Output()
	return err == nil && strings.Contains(string(out), "--mcp-config")
}

// mcpConfigFlag returns the --mcp-config argument that adds Legato's server
// to a session, or "" when the session gets none.
func (a *ChimeraAdapter) mcpConfigFlag() string {
	if !a.mcpTools || launchArgsContain(a.launchArgs, "--mcp-config") {
		// With a flag of their own in launch_args, the person's wins:
		// Chimera takes the flag once.
		return ""
	}
	a.mcpConfigOnce.Do(func() {
		probe := a.mcpConfigProbe
		if probe == nil {
			probe = chimeraAcceptsMCPConfig
		}
		a.mcpConfigUsable = probe()
		if !a.mcpConfigUsable {
			log.Printf("chimera does not accept --mcp-config; its sessions start without Legato's tools (update Chimera to add them)")
		}
	})
	if !a.mcpConfigUsable {
		return ""
	}
	type server struct {
		Transport string   `json:"transport"`
		Command   string   `json:"command"`
		Args      []string `json:"args"`
		// Chimera starts a stdio server with a few variables unless its
		// entry has an env map, and then with all of its own. The one
		// reference is to a variable every Legato session has: Chimera
		// refuses to start a server whose reference is unset.
		Env          map[string]string `json:"env"`
		TrustedTools []string          `json:"trustedTools,omitempty"`
	}
	document, err := json.Marshal(map[string]map[string]server{"mcpServers": {mcpServerName: {
		Transport: "stdio", Command: a.legatoBin, Args: mcpServerArgs,
		Env:          map[string]string{"LEGATO_TASK_ID": "env:LEGATO_TASK_ID"},
		TrustedTools: a.mcpTrustedTools,
	}}})
	if err != nil {
		return ""
	}
	return " --mcp-config " + shellQuote(string(document))
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
	"the user asks directly. When implementation should wait for plan approval, " +
	"author plan.md and plan.json, submit them with `legato plan submit`, and do not " +
	"implement until approved; after successful implementation and verification, run " +
	"`legato plan complete`; use the legato-plan skill for the full workflow. " +
	"plan.md can show an image file of its own bundle, such as a screenshot of the " +
	"screen the plan changes: `![The board today](assets/board.png)`. " +
	"If your session is sandboxed and you need to run the " +
	"`legato` CLI or read host-side paths, switch that tool call to host mode; " +
	"otherwise sandboxed calls will return \"command not found\" / \"no such file\".\n" +
	"\n" +
	"## Peer coordination\n" +
	"\n" +
	"Other Legato agents may share this Git worktree. Run `legato agent peers` to " +
	"discover them, `legato agent message <task-or-execution-id> \"<text>\"` to coordinate " +
	"directly, and `legato agent broadcast \"<text>\"` to announce files you are " +
	"touching. Unexpected changes may belong to a peer: do not revert them before " +
	"checking with the responsible agent. Peer announcements are advisory, not locks.\n" +
	"\n" +
	"## Review capture\n" +
	"\n" +
	"The user reviews your work as a guided tour. Narrate as you go. If " +
	"`LEGATO_TASK_EPHEMERAL=1`, skip all review capture and review CLI commands " +
	"unless the user explicitly asks for a review.\n" +
	"\n" +
	"- **Make reasonable semantic commits as you work** — keep commits logically " +
	"coherent, with bodies explaining *why* when decisions or tradeoffs need context.\n" +
	"- **Build a granular reading order with chapters.** Group related hunks with " +
	"`legato review chapter \"<title>\" [\"<narration>\"] --include <path>:<1-based-hunk>` " +
	"and repeat `--include` for every hunk in that chapter. Use `--risk " +
	"high|medium|low|unsure` and `--order N` where useful; inspect `legato review " +
	"show` or the diff to choose hunk numbers. Chapters should guide the reviewer " +
	"through the change more precisely than commit boundaries. Chapters belong to " +
	"the active review pass; never rewrite chapters from an earlier pass. Narration " +
	"is Markdown. For more than a sentence, write it to a file and pass " +
	"`--narration-file <path>` in place of the narration argument. An image file the " +
	"Markdown names is shown with the chapter: `![After](after.png)` beside that file, " +
	"or an absolute path. Show one when the reviewer has to see the change to judge " +
	"it, such as a screen before and after.\n" +
	"- **Compare against the captured plan.** When context includes a captured plan " +
	"revision, treat it as agreed scope and call out deviations. Reviews without a " +
	"plan still work normally.\n" +
	"- **Flag the risky parts.** Keep `legato review annotate` compatibility for " +
	"extra commit or file context. Run " +
	"`legato review annotate [<sha>] \"<extra context>\" --risk high|medium|low|unsure` " +
	"on any commit the reviewer should scrutinize (defaults to your latest " +
	"commit). Use `--order N` to suggest a reading order when it differs from " +
	"commit order, and `--file <path> \"<note>\"` for context that isn't tied " +
	"to one commit. When individual hunks need context, use " +
	"`legato review annotate [sha] \"text\" --file <path> --hunk <1-based N>`; " +
	"add `--lines <start>-<end>` for a 1-based range inside that hunk. Inspect " +
	"`legato review show` or the diff to choose hunk and line indices.\n" +
	"- **Correct review metadata in place.** Use `legato review chapter edit <id> " +
	"[--title ...] [--narration ...] [--risk ...] [--order N] [--include path:hunk]` " +
	"or `legato review chapter remove <id>`. Use `legato review annotation edit <id> [--text ...] " +
	"[--risk ...] [--order N] [--file path --hunk N] [--lines start-end]` or " +
	"`legato review annotation remove <id>` for any ID returned by `review annotate`. Omitted " +
	"fields are preserved; use the documented `--clear-*` flags to clear optional " +
	"metadata. Prefer `restart` when the whole reading order is stale.\n" +
	"- **Signal when you're done.** Run `legato review ready \"<one-line summary>\"` " +
	"when your work is ready for human review.\n" +
	"- **Offer optional handoff context.** After clean commits and `review ready`, use " +
	"`legato review handoff template` for an editable JSON schema. Replace instructional text and source paths. Inspect `legato review show --json` " +
	"and `legato review handoff show --json`; bind the exact pass/base/head and latest " +
	"`expected_revision`, then run `legato review handoff submit --file <packet.json>`. " +
	"Explain behavior, decisions, risks, source links, and evidence limitations. Imported " +
	"results remain agent-reported, not proof. Never include secrets or human notes. " +
	"Handoff is optional and does not change ready or approval semantics.\n" +
	"- **Answer review questions.** Messages prefixed `[legato review]` are " +
	"reviewer questions about a specific step; each includes the exact safe stdin command " +
	"`legato review answer <step-id> --file - <<'LEGATO_ANSWER'` to reply with. Put the " +
	"Markdown answer on following lines and end it with `LEGATO_ANSWER`. Answer through that " +
	"command (not just chat) so the reply lands in the review record. Format review answers " +
	"as Markdown, using lists, inline code, and fenced code blocks where they improve clarity. " +
	"An answer written to a file and passed with `--file <path>` can show an image file " +
	"beside it the same way.\n" +
	"- **Replace stale tours instead of patching them.** After a rebase or " +
	"substantial rework, run `legato review restart --feedback \"<guidance>\"` to clear the selected pass " +
	"and retain its capture boundary, then author the replacement. Use `legato " +
	"review discard` when no replacement should be created. Both accept `--task` " +
	"and `--name`.\n" +
	"- **Inspect chapter data for automation.** Run `legato review chapters --json` " +
	"for ordered chapter metadata and hunk memberships, then `legato review chapter " +
	"show <chapter-id> --json` for one chapter and its selected structured diff.\n" +
	"\n" +
	"### Named review tours\n" +
	"\n" +
	"Every `legato review` verb (annotate, chapter, chapters, ready, show, sync, answer, restart, discard) " +
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
func (a *ChimeraAdapter) LaunchCommand(env map[string]string, brief, model, tier string) string {
	if env == nil {
		return ""
	}
	cmd := "chimera"
	policy := env["LEGATO_CHIMERA_SESSION_EXISTS"]
	if id := env["LEGATO_CHIMERA_SESSION_ID"]; policy == "resume" && id != "" {
		cmd += " --session " + shellQuote(id)
	} else {
		if name := env["LEGATO_CHIMERA_SESSION_NAME"]; name != "" {
			cmd += " --session-name " + shellQuote(name)
		}
		if id != "" {
			cmd += " --session-id " + shellQuote(id)
		}
		if policy != "" {
			cmd += " --session-exists " + shellQuote(policy)
		}
		if _, ok := env["LEGATO_ROLE_PROMPT_FILE"]; ok {
			cmd += ` --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`
		}
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

	cmd += a.mcpConfigFlag()

	// In Legato's sandbox Chimera cannot run its own: that is a Docker
	// container, and the Docker socket would undo Legato's sandbox.
	sandboxed := env["LEGATO_SANDBOXED"] == "1"
	for _, arg := range withoutDockerSandbox(a.launchArgs, sandboxed) {
		cmd += " " + shellQuote(arg)
	}
	if model != "" {
		cmd += " --model " + shellQuote(model)
	}
	if tier != "" {
		args, ok := a.tiers[tier]
		if !ok || len(args) == 0 {
			// See claude_code.go for why this branch is logged rather than
			// errored — config rotations can leave persisted sub-tasks
			// referencing tiers that no longer exist.
			log.Printf("warn: adapter %q has no tier %q configured; spawning with base launch_args only", a.Name(), tier)
		}
		for _, arg := range withoutDockerSandbox(args, sandboxed) {
			cmd += " " + shellQuote(arg)
		}
	}
	return cmd
}

// chimeraSandboxValued are Chimera's sandbox flags that take a value.
var chimeraSandboxValued = map[string]bool{"--sandbox-mode": true, "--sandbox-image": true, "--sandbox-network": true, "--sandbox-memory": true, "--sandbox-cpus": true}

// withoutDockerSandbox drops Chimera's --sandbox and --sandbox-* flags (with
// their values) when drop is set, and logs that it did.
func withoutDockerSandbox(args []string, drop bool) []string {
	if !drop {
		return args
	}
	out := make([]string, 0, len(args))
	dropped := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--sandbox" || strings.HasPrefix(arg, "--sandbox-") || arg == "--apply-on-success" {
			dropped = true
			if chimeraSandboxValued[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, arg)
	}
	if dropped {
		log.Printf("chimera: its own Docker sandbox is off inside Legato's sandbox")
	}
	return out
}

// launchArgsContainsMode reports whether the user-supplied launch args
// already include a `--mode` flag. When true, the adapter skips its
// automatic mode injection so the user override wins.
func launchArgsContainsMode(args []string) bool {
	return launchArgsContain(args, "--mode")
}

// launchArgsContain reports whether launch args already give flag, as
// `--flag value` or `--flag=value`.
func launchArgsContain(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
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
		content := chimeraAgentStateScript(a.legatoBin, s.event, s.activity)
		if s.event == "SessionCreated" {
			content = chimeraSessionCreatedScript(a.legatoBin)
		} else if s.event == "PermissionResolved" {
			content = chimeraPermissionResolvedScript(a.legatoBin)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	stalePostToolUse := filepath.Join(chimeraHookDir("PostToolUse"), "legato-post-tool-use.sh")
	if err := os.Remove(stalePostToolUse); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", stalePostToolUse, err)
	}
	return nil
}

func chimeraAgentStateScript(legatoBin, event, activity string) string {
	return fmt.Sprintf(`#!/bin/sh
# Generated by legato — do not edit manually.
# Called by Chimera's hook system to update agent activity state.

[ -z "$LEGATO_TASK_ID" ] && exit 0
[ -z "$LEGATO_EXECUTION_ID" ] && exit 1
[ -z "$CHIMERA_SESSION_ID" ] && exit 0

exec %s agent state "$LEGATO_TASK_ID" --execution-id "$LEGATO_EXECUTION_ID" --activity "%s" --working-dir "$PWD" --session-id "$CHIMERA_SESSION_ID" --event "%s"
`, legatoBin, activity, event)
}

func chimeraPermissionResolvedScript(legatoBin string) string {
	return fmt.Sprintf(`#!/bin/sh
# Generated by legato — do not edit manually.
[ -z "$LEGATO_TASK_ID" ] && exit 0
[ -z "$LEGATO_EXECUTION_ID" ] && exit 1
[ -z "$CHIMERA_SESSION_ID" ] && exit 0
exec %s agent permission-resolved "$LEGATO_TASK_ID" --execution-id "$LEGATO_EXECUTION_ID" --working-dir "$PWD" --session-id "$CHIMERA_SESSION_ID"
`, legatoBin)
}

func chimeraSessionCreatedScript(legatoBin string) string {
	return fmt.Sprintf(`#!/bin/sh
# Generated by legato — do not edit manually.
[ -z "$LEGATO_TASK_ID" ] && exit 0
[ -z "$CHIMERA_SESSION_ID" ] && exit 0
exec %s agent session-created "$LEGATO_TASK_ID" "$CHIMERA_SESSION_ID" --hook-payload
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
	stalePostToolUse := filepath.Join(chimeraHookDir("PostToolUse"), "legato-post-tool-use.sh")
	if err := os.Remove(stalePostToolUse); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", stalePostToolUse, err)
	}
	return nil
}

// chimeraHookDir returns the global Chimera hook directory for a given event.
// Chimera scans ~/.chimera/hooks/<EventName>/ on every event firing.
func chimeraHookDir(event string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chimera", "hooks", event)
}
