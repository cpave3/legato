# Legato

A keyboard-driven kanban board TUI for tracking tasks, built for developers who work with AI coding agents. Supports local tasks and pluggable ticket providers (Jira first, others planned).

## Features

- **Local-first**: create and manage tasks locally, optionally sync with Jira
- **Vim-style navigation** (h/j/k/l) across columns and cards
- **Full-screen detail view** with Glamour-rendered markdown
- **Copy context to clipboard** for AI coding agents (`y` description, `Y` full context)
- **Move cards** between columns via overlay (`m`)
- **Create tasks** inline (`n`) with title, column, and priority
- **Delete tasks** with confirmation (`d` from board, `D` from detail)
- **Import remote tickets** — search Jira and pull individual tickets (`i`)
- **Agent sessions** — spawn tmux sessions per task, track active agents on cards
- **Claude Code integration** — hooks report agent activity (working/waiting) back to the board in real-time
- **Pluggable AI tool adapters** — abstract interface for tool integrations (Claude Code first, others planned)
- **Bidirectional Jira sync**: pull tickets, push card moves as transitions
- **Offline-first**: works from local SQLite when the network is down
- **Conflict resolution**: local moves win within a 5-minute window
- **Setup wizard**: first-run experience seeds columns and optionally configures Jira
- **Provider icons**: visual indicators for Jira/GitHub/local/agent on cards
- **Nerd Fonts support**: `icons: nerdfonts` in config for glyph icons
- **Provider-agnostic architecture**: swap Jira for Linear, GitHub Issues, etc.

## Install

```bash
go install github.com/cpave3/legato/cmd/legato@latest
```

Or build from source:

```bash
task build
```

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
```

The `${LEGATO_JIRA_TOKEN}` reference is expanded at load time so the token never lives in the config file as plaintext.

You can also run Legato without any Jira config for a fully local kanban board.

### Config File Location

Legato looks for config in this order:

1. `$LEGATO_CONFIG` (env var override)
2. `$XDG_CONFIG_HOME/legato/config.yaml`
3. `~/.config/legato/config.yaml`

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
| `⟳ RUNNING` (green) | Claude is working | `UserPromptSubmit` hook |
| `◆ WAITING` (blue) | Claude is waiting for input | `Stop` hook |
| `▶ IDLE` (dim) | Agent session running, no activity yet | tmux session alive |

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
legato task update <task-id> --status <column>   # move task to column
legato task note <task-id> <message>             # append note to task
legato agent state <task-id> --activity working  # set agent activity state
legato hooks install [--tool claude-code]        # install AI tool hooks
legato hooks uninstall [--tool claude-code]      # remove AI tool hooks
```

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
| `enter` | Open task detail view |
| `m` | Move card to another column |
| `n` | Create new task |
| `d` | Delete task |
| `i` | Import remote ticket |
| `/` | Search/filter tasks |
| `a` / `t` | Spawn agent (tmux) on selected card |
| `A` | Switch to agent view |
| `r` | Manual sync |
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
| `esc` | Back to board |

#### Agent View

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate agent list |
| `s` | Spawn new agent |
| `x` | Kill selected agent |
| `enter` | Attach to tmux session |
| `esc` / `q` | Back to board |

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

Requires Go 1.23+ and [Task](https://taskfile.dev/).

```bash
task test          # run all tests
task test:race     # run with race detector
task test:cover    # run with coverage
task check         # build + test + vet + lint
```

## License

MIT
