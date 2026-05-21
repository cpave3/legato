# Legato — Documentation Summary

**Legato** is a keyboard-driven kanban board TUI built for developers who work with AI coding agents. It lets users create and manage tasks locally, optionally syncing with Jira, and provides vim-style navigation across columns and cards. Features include full-screen markdown detail views, clipboard copy for AI context, agent session spawning with tmux, bidirectional Jira sync, and GitHub PR tracking — all backed by a headless service layer designed to support both a TUI client today and a web UI tomorrow.

## Architectural Layers

- **`cmd/legato/`** — Entry point that wires all layers together.
- **`internal/tui/`** — Presentation layer (Bubbletea, Lipgloss, Glamour). Consumes the service layer via Go interfaces; never imports engine directly.
- **`internal/service/`** — Business logic layer (BoardService, SyncService, AgentService). Imports engine/; never imports tui/.
- **`internal/engine/`** — Infrastructure layer (SQLite store, Jira REST client, event bus, tmux). Imports only stdlib and third-party packages.
- **`config/`** — Standalone YAML configuration parser with XDG-compliant paths.

## docs/claude/ — Table of Contents

| File | What it covers |
|------|----------------|
| `packages.md` | Key packages and their responsibilities — store, macros, events, server, provider interface, and TUI models. |
| `database.md` | SQLite database location, embedded migrations via `embed.FS`, WAL mode, and foreign keys. |
| `config.md` | Config file resolution order, default behaviour when missing, and environment variable expansion in YAML values. |
| `sync.md` | The provider architecture: `TicketProvider` interface with Jira as the first implementation, and how others (Linear, GitHub Issues) can be plugged in. |
| `dev-notes.md` | Testing conventions — real SQLite databases in temp directories, real channel-based event bus, no mocks for storage or pub/sub. |
| `pr-tracking.md` | GitHub PR tracking orthogonal to ticket providers, using the `gh` CLI for read-only enrichment with graceful degradation. |
| `cli.md` | CLI subcommand dispatch (`task`, `agent`, `hooks`, etc.) alongside the default TUI launcher. |
| `web-ui.md` | Remote web interface built with React 19 + Vite + TailwindCSS, embedded in the Go binary, auto-starting alongside the TUI when enabled. |
| `swarm.md` | Conductor-driven swarm orchestration: a single LLM conductor drafts plans and dispatches ephemeral worker agents. |
