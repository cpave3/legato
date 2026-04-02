# Legato

for all work in this project, use the tdd skill, if present.

AI Agent Orchestration TUI — a keyboard-driven kanban board for tracking tasks, built for developers who work with AI coding agents. Supports local tasks and pluggable ticket providers (Jira first, others planned).

## Tech Stack

- **Language**: Go 1.23+
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

**Import rules are enforced by convention, not tooling.** engine/ must never import service/ or tui/. service/ must never import tui/.

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

## Detailed Documentation

@docs/claude/packages.md
@docs/claude/database.md
@docs/claude/config.md
@docs/claude/sync.md
@docs/claude/dev-notes.md
@docs/claude/pr-tracking.md
@docs/claude/cli.md
@docs/claude/web-ui.md
