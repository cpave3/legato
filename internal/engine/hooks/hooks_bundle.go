package hooks

import (
	"fmt"
	"path/filepath"
	"slices"
)

// HooksBundleName names the plugin (Claude Code) that runs the operator's
// managed hooks in a session (spec LEG-687, "Per-execution delivery").
const HooksBundleName = "legato-hooks"

// HooksEntryGrace is how much longer than the longest time limit of its
// event Claude Code gives a runner entry, so the runner can end a hook that
// ran out of time and report it before Claude Code ends the runner.
const HooksEntryGrace = 15

// hooksInternalTimeout is the time limit of the two internal entries, which
// run no hook of the operator's.
const hooksInternalTimeout = 10

// The runner's internal events, which no manifest may name: the canary at
// the session's start, which tells Legato the hooks are live, and the turn
// reset at each prompt, which starts the stop-block count of a turn again.
const (
	HooksEventCanary    = "canary"
	HooksEventTurnReset = "turn_reset"
)

// claudeHookEvents maps the author events the Claude Code bundle delivers
// to Claude Code's hook events. Phase 1 delivers session_start and stop;
// pre_tool and post_tool are Phase 2.
var claudeHookEvents = map[string]string{
	"session_start": "SessionStart",
	"stop":          "Stop",
}

// HooksBundle makes root/legato-hooks a Claude Code plugin whose
// hooks/hooks.json calls the runner (`legato agent managed-hooks run`) at
// each event in events, mapped to the longest time limit in seconds of the
// hooks that run on it, and always at SessionStart (the canary) and
// UserPromptSubmit (the turn reset). It adds the plugin to
// CLAUDE_CODE_PLUGIN_DIRS and returns its folder, in which the caller lays
// out what the runner reads (see docs/ai/managed-hooks.md § The bundle). An
// event it cannot deliver is an error, and env is then left alone.
//
// An entry runs the runner and passes on what it printed only when it exits
// 0; any other end (no LEGATO_BIN, no binary, a legato without the runner,
// which answers an unknown command with exit 2, a panic, which exits 2 as
// well) is a pass with nothing printed. So the runner blocks with Claude
// Code's JSON decision on stdout and exit 0, never with exit 2.
func (a *ClaudeCodeAdapter) HooksBundle(root string, events map[string]int, env map[string]string) (string, error) {
	plugin := filepath.Join(root, HooksBundleName)
	entries := map[string][]map[string]any{
		"SessionStart":     {hooksEntry(HooksEventCanary, plugin, hooksInternalTimeout)},
		"UserPromptSubmit": {hooksEntry(HooksEventTurnReset, plugin, hooksInternalTimeout)},
	}
	names := make([]string, 0, len(events))
	for event := range events {
		names = append(names, event)
	}
	slices.Sort(names)
	for _, event := range names {
		claudeEvent, ok := claudeHookEvents[event]
		if !ok {
			return "", fmt.Errorf("claude code: the %s event is not delivered", event)
		}
		entries[claudeEvent] = append(entries[claudeEvent], hooksEntry(event, plugin, events[event]+HooksEntryGrace))
	}
	hooksFile := map[string]any{}
	for claudeEvent, list := range entries {
		hooksFile[claudeEvent] = []map[string]any{{"matcher": "", "hooks": list}}
	}
	manifest := map[string]any{
		"name":        HooksBundleName,
		"version":     "1.0.0",
		"description": "The managed hooks the operator keeps in Legato, run in this session by Legato's runner.",
		"author":      map[string]string{"name": "Legato"},
	}
	if err := writeManifest(filepath.Join(plugin, ".claude-plugin", "plugin.json"), manifest); err != nil {
		return "", err
	}
	if err := writeManifest(filepath.Join(plugin, "hooks", "hooks.json"), map[string]any{"hooks": hooksFile}); err != nil {
		return "", err
	}
	appendPathList(env, "CLAUDE_CODE_PLUGIN_DIRS", plugin)
	if env["LEGATO_BIN"] == "" && a.legatoBin != "" {
		env["LEGATO_BIN"] = a.legatoBin
	}
	return plugin, nil
}

// hooksEntry is a command hook that runs the runner for one event of the
// bundle at plugin. Its stderr is dropped: the runner reports through the
// broker, and Claude Code would show it only on a failure the entry turns
// into a pass.
func hooksEntry(event, plugin string, timeout int) map[string]any {
	command := `[ -n "$LEGATO_BIN" ] || { cat >/dev/null; exit 0; }; ` +
		`out=$("$LEGATO_BIN" agent managed-hooks run --tool claude-code --event ` + event + ` --bundle ` + shellQuote(plugin) + ` 2>/dev/null) || exit 0; ` +
		`printf '%s' "$out"; exit 0`
	return map[string]any{"type": "command", "command": command, "timeout": timeout}
}
