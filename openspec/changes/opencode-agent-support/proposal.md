## Why

Legato currently only supports Claude Code as an AI agent via its hook system. OpenCode is another AI coding agent with a growing user base and a plugin system that maps well to Legato's activity tracking model. Adding OpenCode support makes Legato useful to a wider audience and validates the `AIToolAdapter` abstraction we already built.

## What Changes

- New `OpenCodeAdapter` implementation of `AIToolAdapter` in `internal/engine/hooks/`
- Plugin generates a TypeScript file (`legato.ts`) instead of shell scripts — OpenCode plugins are in-process JS/TS modules using Bun
- Install writes to `~/.config/opencode/plugins/legato.ts` (global plugin directory), uninstall removes it
- Plugin listens for `session.status` events (busy→working, idle→waiting) and `session.deleted` (clear activity) via the OpenCode event hook
- Plugin reads `LEGATO_TASK_ID` from environment and calls `legato agent state` via Bun's `$` shell API
- CLI `legato hooks install --tool opencode` and `legato hooks uninstall --tool opencode` support

## Capabilities

### New Capabilities
- `opencode-plugin`: OpenCode adapter implementing AIToolAdapter — plugin file generation, install/uninstall, lifecycle event mapping to Legato activity states

### Modified Capabilities
- `legato-cli`: Add `opencode` as a valid `--tool` option for `hooks install`/`hooks uninstall` subcommands

## Impact

- **New file**: `internal/engine/hooks/opencode.go` — adapter implementation + plugin template
- **Modified**: `cmd/legato/main.go` — register OpenCode adapter in the adapter registry
- **Modified**: CLI hooks subcommand — accept `--tool opencode`
- **No new Go dependencies** — the TypeScript plugin file is generated as a string template (same pattern as Claude Code shell scripts)
- **No database changes** — uses existing `agent_sessions.activity` column and IPC broadcast
