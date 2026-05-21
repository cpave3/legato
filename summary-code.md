# Legato Code Layout Summary

Legato is an AI-agent orchestration TUI built in Go with a strict three-layer architecture. The entry point in `cmd/legato/main.go` parses subcommands (`task`, `agent`, `hooks`, `serve`, `auth`, `pair`, `swarm`) and falls back to an interactive Bubbletea TUI. It wires configuration, SQLite, the event bus, every service, IPC, an optional web server, and the TUI app together.

## Engine Layer (`internal/engine/`)

Pure infrastructure — no imports of `service/` or `tui/`.

- **`analytics/`** — Aggregated working/waiting duration queries and daily/task-level rollup.
- **`auth/`** — Web UI bearer-token lifecycle: auto-generate on first run, read, regenerate.
- **`certs/`** — Self-signed ECDSA CA + server certificate generation with SAN auto-detection.
- **`events/`** — Buffered pub/sub event bus with typed events (sync, agent, PR, swarm, etc.).
- **`github/`** — PR status client via the `gh` CLI with batch fetching and repo/branch detection.
- **`hooks/`** — AI-tool hook adapters (Claude Code, Chimera, Staccato) that install shell scripts and generate launch commands with role-prompt injection.
- **`ipc/`** — Unix-domain-socket IPC for CLI-to-TUI/server communication (JSON messages, PID-based sockets).
- **`jira/`** — Jira REST client (v3) with Basic Auth, backoff, and ADF-to-Markdown conversion.
- **`macros/`** — Shared `Macro` struct and JSON list types used by config and API.
- **`prompt/`** — Terminal-output classifier that detects approval, plan, and free-text prompt states.
- **`store/`** — SQLite persistence with embedded migrations: tasks, columns, mappings, agent sessions, sync logs, state intervals, and swarm sub-tasks.
- **`swarm/`** — File-scope detection (`MatchScope`, `ScopeOverlaps`) and plan validation using `doublestar` globs.
- **`tmux/`** — tmux session manager: spawn, kill, capture-pane, attach, send-keys, list-panes, and pane-command introspection.

## Service Layer (`internal/service/`)

Business logic — imports `engine/` only, never `tui/`.

- **`BoardService`** — Kanban CRUD (columns, cards, workspaces), archiving, and search.
- **`SyncService`** — Jira pull/push with conflict resolution, remote ticket search/import.
- **`AgentService`** — tmux agent lifecycle, SQLite tracking, dynamic pane-command population, state-duration queries, and per-task sparkline data.
- **`PRTrackingService`** — Branch-to-task linking and periodic GitHub PR status polling.
- **`ReportService`** — Time-spent reports by day, task, workspace, swarm, and working directory.
- **`SwarmService`** — Conductor-driven orchestration including plan approval, dispatch, messaging, worker progress/question/built, and snapshot caching.
- **`Context` / `TierCatalog` / `Workspace`** — Supporting utilities for export formatting, tier descriptions, and workspace seeding.

## TUI Layer (`internal/tui/`)

Presentation — imports `service/` through interfaces, never `engine/` directly.

- **`app.go`** — Root Bubbletea model, view routing (board, detail, agents, report), and event-bus bridging.
- **`agents/`** — Split-view agent list + live terminal output with capture-pane polling.
- **`board/`** — Vim-navigable kanban board with card/column rendering.
- **`detail/`** — Full-screen Glamour-rendered markdown detail with metadata and editor spawning.
- **`clipboard/`** — Cross-platform clipboard and browser-open helpers.
- **`overlay/`** — All overlays: agent actions, archive, create, delete, help, import, link PR, macro picker, move, plan approval, search, swarm init, title edit, and workspace selection.
- **`report/`** — Report rendering view.
- **`statusbar/`** — Bottom bar showing sync state, key hints, and transient errors.
- **`theme/`** — Lipgloss color palette, style constants, and unicode/nerdfont icon sets.

## Import Rules

The architecture enforces a strict downward-pointing dependency graph:

- **`cmd/legato/`** may import all layers.
- **`internal/engine/`** may import only stdlib and third-party packages — **never** `service/` or `tui/`.
- **`internal/service/`** may import `engine/` — **never** `tui/`.
- **`internal/tui/`** may import `service/` (via interfaces) — **never** `engine/` directly.
