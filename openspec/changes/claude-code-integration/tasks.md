## 1. IPC Socket (engine layer)

- [x] 1.1 Create `internal/engine/ipc/` package with socket path resolution (`XDG_RUNTIME_DIR` with `/tmp` fallback, `SocketPath()` function)
- [x] 1.2 Implement IPC server: `NewServer(socketPath, callback)` that listens on a Unix domain socket, accepts connections, reads newline-delimited JSON, calls the callback with parsed messages. Handle stale socket cleanup on startup.
- [x] 1.3 Implement IPC client: `Send(socketPath, message)` that connects to the socket, writes a JSON message, and disconnects. Return nil silently if socket doesn't exist or connection is refused.
- [x] 1.4 Define message types: `Message` struct with `Type`, `TaskID`, `Status`, `Content` fields. Support `task_update` and `task_note` message types.
- [x] 1.5 Write tests for server/client round-trip, stale socket cleanup, concurrent connections, and missing socket graceful handling.

## 2. AI Tool Adapter Interface (service layer)

- [x] 2.1 Create `internal/service/adapter.go` with `AIToolAdapter` interface (`Name()`, `InstallHooks()`, `UninstallHooks()`, `EnvVars()`) and adapter registry (`RegisterAdapter`, `GetAdapter`, `ListAdapters`)
- [x] 2.2 Write tests for adapter registry: register, lookup, lookup-missing, list

## 3. Tmux Environment Variable Injection (engine layer)

- [x] 3.1 Add `SetEnv(sessionName, key, value string) error` method to `tmux.Manager` using `tmux set-environment`
- [x] 3.2 Add `SetEnv` to `TmuxManager` interface in service layer
- [x] 3.3 Update `agentService.SpawnAgent` to accept optional env vars map and call `tmux.SetEnv` after spawning
- [x] 3.4 Write tests for SetEnv (mock tmux pattern) and updated SpawnAgent with env injection

## 4. Claude Code Adapter (engine + service layer)

- [x] 4.1 Create `internal/engine/hooks/` package with hook script templates for `Stop` and `TaskCompleted` events. Scripts check `LEGATO_TASK_ID`, parse stdin, call legato CLI with resolved binary path.
- [x] 4.2 Implement `claude_code.go` adapter: `InstallHooks` generates scripts to `.claude/hooks/legato-*.sh`, merges entries into `.claude/settings.json` (preserving existing hooks). `UninstallHooks` removes only Legato entries.
- [x] 4.3 Implement `EnvVars` returning `LEGATO_TASK_ID`, `LEGATO_SOCKET`, `LEGATO_PROJECT_DIR`
- [x] 4.4 Write tests: install creates correct files/settings, uninstall preserves user hooks, reinstall overwrites, missing `.claude/` errors gracefully

## 5. CLI Subcommand Dispatch (cmd layer)

- [x] 5.1 Refactor `cmd/legato/main.go` to detect subcommands from `os.Args`. No args → TUI (existing). Known subcommand → dispatch. Unknown → error with usage.
- [x] 5.2 Implement `legato task update <task-id> --status <status>`: load config+store, resolve status→column, update task in DB, send IPC notification, exit
- [x] 5.3 Implement `legato task note <task-id> <message>`: append note to task, send IPC notification, exit
- [x] 5.4 Implement `legato hooks install [--tool claude-code]`: look up adapter, call `InstallHooks(cwd)`, print result
- [x] 5.5 Implement `legato hooks uninstall [--tool claude-code]`: look up adapter, call `UninstallHooks(cwd)`, print result
- [x] 5.6 Write tests for subcommand dispatch (routing, unknown command error) and for each subcommand's happy/error paths

## 6. TUI Socket Integration (presentation layer)

- [x] 6.1 Start IPC server in `cmd/legato/main.go` before TUI launch, pass socket path to app. Defer server shutdown + socket cleanup.
- [x] 6.2 Wire IPC server callback to event bus: on `task_update` publish `EventCardUpdated`, on `task_note` publish `EventCardUpdated`. TUI already listens to these events and refreshes.
- [x] 6.3 Pass socket path and adapter to `AgentService` so `SpawnAgent` injects env vars into tmux sessions
- [x] 6.4 Test that IPC message → event bus → board refresh flow works end-to-end (model state test, not rendered output)

## 7. Wiring and Integration

- [x] 7.1 Register Claude Code adapter in `main.go` at startup
- [x] 7.2 Thread socket path through to `AgentService` → `SpawnAgent` → `tmux.SetEnv` calls
- [x] 7.3 End-to-end manual test: spawn agent, verify env vars set, simulate hook call via `legato task update`, confirm TUI board updates
