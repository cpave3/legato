## Context

Legato tracks AI agent activity states (working/waiting/idle) via the `AIToolAdapter` interface. Claude Code is the first implementation — it generates shell scripts and merges them into `.claude/settings.json`. The adapter pattern was designed to be pluggable, and OpenCode is the second agent to support.

OpenCode uses a fundamentally different integration model: in-process TypeScript plugins (run via Bun) rather than external shell script hooks. Plugins receive lifecycle events through an event bus and execute commands via Bun's `$` shell API. The plugin file lives at `~/.config/opencode/plugins/` (global) or `.opencode/plugins/` (project-level).

## Goals / Non-Goals

**Goals:**
- Implement `OpenCodeAdapter` that generates a TypeScript plugin file for OpenCode
- Map OpenCode lifecycle events to Legato activity states (busy→working, idle→waiting, deleted→clear)
- Support install/uninstall via `legato hooks install --tool opencode` / `legato hooks uninstall --tool opencode`
- Register the adapter at startup so it's available for agent spawning

**Non-Goals:**
- Publishing the plugin as an npm package (file-drop is sufficient for now)
- Supporting OpenCode-specific features beyond activity tracking (custom tools, permission hooks, etc.)
- Project-level plugin installation (global is simpler and sufficient)
- Config file changes — OpenCode auto-discovers plugins from the plugins directory

## Decisions

### 1. Global plugin directory over project-level

Write to `~/.config/opencode/plugins/legato.ts` rather than `.opencode/plugins/`.

**Rationale**: Claude Code hooks are per-project because `.claude/settings.json` is project-scoped. OpenCode plugins work globally — one install covers all projects. The plugin checks `LEGATO_TASK_ID` at startup and no-ops when not in a Legato tmux session, so a global install is safe.

**Alternative**: Project-level `.opencode/plugins/` — rejected because it would require installing per-project like Claude Code, and the plugin only activates when `LEGATO_TASK_ID` is set anyway.

### 2. `session.status` event over `chat.message` hook

Use the `event` hook listening for `session.status` (busy/idle) rather than the `chat.message` hook for "working" detection.

**Rationale**: `session.status` with `type: "busy"` fires on every processing cycle (including tool use continuations), not just user messages. This is closer to Claude Code's `UserPromptSubmit` + `PostToolUse` combined behavior. `chat.message` only fires when the user explicitly submits a prompt.

### 3. Bun `$` shell for CLI invocation

The generated plugin uses Bun's `$` template literal shell API to call `legato agent state`.

**Rationale**: OpenCode runs on Bun, and `$` is available in the plugin context. It's the idiomatic way to run commands from OpenCode plugins. No need for Node.js `child_process`.

### 4. `InstallHooks` ignores `projectDir` parameter

The `projectDir` parameter from the `AIToolAdapter` interface is unused for OpenCode since the plugin is global.

**Rationale**: The interface was designed around Claude Code's project-scoped model. OpenCode's global plugin directory doesn't need a project path. The adapter accepts the parameter for interface compliance but doesn't use it. `UninstallHooks` similarly ignores it.

### 5. Embed legato binary path in plugin

Same pattern as Claude Code — resolve `os.Executable()` at install time and embed the absolute path in the generated plugin file.

**Rationale**: tmux sessions may not have legato on PATH. Consistent with the existing adapter pattern.

## Risks / Trade-offs

- **OpenCode plugin API stability**: OpenCode is newer than Claude Code and its plugin API may change. → Mitigation: The plugin uses only stable hooks (`event` and basic lifecycle events). Keep the plugin minimal.
- **Bun dependency**: The plugin assumes Bun's `$` shell API is available. → Mitigation: This is guaranteed by OpenCode's plugin runtime — OpenCode itself runs on Bun.
- **`session.deleted` reliability**: If OpenCode doesn't fire `session.deleted` on all exit paths (e.g., crash), the activity state may not clear. → Mitigation: Legato's `ReconcileSessions` already handles orphaned agent states when tmux sessions die. This is the same recovery path used for Claude Code.
- **No XDG override for OpenCode config**: We hardcode `~/.config/opencode/plugins/` without checking `$XDG_CONFIG_HOME`. → Mitigation: Follow OpenCode's own convention — check `$XDG_CONFIG_HOME` with `~/.config` fallback, same pattern as `StaccatoAdapter`.
