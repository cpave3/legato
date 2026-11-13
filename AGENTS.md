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
task docs:dev              # serve the docs site (site/) with live reload
task docs:build            # build the docs site; fails on a broken link
task desktop:test          # the desktop app's Rust tests and TypeScript check
task desktop:dev           # run the desktop app (desktop/)
task packages:check        # lint, typecheck and test the apps under packages/
task even-hub:dev          # the Even G2 glasses app (packages/even-hub/) on its fixture
task even-hub:sim          # the Even Hub simulator, open on it
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
- `docs/ai/reeve.md` — the Reeve: sessions, sandbox pinning, its folder and tokens, what it may call
- `docs/ai/settings.md` — the settings registry, settings kept in the database, pins and the effective configuration
- `docs/ai/cli.md` — subcommands, host broker, JSON contract and exit codes, hook integrations (Claude Code, Codex, Chimera, Staccato), tmux status line
- `docs/ai/web-ui.md` — web UI pages, components, API endpoints, WebSocket sync, PWA, pairing
- `docs/ai/tui.md` — TUI theme, board, agents view, spawn overlay
- `docs/ai/agent-states.md` — agent states (working, waiting, background, idle, error), what each agent tool's hooks and bridge report, background jobs, the gaps
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
- `docs/ai/integrations.md` — Argus: scoped tokens, owner lookup, delegations, replies, the OAuth connection, cases in Attention
- `docs/ai/resources.md` — named file/folder resources given to agents
- `docs/ai/engram.md` — engram sessions: the engram client, the setting and probe, bindings, the spawn as an engram
- `docs/ai/sandbox.md` — bubblewrap sandbox: what agents see and what is hidden
- `docs/ai/updates.md` — the version, the release workflow, and the self-updater
- `docs/ai/plans.md` — collaborative plans: storage, revisions, API, image copies
- `docs/ai/reviews.md` — review tours: capture, chapters, passes, plan and review lineage, the queue
- `docs/ai/review-handoff.md` — review handoff packets: API, storage, freshness, CLI output
- `docs/ai/workflows.md` — workflow runs, completion and artifact collection, API, live updates
- `docs/ai/model-settings.md` — native model settings: proxy, API, vision metadata, rollout
- `docs/ai/desktop.md` — the desktop app (`desktop/`, Tauri): window and webviews, pairing, pinned TLS, security keys, its releases and updater
- `docs/ai/even-hub.md` — the Even Realities G2 glasses app (`packages/even-hub/`, Even Hub SDK): the SDK's limits, the dev proxy and token, the simulator, packing; and the `packages/` workspace

## User documentation site

The documentation for people who use Legato is the Starlight site in `site/`, published to <https://cpave3.github.io/legato/> by `.github/workflows/docs.yml`. `site/README.md` is its page map and style guide. `docs/ai/` stays the reference for agents working on this code; the site never links into it except as "internals on GitHub".

- A change a user can see (a key, a setting, a command, a screen, a default) updates its site page in the same commit, as well as its `docs/ai/` doc.
- `site/src/content/docs/reference/keyboard.md` is generated from the hotkey registry: run `go test ./internal/tui/hotkey -update` after changing a binding.
- `site/src/content/docs/reference/api.md` is generated from the route table: run `go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update` after changing a route.
- `go test ./config` fails when a `config.yaml` setting is not named on `site/src/content/docs/reference/configuration.md`.
- The glossary page is written from `GLOSSARY.md` at build time; edit `GLOSSARY.md` only.

| Agent doc | Site pages with the same facts |
| --- | --- |
| `docs/ai/config.md` | `reference/configuration`, the feature page of each setting |
| `docs/ai/settings.md` | `reference/configuration` |
| `docs/ai/reeve.md` | `using/reeve` |
| `docs/ai/cli.md` | `reference/cli`, `agent-tools/*`, `integrations/staccato`, `integrations/tmux-status` |
| `docs/ai/tui.md` | `using/board`, `using/agents`, `start/tour` |
| `docs/ai/agent-states.md` | `agent-tools/claude-code`, `using/board`, `using/agents`, `using/reeve`, `start/tour` |
| `docs/ai/web-ui.md` | `using/web-ui`, `running/devices`, `security/web-auth` |
| `docs/ai/attention.md`, `docs/ai/approvals.md` | `using/attention` |
| `docs/ai/swarm.md` | `using/swarms` |
| `docs/ai/learning.md` | `using/learn` |
| `docs/ai/plans.md`, `docs/ai/reviews.md`, `docs/ai/review-handoff.md` | `using/plans`, `using/reviews`, `using/review-handoff` |
| `docs/ai/workflows.md` | `using/workflows` |
| `docs/ai/model-settings.md` | `agent-tools/model-settings` |
| `docs/ai/sync.md`, `docs/ai/pr-tracking.md` | `integrations/jira`, `integrations/github` |
| `docs/ai/claude-code-native.md`, `docs/ai/codex-native.md`, `docs/ai/agent-mcp.md`, `docs/ai/resources.md` | `agent-tools/*` |
| `docs/ai/credentials.md`, `docs/ai/sandbox.md` | `security/*`, `running/agent-server` |
| `docs/ai/engram.md` | `agent-tools/engrams` |
| `docs/ai/integrations.md` | `integrations/argus` |
| `docs/ai/updates.md` | `running/updates`, `start/install` |
| `docs/ai/desktop.md` | `running/desktop-app` |
| `docs/ai/even-hub.md` | `integrations/even-g2`, `contributing/build` |

## Agent skills

### Issue tracker

Issues, specs and wayfinder maps live in Burnlite, in the Project `LEG`, managed with the Burnlite MCP tools (or the `burnlite` CLI when they aren't connected). Not GitHub Issues: pull requests stay on GitHub. See `docs/agents/issue-tracker.md`.

### Triage labels

Burnlite Labels: `needs-triage`, `ready-for-agent`, `ready-for-human`, `wontfix`, plus `bug` and `enhancement`; `needs-info` is not used. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root, created only when first needed. See `docs/agents/domain.md`.
