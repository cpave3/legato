## 1. Store Layer — Aggregate Agent Counts

- [x] 1.1 Add `GetAgentActivityCounts(ctx, excludeTaskID string) (working int, waiting int, idle int, err error)` to `internal/engine/store/agent_session.go`. Single query: `SELECT activity, COUNT(*) FROM agent_sessions WHERE status='running' [AND task_id != ?] GROUP BY activity`. Map results to three counts (activity="working", "waiting", "" for idle). Test with real SQLite: multiple sessions in mixed states, exclude flag, no sessions.

## 2. Service Layer — Expose Summary

- [x] 2.1 Add `GetAgentSummary(ctx, excludeTaskID string) (working, waiting, idle int, err error)` to `AgentService` in `internal/service/agent.go`. Delegates to store's `GetAgentActivityCounts`. Calls `ReconcileSessions` first to clean up stale entries. Test with mock tmux + real SQLite.

## 3. CLI Subcommand — `agent summary`

- [x] 3.1 Add `AgentSummary(s *store.Store, excludeTaskID string) (string, error)` to `internal/cli/cli.go`. Calls `store.GetAgentActivityCounts`, formats output as tmux style markup string (e.g., `#[fg=green]2 working #[fg=yellow]·#[fg=yellow] 1 waiting #[fg=colour8]· 0 idle`). Omit zero-count states except idle. Test output format for various count combinations.
- [x] 3.2 Wire `agent summary [--exclude <task-id>]` subcommand in `cmd/legato/main.go`. Loads config + store only (no TUI/sync/tmux). Prints result to stdout and exits.

## 4. Tmux Status Line Injection at Spawn

- [x] 4.1 In `internal/service/agent.go` `SpawnAgent`, after session spawn and before user `tmux_options`, apply legato status line options: `status-right` set to `#(</absolute/path/legato> agent summary --exclude <taskID>)`, `status-interval` to `5`, and a `status-style` for minimal distinction. Resolve binary path via `os.Executable()` (store in `AgentServiceOptions` or resolve at service init). Test that options are applied in correct order (legato defaults before user overrides).
- [x] 4.2 Pass absolute binary path into `AgentServiceOptions` from `cmd/legato/main.go` (resolve once at startup via `os.Executable()`). Ensure the path is available for status-right formatting.

## 5. Documentation

- [x] 5.1 Update CLAUDE.md CLI subcommands section with `legato agent summary` and the tmux status line behavior.
