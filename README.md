# Legato

A keyboard-driven kanban board TUI for tracking tickets, built for developers who work with AI coding agents. Supports pluggable ticket providers (Jira first, others planned).

## Features

- Vim-style keyboard navigation (h/j/k/l)
- Full-screen ticket detail view with Glamour-rendered markdown
- Copy ticket context to clipboard for AI coding agents (`y` description, `Y` full context)
- Move cards between columns via overlay (`m`)
- Open tickets in browser (`o`)
- Bidirectional sync: pull tickets from Jira, push card moves back as transitions
- Offline-first: works from local SQLite when the network is down
- ADF-to-Markdown conversion for Jira ticket descriptions
- Conflict resolution: local moves win within a 5-minute window
- OS-native clipboard detection (pbcopy, wl-copy, xclip, xsel)
- Provider-agnostic architecture: swap Jira for Linear, GitHub Issues, etc.

## Install

```bash
go install github.com/cpave3/legato/cmd/legato@latest
```

Or build from source:

```bash
task build
```

## Setup

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
```

The `${LEGATO_JIRA_TOKEN}` reference is expanded at load time so the token never lives in the config file as plaintext.

### Config File Location

Legato looks for config in this order:

1. `$LEGATO_CONFIG` (env var override)
2. `$XDG_CONFIG_HOME/legato/config.yaml`
3. `~/.config/legato/config.yaml`

## Usage

```bash
legato        # launch the TUI
legato setup  # run the setup wizard (coming soon)
```

### Keyboard Shortcuts

#### Board View

| Key | Action |
|-----|--------|
| `h` / `l` | Move between columns |
| `j` / `k` | Move between cards |
| `g` / `G` | Jump to first/last card |
| `1`-`5` | Jump to column by number |
| `enter` | Open ticket detail view |
| `m` | Move card to another column |
| `r` | Manual sync |
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
| `esc` | Back to board |

## Architecture

```
cmd/legato/       -> wires everything
internal/tui/     -> presentation (Bubbletea)
internal/service/ -> business logic, TicketProvider interface
internal/engine/  -> infrastructure (SQLite, Jira client, event bus)
internal/setup/   -> setup wizard logic
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
