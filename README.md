# Legato

A keyboard-driven kanban board TUI for tracking tasks, built for developers who work with AI coding agents. Supports local tasks and pluggable ticket providers (Jira first, others planned).

## Features

See [native model settings](docs/model-settings.md) for thinking, reasoning effort, and generation controls in the agent panel.

- **Local-first**: create and manage tasks locally, optionally sync with Jira
- **Vim-style navigation** (h/j/k/l) across columns and cards
- **Full-screen detail view** with Glamour-rendered markdown
- **Copy context to clipboard** for AI coding agents (`y` description, `Y` full context)
- **Move cards** between columns via overlay (`m`)
- **Create tasks** inline (`n`) with title, description, column, and priority
- **Edit local tasks** — rename title (`t` in detail) or edit description in `$EDITOR` (`e` in detail)
- **Delete tasks** with confirmation (`d` from board, `D` from detail)
- **Import remote tickets** — search Jira and pull individual tickets (`i`), inherits active workspace
- **Agent sessions** — spawn tmux sessions per task, track active agents on cards
- **AI coding agent integration** — hooks report agent activity (working/waiting) for Claude Code, Chimera, and Codex back to the board in real-time
- **GitHub PR tracking** — link PRs to tasks, see CI/review/comment status on cards (`p` to link, `o` to open with picker when both Jira and PR URLs exist, polls via `gh` CLI)
- **Staccato integration** — auto-links repo+branch when creating PRs via staccato (`legato hooks install --tool staccato`)
- **Yggdrasil worktrees** — create a task branch/worktree with `b` and automatically use it when spawning task agents
- **Pluggable AI tool adapters** — abstract interface for tool integrations (Claude Code, Chimera, Codex, Staccato)
- **Bidirectional Jira sync**: pull tickets, push card moves as transitions
- **Offline-first**: works from local SQLite when the network is down
- **Conflict resolution**: local moves win within a 5-minute window
- **Setup wizard**: first-run experience seeds columns and optionally configures Jira
- **Provider icons**: visual indicators for Jira/GitHub/local/agent on cards
- **Nerd Fonts support**: `icons: nerdfonts` in config for glyph icons
- **Provider-agnostic architecture**: swap Jira for Linear, GitHub Issues, etc.

## Yggdrasil worktrees

Set `worktrees.yggdrasil.enabled: true` and ensure `yg` is on `PATH`. Select a task and press `b` to choose the primary repository, worktree branch, and base branch. Legato remembers the resulting path and preselects it when spawning agents for that task.

For worktrees created through other Yggdrasil invocations, install the project hook from the repository containing `.yggdrasil.toml`:

```bash
legato hooks install --tool yggdrasil
```

The managed `post_create` hook preserves unrelated hooks, does nothing when `legato` is not on `PATH` or `LEGATO_TASK_ID` is unset, and records Yggdrasil's `YG_*` metadata when invoked with a task ID. Remove only Legato's managed callback with `legato hooks uninstall --tool yggdrasil`.

## Install

Legato must be built from source — the web dashboard is a React frontend that gets embedded into the Go binary, so `go install` from a remote module cannot include it.

### Prerequisites

- **Go 1.23+**
- **Task** — [taskfile.dev](https://taskfile.dev)
- **tmux** (optional) — required only for spawning agents from the board
- **pnpm** (optional) — required only if you want the web dashboard

### TUI only

```bash
git clone https://github.com/cpave3/legato.git
cd legato
task build
```

Install a copy to `$GOPATH/bin`:

```bash
task install
```

For development, build and symlink this checkout's binary into `$GOBIN` (or `$GOPATH/bin` when `GOBIN` is unset):

```bash
task link
```

Subsequent `task build` runs update the linked binary in place.

### TUI + Web Dashboard

The web dashboard (and PWA) is a React/Vite app in `web/`. It must be compiled before the Go binary is built so the assets can be embedded:

```bash
git clone https://github.com/cpave3/legato.git
cd legato

# Build the frontend into internal/server/static/dist/
cd web && pnpm install && pnpm run build && cd ..

# Build the Go binary with embedded assets
task build
```

Or use the combined Task target:

```bash
task build:full
```

**Without the frontend build**, `legato serve` only serves API endpoints — the SPA fallback route is omitted when no assets are present. The TUI works identically either way.

### Development hot reload

Build the stable repository binary once, then launch either foreground mode through the supervisor:

```bash
task build
task run:hot

# Or run only the web server (LEGATO_SERVER_PORT defaults to 3080).
task server:hot
```

`./legato --hot-reload` supervises the normal TUI, including its configured in-process web server. `./legato --hot-reload serve --port 3080` supervises standalone serve mode. Other CLI commands reject the flag because they are short-lived.

The original process watches the exact executable that launched it. After a successful `task build`, it asks the old child to shut down, waits for database, IPC, HTTP, and terminal cleanup, then starts exactly one child from the replacement binary with the same arguments, environment, working directory, and terminal. There is a brief disconnect and transient TUI navigation state is reset. Failed or incomplete builds leave the healthy child running. If graceful TUI shutdown stalls, Bubble Tea is told to restore the terminal and stop; if the process still exceeds the supervisor deadline, it is force-stopped and reaped before the replacement starts, so children never overlap or retain terminal ownership.

Use `task build:full` when frontend changes must be included. The build emits a new embedded frontend identity; connected web/PWA clients detect it on reconnect, update the service worker, and reload once. A Go-only `task build` retains the current embedded web identity and does not reload same-build browser tabs.

When the active server is a hot-reload child launched from a Legato checkout, Settings shows **Rebuild and hot reload**. This authenticated action starts `task build:full` in the checkout, allowing a phone or other remote web client to request the same rebuild without terminal access. The button confirms when the build starts; output is written to `.legato/run/hot-reload-build.log`, and concurrent requests are rejected until that build exits. The action is absent from normal, installed, or non-checkout servers.

Hot reload is a foreground Unix development workflow, not zero-downtime deployment or daemon management. `task server:restart` remains the explicit detached standalone-server helper.

## Setup

On first launch with an empty database, Legato runs an interactive setup wizard that:

1. Seeds default board columns (Backlog, Ready, Doing, Review, Done)
2. Optionally configures Jira (credentials, project selection, status auto-mapping)

To set up manually or reconfigure later:

### 1. Create a Jira API Token

Go to https://id.atlassian.com/manage-profile/security/api-tokens and generate a new token.

### 2. Set the Token as an Environment Variable

Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):

```bash
export LEGATO_JIRA_TOKEN=your-api-token-here
```

### 3. Create the Config File

Create `~/.config/legato/config.yaml`:

```yaml
jira:
  base_url: https://yourcompany.atlassian.net
  email: you@company.com
  api_token: ${LEGATO_JIRA_TOKEN}
  project_keys:
    - PROJ
  jql_filter: ""                # optional: additional JQL filter
  sync_interval_seconds: 60     # how often to pull from Jira
  status_sync_enabled: true     # false: refresh tickets without aligning board/Jira statuses

worktrees:
  yggdrasil:
    enabled: false              # enable task worktree creation with `b`

groups:
  defaults: [backend, urgent]

board:
  columns:
    - name: Backlog
      remote_statuses: ["To Do", "Open", "Backlog"]
    - name: Ready
      remote_statuses: ["Ready for Dev", "Selected for Development"]
    - name: Doing
      remote_statuses: ["In Progress", "In Development"]
      remote_transition_id: "21"   # transition ID to move issues into this status
    - name: Review
      remote_statuses: ["In Review"]
    - name: Done
      remote_statuses: ["Done", "Closed"]

icons: unicode    # "unicode" (default) or "nerdfonts" for Nerd Font glyphs
editor: ""        # optional: override $VISUAL/$EDITOR for description editing (e.g. "nvim", "code --wait")
```

The `${LEGATO_JIRA_TOKEN}` reference is expanded at load time so the token never lives in the config file as plaintext.

You can also run Legato without any Jira config for a fully local kanban board.

### Config File Location

Legato looks for config in this order:

1. `$LEGATO_CONFIG` (env var override)
2. `$XDG_CONFIG_HOME/legato/config.yaml`
3. `~/.config/legato/config.yaml`

## Macros

Macros are named snippets you can fire into an agent from the TUI or web UI. In the TUI, a trailing `\n` in the `keys` field sends Enter to the tmux pane.

### Why macros

- Run the same command across multiple agents without retyping
- Give agents natural-language prompts they execute inline
- Broadcast quick corrections or status checks to visible panes

### Configuring macros

Add a top-level `macros:` list to your config file:

```yaml
macros:
  - name: "run tests"
    keys: "task test\n"
  - name: "commit changes"
    keys: "Please commit all current changes with a descriptive semantic commit message.\n"
```

- `name` — label shown in the macro picker / web dropdown
- `keys` — text executed by the active surface's normal macro transport

On the web Agents view, with no input, textarea, select, or content-editable field focused, press `M` to open the active agent's macro menu. Press `1`–`9` to run the corresponding first nine macros in configured list order; later macros remain available by clicking them. Native Chimera agents preserve bang handling for leading `!` macros and use `agent_message` for ordinary macros, while tmux-backed web agents submit macros through their normal message transport. The TUI continues to send macro keys directly to tmux.

No env-var expansion or placeholder substitution — strings are used exactly as written.

## Claude Code Integration

Legato integrates with Claude Code via hooks to show real-time agent activity on your board cards.

### Setup

```bash
# Install hooks in your project (requires .claude/ directory)
legato hooks install
```

This generates hook scripts in `.claude/hooks/` and registers them in `.claude/settings.json`.

### How it works

When you spawn an agent from Legato (`a` on a card), the tmux session gets a `LEGATO_TASK_ID` environment variable injected. Claude Code hooks fire on lifecycle events and update the card:

| Card indicator | Meaning | Triggered by |
|---|---|---|
| `⠹ running` (green) | Claude is actively working | `UserPromptSubmit` / `PostToolUse` hooks |
| `◆ needs you` (amber) | Claude needs your approval | `PermissionRequest` hook |
| `◐ background` (cyan) | The turn ended while a shell or subagent it started still runs | `Stop` hook |
| `○ idle` (dim) | Agent session alive, not working | `Stop` hook / default |

Hooks only fire inside Legato-spawned tmux sessions — they're no-ops elsewhere.

Multiple Legato instances can run simultaneously. Each instance creates its own IPC socket, and CLI commands broadcast to all of them — every open board updates in real-time.

### Uninstall

```bash
legato hooks uninstall
```

Removes only Legato's hooks; your other Claude Code hooks are preserved.

## CLI

Legato also exposes CLI subcommands for scripting and tool integration:

```bash
legato                                           # launch TUI (default)
legato task show <task-id> [--format description|full|json]  # print task context
legato task update <task-id> --status <column>   # move task to column
legato task note <task-id> <message>             # append note to task
legato agent state <task-id> --activity working  # set agent activity state
legato agent permission-resolved [<task-id>]      # map Chimera hook JSON from stdin
legato task link <task-id> [--branch <b>] [--repo <o/r>]  # link PR branch
legato task unlink <task-id>                     # remove PR link
legato columns sync                              # sync config columns to the board
legato hooks install [--tool claude-code|staccato|chimera|codex]       # install hooks
legato hooks uninstall [--tool claude-code|staccato|chimera|codex]     # remove hooks
```

`legato columns sync` makes the database-backed board match `board.columns` in
the active config file. Tasks in removed columns move to `Unmapped` when that
column is configured, or to the first configured column otherwise.
Columns with no `remote_statuses` are local-only: remote sync refreshes their
task details but does not move cards out of those columns.

Set `jira.status_sync_enabled: false` to keep pulling Jira ticket details while
disabling status changes in both directions. Existing cards keep their Legato
columns, and moving a card does not transition its Jira issue. Manual imports
still choose an initial column from the issue's current Jira status.

## Usage

```bash
legato    # launch the TUI
```

### Keyboard Shortcuts

#### Board View

| Key | Action |
|-----|--------|
| `h` / `l` | Move between columns |
| `j` / `k` | Move between cards |
| `g` / `G` | Jump to first/last card |
| `1`-`5` | Jump to column by number |
| `z` | Open or collapse the columns that have no cards |
| `enter` | Open task detail view |
| `m` | Move card to another column |
| `n` | Create new task |
| `d` | Delete task |
| `p` | Link PR to task |
| `i` | Import remote ticket |
| `w` | Switch workspace |
| `X` | Archive done cards |
| `/` | Search/filter tasks |
| `a` / `t` | Spawn agent (tmux) on selected card |
| `A` | Switch to agent view |
| `r` | Refresh (sync Jira + poll PRs) |
| `?` | Help overlay |
| `q` | Quit |

#### Detail View

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll line by line |
| `d` / `u` | Scroll half page |
| `g` / `G` | Jump to top/bottom |
| `y` | Copy description to clipboard |
| `Y` | Copy full context to clipboard |
| `o` | Open ticket in browser |
| `m` | Move card to another column |
| `D` | Delete task |
| `e` | Edit Legato-owned details in `$EDITOR` (all tasks) |
| `t` | Edit title (local tasks only) |
| `esc` | Back to board |

#### Agent View

Agents are listed by what they are doing, the ones that need you first. `e` changes between the list, with the selected agent's terminal beside it, and the ensemble, which shows every agent at once.

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate agents (`h` / `l` too in the ensemble) |
| `w` | Select the next agent that needs you |
| `e` | Change between the list and the ensemble |
| `s` | Spawn new agent |
| `X` | Kill selected agent |
| `m` | Open macro picker |
| `enter` | Attach to tmux session |
| `esc` / `q` | Back to board |

With a `manticore:` block in the config, the agent view also shows the tokens used today.

The web UI has the same ensemble at `/agents?view=ensemble`: the grid button in the agent list, or `e`.

## Architecture

```
cmd/legato/       -> wires everything
internal/tui/     -> presentation (Bubbletea)
internal/service/ -> business logic, TicketProvider interface
internal/engine/  -> infrastructure (SQLite, Jira client, event bus, tmux)
internal/setup/   -> first-run setup wizard
config/           -> YAML config parser
```

The ticket source is abstracted behind a `TicketProvider` interface. Jira is the first implementation, but the sync service never imports Jira directly — it works through the interface. Adding a new provider means implementing four methods: `Search`, `GetTicket`, `ListTransitions`, and `DoTransition`.

## Development

Requires Go 1.23+, [Task](https://taskfile.dev/), and pnpm.

```bash
task build:full    # frontend + go binary
task test          # run all tests
task test:race     # run with race detector
task test:cover    # run with coverage
task check         # build + test + vet + lint
task web:dev       # start the frontend dev server (hot reload against localhost:3080)
```

## License

MIT
