# Legato

## Agent instructions

- For all work in this project, use the tdd skill, if present: tests first.
- Never stop, kill, restart, or replace a running Legato process unless the user explicitly asks for that process action. A request to change, build, validate, or deploy Legato does not imply permission to disrupt a running process.
- Do not perform browser or end-to-end testing against a running Legato unless the user explicitly asks for it. Such tests are fine against scratch instances (their own config, database, data and runtime folders, tmux server and port) that do not disrupt running ones; stop them and remove their folders afterwards.

AI Agent Orchestration TUI — a keyboard-driven kanban board for tracking tasks, built for developers who work with AI coding agents. Supports local tasks and pluggable ticket providers (Jira first, others planned).

## Tech Stack

- **Language**: Go 1.25+
- **TUI**: Bubbletea, Lipgloss (styling), Glamour (markdown rendering)
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGO), `sqlx` for queries
- **Config**: YAML via `gopkg.in/yaml.v3`, XDG-compliant paths
- **Module path**: `github.com/cpave3/legato`

## Architecture

3-layer architecture with strict import rules:

```
cmd/legato/     → wires everything (imports all layers)
internal/tui/   → presentation (imports service/ via interfaces, never engine/)
internal/service/ → business logic (imports engine/, never tui/)
internal/engine/  → infrastructure (imports only stdlib + 3rd-party)
config/           → configuration (standalone, no internal imports)
```

Import rules are enforced by architecture tests. Engine packages must not import sibling engine packages, service, or TUI; service must never import TUI. Production database callers use store behavior rather than `Store.DB()`/`sqlx`, command entry points open databases through `openStore`, and filesystem state resolves from `config.ResolveDataDir` rather than the database path.

## Commands

```bash
task test                  # run all tests
task test:race             # run with race detector (use for event bus changes)
task test:cover            # run tests with coverage
task build                 # build the legato binary
task run                   # run the app
task vet                   # static analysis
task lint                  # golangci-lint
task check                 # build + test + vet + lint
```

## Build Plan

6-phase v0 plan in `openspec/changes/`:

1. ~~Engine Layer~~ (complete)
2. ~~Service Layer~~ (complete)
3. ~~TUI Shell~~ (complete)
4. ~~Jira Integration~~ (complete)
5. ~~Detail View & Clipboard~~ (complete)
6. ~~Polish~~ (complete) — overlays (search/move/help), error handling, server stub

Post-v0: Attention dashboard (complete) — durable generic registry with centrally allowed plan, review, PR, standalone-agent, workflow, and trigger projections; shared live reconciliation; grouped web dashboard; generic TUI/CLI/Stream Deck consumers. See `docs/ai/attention.md`.

## Detailed Documentation

Two docs apply to almost every change and are loaded automatically (`@path` lines are imported by agent tools that support them):

@docs/ai/packages.md
@docs/ai/dev-notes.md

The rest are not imported. Before changing an area, read its doc, and keep it current:

- `docs/ai/database.md` — store boundary, migrations, schema, execution activity
- `docs/ai/config.md` — every `config.yaml` setting and its validation
- `docs/ai/cli.md` — subcommands, host broker, JSON contract and exit codes, hook integrations (Claude Code, Codex, Chimera, Staccato), tmux status line
- `docs/ai/web-ui.md` — web UI pages, components, API endpoints, WebSocket sync, PWA, pairing
- `docs/ai/tui.md` — TUI theme, board, agents view, spawn overlay
- `docs/ai/sync.md` — ticket providers and the Jira sync algorithm
- `docs/ai/pr-tracking.md` — GitHub PR discovery, code context, read-only client
- `docs/ai/swarm.md` — conductor/worker orchestration, plans, tiers, adapter launch flags
- `docs/ai/attention.md` — Attention registry, producers, signal policy
- `docs/ai/approvals.md` — approval requests, tray, signed approvals and devices
- `docs/ai/credentials.md` — credential broker, rules, HTTP proxy, SSH agent, GitHub App, stores, audit log
- `docs/ai/learning.md` — Legato Learn ledger, curation workflow, prompt eligibility
- `docs/ai/claude-code-native.md` — Claude Code mod and bridge, permissions, scratch-instance setup
- `docs/ai/codex-native.md` — Codex app-server bridge and launcher
- `docs/ai/agent-mcp.md` — `legato mcp serve` tools, artifacts/cards, images, videos, studios, HTML sandbox
- `docs/ai/resources.md` — named file/folder resources given to agents
- `docs/ai/sandbox.md` — bubblewrap sandbox: what agents see and what is hidden
