## 1. OpenCode Adapter Implementation

- [ ] 1.1 Create `internal/engine/hooks/opencode.go` with `OpenCodeAdapter` struct implementing `AIToolAdapter` — `Name()` returns `"opencode"`, `EnvVars()` returns `LEGATO_TASK_ID` map, `openCodePluginDir()` helper resolves `$XDG_CONFIG_HOME/opencode/plugins/` with `~/.config` fallback
- [ ] 1.2 Implement `pluginSource(legatoBin)` function that returns the TypeScript plugin source string — event hook listening for `session.status` (busy→working, idle→waiting) and `session.deleted` (clear), Bun `$` shell calls to embedded legato binary path, `LEGATO_TASK_ID` guard at top
- [ ] 1.3 Implement `InstallHooks()` — create plugins directory if needed, write `legato.ts` with 0644 permissions
- [ ] 1.4 Implement `UninstallHooks()` — remove `legato.ts`, return nil if file doesn't exist

## 2. Adapter Registration

- [ ] 2.1 Register `OpenCodeAdapter` in `cmd/legato/main.go` adapter registry alongside existing Claude Code and Staccato adapters

## 3. Tests

- [ ] 3.1 Write unit tests in `internal/engine/hooks/opencode_test.go` — test `Name()`, `EnvVars()`, `InstallHooks()` (creates file, creates directory, overwrites existing), `UninstallHooks()` (removes file, no-op when missing), verify generated plugin source contains legato binary path and correct event handlers
