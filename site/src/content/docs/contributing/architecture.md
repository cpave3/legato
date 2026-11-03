---
title: Architecture
description: How Legato's Go code is laid out, the rules between its layers, and where to read more.
sidebar:
  order: 20
---

Legato is one Go binary. It holds the TUI, the web server with the React web UI built into it, and the command line that agents and hooks call. This page is a map for contributors: the layers, the main packages, the data, and where the detailed notes live.

## Layers

The Go code has three layers and a configuration package. Each layer imports only the layers below it:

```text
cmd/legato/          wires everything together; imports every layer
  internal/tui/        presentation: the Bubble Tea TUI
  internal/server/     presentation: HTTP and WebSocket API for the web UI
  internal/cli/        the handlers of the legato subcommands
    internal/service/    business logic
      internal/engine/     infrastructure: SQLite, tmux, Jira, GitHub, the event bus, ...
config/              configuration; stands alone (it shares one type with internal/engine/macros)
```

The rules:

- `internal/engine/` packages import only the standard library and third-party modules. An engine package never imports another engine package, `internal/service/` or `internal/tui/`. When two engine packages need the same thing, the service layer joins them.
- `internal/service/` imports `internal/engine/` and never `internal/tui/`.
- `internal/tui/` reaches the logic through service interfaces. In practice it also uses a few engine value types, such as the event bus and store records.
- `internal/setup/` holds the first-run setup wizard.

Tests enforce the boundary rules in [`internal/engine/store/architecture_test.go`](https://github.com/cpave3/legato/blob/main/internal/engine/store/architecture_test.go):

- No engine package imports a sibling engine package.
- Production code in `cmd/`, `internal/cli/`, `internal/service/`, `internal/setup/`, `internal/tui/` and `internal/server/` does not import `sqlx` or call `Store.DB()`. It uses the store's own methods.
- Commands in `cmd/legato/` open the database only through `openStore` in `database.go`.

Code that keeps files, such as certificates and tokens, finds their folder with `config.ResolveDataDir`, never from the database path. The data folder is the database's folder unless `data_dir` moves it.

## Main packages

The full list is in [`docs/ai/packages.md`](https://github.com/cpave3/legato/blob/main/docs/ai/packages.md). The main ones:

**Engine** (`internal/engine/`)

- `store`: the persistence boundary. It opens SQLite, runs the embedded migrations, and owns every query.
- `events`: the in-process event bus.
- `tmux`: starts, captures and attaches to the tmux sessions agents run in.
- `jira` and `github`: the Jira REST client and provider, and a read-only GitHub client.
- `hooks`: the adapters for agent tools (Claude Code, Codex, Chimera, Staccato). They write hook scripts and build the command that launches each tool.
- `claudebridge` and `codexbridge`: per-session bridges that serve a Claude Code or Codex session to Legato's native agent panel.
- `ipc` and `broker`: Unix sockets between Legato processes. CLI commands tell running instances about changes over `ipc`; agent-side processes in the sandbox reach the host Legato through the `broker`.
- `sandbox`, `credential`, `gitrepo`, `mcpserver`, `workflow`, `prompt`, `swarm`: the bubblewrap sandbox, the credential broker's parts, git for the repository cache, the MCP server, workflow definitions, reading prompts from terminal output, and swarm scope checks.

**Service** (`internal/service/`)

The services hold the behaviour: `BoardService` for columns and cards, `SyncService` for ticket providers, `AgentService` for agent sessions, and services for plans, reviews, workflows, swarms, Attention, approvals, credentials, repositories, Legato Learn, reports and updates. The TUI, the web server and the CLI all call the same services.

**Presentation**

- `internal/tui/`: the root Bubble Tea model, its views (board, detail, agents, Attention and more), the overlays, and the hotkey registry in `internal/tui/hotkey/` that drives dispatch, help and the [keyboard reference](/legato/reference/keyboard/).
- `internal/server/`: the HTTP API, the WebSocket, terminal streaming for agents, and the embedded web UI.
- `internal/cli/`: what each `legato` subcommand does. `cmd/legato/command_tree.go` declares the commands, their flags and their help.

## Data

Legato keeps its state in one SQLite database, through `modernc.org/sqlite`, a SQLite written in Go, so the build needs no C. Queries use `sqlx` inside the store.

- The database is at `~/.local/share/legato/legato.db` by default (`$XDG_DATA_HOME/legato/` when set), or at `db.path`.
- Migrations are SQL files embedded from `internal/engine/store/migrations/`, numbered in order. A `schema_migrations` table records which have run. Migrations only go forward. Legato refuses a database whose schema is newer than the binary.
- Each pooled connection turns on foreign keys and a 5-second busy timeout, so several Legato processes can share one database.
- A task is one row in `tasks`. A task from a ticket provider has a provider, a remote ID and provider metadata; a local task has none.

See [`docs/ai/database.md`](https://github.com/cpave3/legato/blob/main/docs/ai/database.md) for the schema and the reporting model.

## Events

`internal/engine/events` is a publish and subscribe bus on Go channels. Each subscriber gets a channel with a buffer of 64 events. `Publish` never blocks: when a subscriber's buffer is full, it misses that event. Subscribers must therefore treat an event as a prompt to reload, not as the only copy of a change.

Services publish events when cards, agents, plans, workflows or Attention items change. The TUI turns them into Bubble Tea messages. The web server turns them into WebSocket messages, such as `cards_changed`, that tell the web UI to fetch again.

Run `task test:race` when you change the bus or its subscribers.

## Adding a ticket provider

The sync service never imports Jira. It works through the `TicketProvider` interface in [`internal/service/provider.go`](https://github.com/cpave3/legato/blob/main/internal/service/provider.go), which has four methods:

- `Search(ctx, query)`: tickets that match a provider-specific query, such as JQL.
- `GetTicket(ctx, id)`: one ticket in full.
- `ListTransitions(ctx, id)`: the status changes a ticket allows.
- `DoTransition(ctx, id, transitionID)`: make one.

A provider returns tickets as `RemoteTicket`, with the description already in Markdown. To add one:

1. Write its client and a provider type in a new package under `internal/engine/`.
2. Add its settings to `config/`.
3. Construct it in `cmd/legato/main.go`, where the TUI and `legato serve` each build the Jira provider, and pass it to `NewSyncService`.

Test the sync logic with a mock provider, as the existing sync service tests do, and the client with `net/http/httptest`. See [`docs/ai/sync.md`](https://github.com/cpave3/legato/blob/main/docs/ai/sync.md) for the sync algorithm.

## The web UI in the binary

The web UI is a React and Vite app in `web/`. `task web:build` builds it into `internal/server/static/dist/`, and `internal/server/static` embeds that folder with `go:embed`. The build also writes a `build-id` file; open web pages compare it on reconnect and reload when the web UI changed.

A binary built without the web UI still serves the API. It leaves out the page routes and reports its build identity as `frontend-unavailable`. See [Build from source](/legato/contributing/build/).

## Testing conventions

- Storage tests use real SQLite files in `t.TempDir()`, not mocks.
- Event bus tests use real channels.
- Configuration tests isolate environment variables with `t.Setenv()`.
- TUI tests check the model's state after `Update()`, not the rendered text.
- Tests that need tmux skip when it is not installed.

More notes are in [`docs/ai/dev-notes.md`](https://github.com/cpave3/legato/blob/main/docs/ai/dev-notes.md).

## Where to read more

- [`docs/ai/`](https://github.com/cpave3/legato/tree/main/docs/ai) holds the internals, one file to an area: the database, the CLI, the web UI, the TUI, sync, swarms, Attention, approvals, credentials, the sandbox, updates and more. Read the file for an area before you change it, and keep it current.
- [`AGENTS.md`](https://github.com/cpave3/legato/blob/main/AGENTS.md) (also `CLAUDE.md`) tells coding agents how to work on the repository: tests first, the commands, and the rules about running Legato processes.
- [`openspec/`](https://github.com/cpave3/legato/tree/main/openspec) holds change proposals, with their designs, specs and task lists.
- [`GLOSSARY.md`](https://github.com/cpave3/legato/blob/main/GLOSSARY.md) defines Legato's words. The [glossary page](/legato/reference/glossary/) is built from it.
