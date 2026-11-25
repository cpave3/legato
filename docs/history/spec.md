# Legato — v0 Spec

**AI Agent Orchestration TUI**

_Streamlines the continuous flow from ticket to PR._

---

## 1. Vision

Legato is a terminal-native tool for orchestrating AI coding agents across a Jira + GitHub workflow. It provides a keyboard-driven kanban interface that syncs with Jira, renders ticket context in copy-friendly markdown, and — in later versions — manages agent lifecycles directly.

The name pairs with **Staccato** (git stacking / PR comment extraction tool). Where Staccato handles discrete, rhythmic git operations, Legato handles the smooth, connected flow of work from intake to completion.

---

## 2. Goals — v0

v0 is a **read-and-triage tool**. It does not spawn agents or create PRs. It reduces the friction of working with Jira tickets in a terminal-native workflow.

Specifically, v0 must:

1. Pull assigned Jira tickets and display them on a kanban board.
2. Allow keyboard-driven navigation and card movement between columns.
3. Render full ticket bodies as clean, copy-friendly terminal markdown.
4. Sync card movements back to Jira (bidirectional state sync).
5. Persist local state in SQLite for speed and offline fallback.
6. Be fast — sub-100ms for all local operations, async Jira sync.

### Non-goals for v0

- Agent spawning or lifecycle management.
- Terminal embedding (PTY management, xterm).
- Automated PR creation.
- PR comment ingestion (handled by Staccato today).
- Multi-user or team features.
- GitHub integration beyond what Jira already links.

---

## 3. Architecture

### 3.1 Design Principle — Headless Core, Pluggable Presentation

Legato is architected as a **headless service with a TUI as the first client**. All business logic, state management, Jira sync, and (future) agent orchestration live in a core service layer that knows nothing about how it's presented. The TUI consumes this service layer through Go interfaces. A future web UI would consume the same service layer through an HTTP/WebSocket API.

This is the same pattern as opencode's web UI — the CLI runs as a server, a browser connects to it. Legato should be built to support this from day one without actually building the web UI yet.

**The rule:** if you're writing code that imports `bubbletea`, `lipgloss`, or `glamour`, it goes in `internal/tui/`. If it doesn't, it goes in `internal/service/` or `internal/engine/`. Nothing in the service layer should import anything from the TUI layer.

### 3.2 Tech Stack

| Layer                       | Choice                                        | Rationale                                                                            |
| --------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------ |
| Language                    | Go                                            | Excellent process management (future), fast compilation, single binary distribution. |
| **Core / Service**          |                                               |                                                                                      |
| Database                    | SQLite via `modernc.org/sqlite`               | Pure Go (no CGO), zero external dependencies.                                        |
| Jira client                 | Go HTTP client + Jira REST API v3             | Direct integration, no SDK dependency.                                               |
| Event bus                   | Go channels                                   | Internal pub/sub for state changes. Presentation layers subscribe.                   |
| Configuration               | YAML (`~/.config/legato/config.yaml`)         | Standard XDG-compliant config location.                                              |
| **TUI Presentation**        |                                               |                                                                                      |
| TUI framework               | Bubbletea                                     | Elm-architecture, composable models, strong ecosystem.                               |
| Styling                     | Lipgloss                                      | Terminal styling, box rendering, colour themes.                                      |
| Markdown rendering          | Glamour                                       | Renders markdown in the terminal with syntax highlighting.                           |
| **Future Web Presentation** |                                               |                                                                                      |
| API server                  | Go `net/http` + gorilla/websocket (or stdlib) | Expose service layer over HTTP + WS.                                                 |
| Frontend                    | (TBD — React, Svelte, etc.)                   | Consumes the same API.                                                               |

### 3.3 Layer Boundaries

```
┌─────────────────────────────────────────────────────────┐
│                   Presentation Layer                     │
│                                                         │
│  ┌─────────────────┐         ┌────────────────────┐     │
│  │   TUI (v0)      │         │   Web UI (future)  │     │
│  │   Bubbletea     │         │   HTTP/WS client   │     │
│  │   Lipgloss      │         │   React/Svelte     │     │
│  │   Glamour       │         │                    │     │
│  └────────┬────────┘         └─────────┬──────────┘     │
│           │ Go interfaces               │ HTTP/WS API   │
├───────────┴─────────────────────────────┴───────────────┤
│                    Service Layer                         │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │ BoardService │  │  SyncService │  │ AgentService │  │
│  │              │  │              │  │  (future)    │  │
│  │ - ListCards  │  │ - Pull       │  │ - Spawn      │  │
│  │ - MoveCard   │  │ - Push       │  │ - Stream     │  │
│  │ - GetDetail  │  │ - Reconcile  │  │ - Stop       │  │
│  │ - Search     │  │              │  │              │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  │
│         │                 │                  │          │
├─────────┴─────────────────┴──────────────────┴──────────┤
│                    Engine Layer                          │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │    Store     │  │  Jira Client │  │  Event Bus   │  │
│  │   (SQLite)   │  │  (REST API)  │  │  (channels)  │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### 3.4 Service Interfaces

These are the contracts the presentation layer depends on. Defined as Go interfaces so they can be consumed directly (TUI) or wrapped in HTTP handlers (web).

```go
// BoardService is the primary interface for kanban operations.
type BoardService interface {
    // Board state
    ListColumns(ctx context.Context) ([]Column, error)
    ListCards(ctx context.Context, column string) ([]Card, error)
    GetCard(ctx context.Context, id string) (*CardDetail, error)

    // Mutations
    MoveCard(ctx context.Context, id string, toColumn string) error
    ReorderCard(ctx context.Context, id string, newPosition int) error

    // Search
    SearchCards(ctx context.Context, query string) ([]Card, error)

    // Context export (for clipboard / agent injection)
    ExportCardContext(ctx context.Context, id string, format ExportFormat) (string, error)
}

// SyncService manages Jira synchronisation.
type SyncService interface {
    // Trigger a full sync (pull + push pending)
    Sync(ctx context.Context) (*SyncResult, error)

    // Get sync status
    Status() SyncStatus

    // Subscribe to sync events
    Subscribe() <-chan SyncEvent
}

// EventBus provides pub/sub for state changes.
// Presentation layers subscribe to re-render on changes.
type EventBus interface {
    Publish(event Event)
    Subscribe(eventType EventType) <-chan Event
    Unsubscribe(ch <-chan Event)
}
```

The TUI's Bubbletea models call these interfaces directly. A future web server would wrap them:

```go
// Future: thin HTTP handler wrapping BoardService
func (h *Handler) HandleMoveCard(w http.ResponseWriter, r *http.Request) {
    var req MoveCardRequest
    json.NewDecoder(r.Body).Decode(&req)
    err := h.board.MoveCard(r.Context(), req.ID, req.ToColumn)
    // ...
}
```

### 3.5 Event System

The event bus decouples state changes from rendering. When the sync service pulls new tickets or a card is moved, it publishes events. The TUI subscribes and re-renders. A future WebSocket server would subscribe and push to connected clients.

```go
type EventType int

const (
    EventCardMoved EventType = iota
    EventCardUpdated
    EventCardsRefreshed
    EventSyncStarted
    EventSyncCompleted
    EventSyncFailed
    EventAgentStarted      // future
    EventAgentOutput       // future
    EventAgentCompleted    // future
)

type Event struct {
    Type    EventType
    Payload interface{}  // typed per event
    At      time.Time
}
```

### 3.6 Project Structure

```
legato/
├── cmd/
│   └── legato/
│       └── main.go                 # Entry point, wires services, launches TUI
├── internal/
│   ├── engine/                     # Low-level infrastructure (no business logic)
│   │   ├── store/
│   │   │   ├── db.go              # SQLite connection, migrations
│   │   │   ├── tickets.go        # Ticket CRUD
│   │   │   └── config.go         # Persisted settings
│   │   ├── jira/
│   │   │   ├── client.go         # Jira REST API client
│   │   │   ├── adf.go            # ADF → Markdown converter
│   │   │   └── types.go          # Jira data types
│   │   └── events/
│   │       └── bus.go             # Event bus implementation
│   │
│   ├── service/                    # Business logic (presentation-agnostic)
│   │   ├── board.go               # BoardService implementation
│   │   ├── sync.go                # SyncService implementation
│   │   ├── context.go             # Context export / formatting for clipboard + agents
│   │   └── interfaces.go         # Service interface definitions
│   │
│   ├── tui/                        # TUI presentation (imports bubbletea/lipgloss)
│   │   ├── app.go                 # Root Bubbletea model, view routing
│   │   ├── board/
│   │   │   ├── model.go          # Kanban board model
│   │   │   ├── column.go         # Column component
│   │   │   └── card.go           # Card rendering
│   │   ├── detail/
│   │   │   └── model.go          # Ticket detail view (Glamour)
│   │   ├── overlay/
│   │   │   ├── move.go           # Move picker
│   │   │   ├── help.go           # Help screen
│   │   │   └── search.go         # Fuzzy search
│   │   ├── statusbar/
│   │   │   └── model.go          # Status bar component
│   │   ├── theme/
│   │   │   └── theme.go          # Lipgloss styles
│   │   └── clipboard/
│   │       └── clipboard.go      # OS-native clipboard (pbcopy/xclip)
│   │
│   └── server/                     # Future: HTTP/WS API server
│       ├── server.go              # (stub) HTTP server wrapping services
│       ├── handlers.go            # (stub) REST handlers
│       └── ws.go                  # (stub) WebSocket event streaming
│
├── config/
│   └── config.go                   # YAML config parsing
├── go.mod
├── go.sum
└── README.md
```

### 3.7 Data Flow

**v0 — TUI direct:**

```
┌──────────┐       ┌──────────────┐       ┌──────────────┐       ┌──────────┐
│   Jira   │◄─────►│ SyncService  │◄─────►│ BoardService │◄─────►│   TUI    │
│  (REST)  │       │              │       │              │       │(Bubbletea│
└──────────┘       └──────┬───────┘       └──────┬───────┘       └──────────┘
                          │                      │
                          ▼                      ▼
                   ┌──────────────┐       ┌──────────────┐
                   │   EventBus   │──────►│  (listeners) │
                   └──────────────┘       └──────────────┘
                          │
                          ▼
                   ┌──────────────┐
                   │    SQLite    │
                   └──────────────┘
```

**Future — TUI + Web:**

```
                   ┌──────────┐       ┌──────────────┐
                   │   TUI    │◄─────►│              │
                   │(Bubbletea│  Go   │              │
                   └──────────┘ iface │              │
                                      │ BoardService │
                   ┌──────────┐       │ SyncService  │
                   │  Web UI  │◄─────►│ AgentService │
                   │ (browser)│ HTTP/ │              │
                   └──────────┘  WS   │              │
                                      └──────────────┘
```

Both clients consume the same services. The TUI calls Go interfaces directly. The web UI calls them through HTTP/WS handlers. The event bus fans out to both.

---

## 4. Data Model

### 4.1 SQLite Schema

```sql
CREATE TABLE tickets (
    id              TEXT PRIMARY KEY,        -- Jira issue key (e.g. REX-1234)
    summary         TEXT NOT NULL,
    description     TEXT,                    -- Raw Jira markup
    description_md  TEXT,                    -- Converted markdown
    status          TEXT NOT NULL,           -- Local kanban column
    jira_status     TEXT NOT NULL,           -- Actual Jira status (for sync)
    priority        TEXT,
    issue_type      TEXT,
    assignee        TEXT,
    labels          TEXT,                    -- JSON array
    epic_key        TEXT,
    epic_name       TEXT,
    url             TEXT,                    -- Jira browse URL
    created_at      TEXT NOT NULL,           -- ISO 8601
    updated_at      TEXT NOT NULL,           -- ISO 8601
    jira_updated_at TEXT NOT NULL,           -- Jira's own updated timestamp
    sort_order      INTEGER DEFAULT 0        -- Position within column
);

CREATE TABLE column_mappings (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    column_name     TEXT NOT NULL UNIQUE,    -- Legato kanban column
    jira_statuses   TEXT NOT NULL,           -- JSON array of Jira status names
    jira_transition TEXT,                    -- Transition ID to move INTO this column
    sort_order      INTEGER DEFAULT 0
);

CREATE TABLE sync_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id       TEXT NOT NULL,
    action          TEXT NOT NULL,           -- 'pull', 'push_status', 'push_fail'
    detail          TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_updated ON tickets(jira_updated_at);
```

### 4.2 Default Column Mappings

These are configurable, but sensible defaults:

| Legato Column | Jira Statuses (default)     | Jira Transition          |
| ------------- | --------------------------- | ------------------------ |
| Backlog       | To Do, Open, Backlog        | (configured per project) |
| Ready         | Ready for Dev, Selected     | (configured per project) |
| Doing         | In Progress, In Development | (configured per project) |
| Review        | In Review, Code Review      | (configured per project) |
| Done          | Done, Closed, Resolved      | (configured per project) |

---

## 5. Configuration

### 5.1 Config File

Location: `~/.config/legato/config.yaml`

```yaml
jira:
  base_url: "https://yourcompany.atlassian.net"
  email: "cameron@example.com"
  api_token: "${LEGATO_JIRA_TOKEN}" # env var reference
  project_keys: # which projects to pull from
    - "REX"
  jql_filter: "assignee = currentUser() AND status != Done"
  sync_interval_seconds: 60

board:
  columns:
    - name: "Backlog"
      jira_statuses: ["To Do", "Open", "Backlog"]
      jira_transition_id: "11"
    - name: "Ready"
      jira_statuses: ["Ready for Dev", "Selected for Development"]
      jira_transition_id: "21"
    - name: "Doing"
      jira_statuses: ["In Progress", "In Development"]
      jira_transition_id: "31"
    - name: "Review"
      jira_statuses: ["In Review"]
      jira_transition_id: "41"
    - name: "Done"
      jira_statuses: ["Done", "Closed"]
      jira_transition_id: "51"

theme: "default" # future: custom colour schemes

keybindings: # future: full rebinding
  vim_mode: true
```

### 5.2 First-Run Setup

On first run with no config file, Legato should:

1. Prompt for Jira base URL and email.
2. Instruct user to create an API token (link to Atlassian docs).
3. Ask for project key(s).
4. Fetch available statuses and transitions for those projects.
5. Auto-generate column mappings based on the fetched workflow.
6. Write the config file.

This avoids the worst Jira pain point (manually discovering transition IDs).

---

## 6. User Interface

### 6.1 Views

Legato has three primary views, plus an overlay.

#### Board View (default)

```
╭─ Legato ──────────────────────────────────────────────────────╮
│                                                               │
│  Backlog (3)    Ready (1)     Doing (2)     Review (1)        │
│  ──────────     ─────────     ─────────     ──────────        │
│  ┌──────────┐   ┌─────────┐  ┌─────────┐   ┌──────────┐      │
│  │ REX-1234 │   │ REX-1240│  │●REX-1238│   │ REX-1235 │      │
│  │ Fix auth │   │ Add log │  │ Refactor│   │ Update   │      │
│  │ bug      │   │ endpoint│  │ user svc│   │ deps     │      │
│  └──────────┘   └─────────┘  └─────────┘   └──────────┘      │
│  ┌──────────┐                ┌─────────┐                      │
│  │ REX-1236 │                │ REX-1239│                      │
│  │ Migrate  │                │ Add     │                      │
│  │ DB schema│                │ caching │                      │
│  └──────────┘                └─────────┘                      │
│  ┌──────────┐                                                 │
│  │ REX-1237 │                                                 │
│  │ Write    │                                                 │
│  │ API docs │                                                 │
│  └──────────┘                                                 │
│                                                               │
├───────────────────────────────────────────────────────────────┤
│ ● Synced 2m ago │ h/l: column │ j/k: card │ enter: detail    │
╰───────────────────────────────────────────────────────────────╯
```

- `●` indicates the currently selected card.
- Status bar shows sync state, last sync time, and key hints.
- Cards show issue key, truncated summary, and optionally priority/type icon.

#### Detail View

Opened with `enter` on a selected card. Full-screen overlay.

```
╭─ REX-1238 ── Refactor user service ──────────────────────────╮
│                                                               │
│  Status: Doing         Priority: High      Type: Story        │
│  Epic: Platform Modernisation              Labels: backend    │
│  URL: https://yourcompany.atlassian.net/browse/REX-1238       │
│                                                               │
│  ─────────────────────────────────────────────────────────────│
│                                                               │
│  ## Description                                               │
│                                                               │
│  The user service has grown organically and now has multiple   │
│  responsibilities that should be separated:                   │
│                                                               │
│  - Authentication logic mixed with profile management         │
│  - Direct database queries instead of repository pattern      │
│  - No separation between read and write paths                 │
│                                                               │
│  ### Acceptance Criteria                                      │
│                                                               │
│  - [ ] Extract auth into dedicated module                     │
│  - [ ] Implement repository pattern for data access           │
│  - [ ] Add unit tests for extracted modules                   │
│  - [ ] No breaking changes to existing API contracts          │
│                                                               │
│  ### Technical Notes                                          │
│                                                               │
│  See ADR-042 for the agreed service decomposition approach.   │
│                                                               │
├───────────────────────────────────────────────────────────────┤
│ esc: back │ y: copy description │ Y: copy all │ m: move      │
╰───────────────────────────────────────────────────────────────╯
```

- Description rendered via Glamour (terminal markdown).
- `y` copies the markdown description to clipboard (for pasting into Claude Code).
- `Y` copies a structured block: issue key + summary + description + acceptance criteria.
- `m` opens a column picker to move the ticket.

#### Move Overlay

Triggered by `m` from board or detail view.

```
  Move REX-1238 to:
  ─────────────────
  [b] Backlog
  [r] Ready
  [d] Doing      ← current
  [v] Review
  [x] Done
```

Single keypress moves the card. Jira transition fires async. If the transition fails, the card shows a warning icon and the status bar shows the error.

#### Help Overlay

Triggered by `?` from any view.

```
  Legato — Keyboard Reference
  ────────────────────────────
  Navigation
    h / l         Move between columns
    j / k         Move up/down within column
    g / G         Jump to first/last card in column
    1-5           Jump to column by number

  Actions
    enter         Open ticket detail
    m             Move ticket (column picker)
    y             Copy description (in detail view)
    Y             Copy full context block (in detail view)
    r             Force refresh from Jira
    /             Filter tickets (fuzzy search)
    esc           Back / close overlay

  General
    ?             This help screen
    q             Quit
```

### 6.2 Keybindings — Full Reference

| Key       | Context       | Action                                          |
| --------- | ------------- | ----------------------------------------------- |
| `h` / `l` | Board         | Move cursor between columns                     |
| `j` / `k` | Board         | Move cursor between cards in column             |
| `g` / `G` | Board         | First / last card in column                     |
| `1`–`5`   | Board         | Jump directly to column N                       |
| `enter`   | Board         | Open selected card in detail view               |
| `m`       | Board, Detail | Open move overlay for selected card             |
| `y`       | Detail        | Copy ticket description (markdown) to clipboard |
| `Y`       | Detail        | Copy full structured context to clipboard       |
| `o`       | Detail        | Open Jira ticket URL in browser                 |
| `r`       | Board         | Force Jira sync                                 |
| `/`       | Board         | Open fuzzy search/filter                        |
| `esc`     | Any overlay   | Close overlay, return to previous view          |
| `?`       | Any           | Show help overlay                               |
| `q`       | Board         | Quit                                            |

---

## 7. Jira Integration

### 7.1 API Operations

| Operation            | Endpoint                                   | When                                           |
| -------------------- | ------------------------------------------ | ---------------------------------------------- |
| Search tickets       | `GET /rest/api/3/search`                   | Startup, periodic sync, manual refresh         |
| Get ticket detail    | `GET /rest/api/3/issue/{key}`              | On first view of a ticket not yet fully loaded |
| Transition ticket    | `POST /rest/api/3/issue/{key}/transitions` | On card move                                   |
| Get transitions      | `GET /rest/api/3/issue/{key}/transitions`  | Setup wizard, and cached per status            |
| Get project statuses | `GET /rest/api/3/project/{key}/statuses`   | Setup wizard                                   |

### 7.2 Sync Algorithm

**Pull (Jira → local):**

1. Execute JQL query for assigned tickets.
2. For each ticket in results:
   - If not in SQLite: insert with column derived from status → column mapping.
   - If in SQLite and `jira_updated_at` is newer: update fields. If Jira status maps to a different column than the local column, and there's no pending local move, update the column.
3. For tickets in SQLite not in Jira results: mark as stale (configurable: hide after N days or remove).

**Push (local → Jira):**

1. On card move in UI, immediately update SQLite.
2. Queue a Jira transition request (async, non-blocking).
3. On success: update `jira_status` in SQLite, log to `sync_log`.
4. On failure: show warning icon on card, notification in status bar, log to `sync_log`. Card stays in the local column (user's intent is preserved). Retry on next manual sync (`r`).

**Conflict resolution:**

- Local moves take priority within a short window (5 minutes). If Jira changed externally and the user moved locally, the local move wins.
- After the window, external Jira changes take priority on the next sync.
- Conflicts are logged and surfaceable via a future diagnostics view.

### 7.3 Jira Markup → Markdown Conversion

Jira uses Atlassian Document Format (ADF) in API v3 (JSON-based). Legato needs an ADF → Markdown converter. Key mappings:

| ADF Node                     | Markdown Output                        |
| ---------------------------- | -------------------------------------- |
| `heading` (level 1–6)        | `#` – `######`                         |
| `paragraph`                  | Plain text with newline                |
| `bulletList` / `orderedList` | `- ` / `1. `                           |
| `codeBlock`                  | Fenced code block with language        |
| `blockquote`                 | `> ` prefix                            |
| `table`                      | Markdown table                         |
| `mention`                    | `@display_name`                        |
| `inlineCard` (Jira link)     | `[KEY: summary](url)`                  |
| `emoji`                      | Unicode emoji                          |
| `panel` (info/warning/etc)   | Blockquote with type prefix            |
| `media` / `mediaSingle`      | `![alt](url)` or omitted if attachment |
| `status`                     | `[STATUS_TEXT]` inline                 |

This converter is a standalone internal package (`internal/jira/adf.go`) so it can be tested independently.

---

## 8. Clipboard Integration

Critical for v0's core use case (copy ticket context, paste into Claude Code).

### 8.1 Copy Formats

**`y` — Description only:**

```markdown
## REX-1238: Refactor user service

The user service has grown organically and now has multiple
responsibilities that should be separated...

### Acceptance Criteria

- [ ] Extract auth into dedicated module
- [ ] Implement repository pattern for data access
      ...
```

**`Y` — Full context block:**

```markdown
# Ticket: REX-1238

**Summary:** Refactor user service
**Type:** Story | **Priority:** High | **Epic:** Platform Modernisation
**Labels:** backend
**URL:** https://yourcompany.atlassian.net/browse/REX-1238

---

## Description

The user service has grown organically and now has multiple
responsibilities that should be separated...

### Acceptance Criteria

- [ ] Extract auth into dedicated module
- [ ] Implement repository pattern for data access
      ...

### Technical Notes

See ADR-042 for the agreed service decomposition approach.
```

### 8.2 Clipboard Mechanism

Use OS-native clipboard:

- **macOS**: `pbcopy` via `exec.Command`
- **Linux**: `xclip` or `xsel` (with wayland fallback via `wl-copy`)
- Detect platform at startup, warn if clipboard tool not found.

---

## 9. Error Handling

| Scenario                        | Behaviour                                                                                           |
| ------------------------------- | --------------------------------------------------------------------------------------------------- |
| No network on startup           | Load from SQLite, show "offline" in status bar, retry sync on interval                              |
| Jira auth failure               | Show error in status bar, prompt to check config. Don't crash.                                      |
| Jira transition fails           | Card stays in local column, warning icon on card, error detail in status bar. Logged to `sync_log`. |
| Invalid transition ID in config | Log the specific failure, suggest running setup wizard again.                                       |
| SQLite corruption               | On startup, if DB is unreadable, offer to recreate (pull fresh from Jira).                          |
| Jira rate limiting              | Exponential backoff on 429 responses. Show "rate limited" in status bar.                            |

---

## 10. Future Versions — Roadmap

This section is informational. None of this is in scope for v0.

### v1 — Agent Spawning

- "Start work" keybind (`s`) on a card in Doing spawns a Claude Code session.
- Context from the ticket (full `Y` block) is injected as the initial prompt.
- Agent output is viewable in a split pane or tab.
- Agent runs in an isolated Git worktree (one per ticket).
- Agent process registry: start, stop, restart, view status.

### v2 — PR Lifecycle

- When agent signals completion, auto-create a PR via GitHub CLI.
- PR link is attached to the Jira ticket.
- Ticket auto-moves to Review column.
- GitHub webhook listener (or polling) detects PR review comments.
- "Changes requested" moves ticket back to Doing and assembles a comment context file (leveraging Staccato's existing `gh` CLI patterns).
- Re-injects comment context into a new Claude Code session.

### v3 — Multi-Agent Support

- Pluggable `Agent` interface: `Start(ctx Context)`, `Output() <-chan string`, `Status() AgentStatus`, `Inject(context string)`, `Stop()`.
- Claude Code, Devin API, OpenHands, and others as implementations.
- Agent selection per-ticket or per-project.
- Concurrency controls: max parallel agents, file-level conflict detection.
- Cost tracking per agent per ticket.

### v4 — Web UI

- `legato serve` runs the service layer as an HTTP/WebSocket server.
- Lightweight web frontend (React or Svelte) consuming the same service interfaces via REST + WS.
- Same kanban board, ticket detail, and agent stream views — just in a browser.
- TUI and web UI can run simultaneously against the same service instance.
- This is the team-accessible version — non-terminal users get a familiar web experience while the underlying engine is identical.

### v5 — Team Features

- Shared board view (multiple assignees).
- Slack integration for kicking off work.
- Dashboard metrics: cycle time, agent success rate, cost per ticket.

---

## 11. Development Plan

### Phase 1 — Engine Layer (2–3 days)

- [ ] Go module init, layered project structure (`engine/`, `service/`, `tui/`).
- [ ] SQLite setup with migrations (`engine/store/`).
- [ ] Ticket CRUD operations.
- [ ] Column mapping table.
- [ ] Config file parsing (YAML).
- [ ] Event bus implementation (`engine/events/`).
- [ ] **Validate:** write a short `main.go` that inserts/reads tickets via the store, no TUI yet.

### Phase 2 — Service Layer (1–2 days)

- [ ] Define `BoardService`, `SyncService`, `EventBus` interfaces (`service/interfaces.go`).
- [ ] Implement `BoardService` — list, move, reorder, search, export context.
- [ ] Implement `SyncService` stub (in-memory fake Jira data for testing).
- [ ] Wire event bus: services publish, consumers subscribe.
- [ ] **Validate:** write a CLI harness that calls service methods, prints results. Confirm the service layer works without any TUI code.

### Phase 3 — TUI Shell (1–2 days)

- [ ] Bubbletea app shell consuming `BoardService` interface.
- [ ] Static kanban board rendering with data from service layer.
- [ ] Vim-style navigation (h/j/k/l, enter, esc).
- [ ] Lipgloss theming.
- [ ] Status bar subscribing to `EventBus` for sync state display.
- [ ] **Validate:** full board navigation with fake data, zero Jira dependency.

### Phase 4 — Jira Integration (2–3 days)

- [ ] Jira REST client (`engine/jira/`): search, get issue, transitions.
- [ ] ADF → Markdown converter (`engine/jira/adf.go`).
- [ ] Replace fake `SyncService` with real Jira-backed implementation.
- [ ] Pull sync (Jira → SQLite).
- [ ] Push sync (card move → Jira transition, async).
- [ ] Setup wizard (first-run workflow discovery).

### Phase 5 — Detail View & Clipboard (1 day)

- [ ] Glamour-rendered ticket detail view (`tui/detail/`).
- [ ] Context export via `BoardService.ExportCardContext()` — presentation-agnostic.
- [ ] Clipboard copy (`y` / `Y`) in TUI, calling the service export method.
- [ ] Open in browser (`o`).

### Phase 6 — Polish (1–2 days)

- [ ] Fuzzy search / filter (`/`) — calling `BoardService.SearchCards()`.
- [ ] Move overlay.
- [ ] Help overlay.
- [ ] Error handling for all Jira failure modes (surfaced via event bus).
- [ ] Sync log and diagnostics.
- [ ] **Validate:** stub out `internal/server/` package with a single health-check endpoint that returns board state as JSON, confirming the service layer is web-ready without building the full web UI.

**Estimated total: 8–13 days to a usable v0.**

The extra 2–3 days vs. a monolithic approach are spent on the layering, but they pay for themselves immediately when v1 adds agent management and v2 adds the web UI.

---

## 12. Open Questions

1. **Ticket ordering within columns.** Sort by Jira priority? By last-updated? Manual drag (complex in TUI)? Recommend: Jira priority as default, with `J`/`K` (shift) for manual reorder within a column.

2. **Multiple projects.** v0 config supports multiple `project_keys`, but should the board show a single mixed view or project-scoped tabs? Recommend: single mixed view with issue key as the differentiator, filter by project via `/`.

3. **Subtasks.** Should subtasks appear as independent cards or nested under their parent? Recommend: independent cards for v0, with parent key shown in detail view.

4. **Done column retention.** How long to show completed tickets? Recommend: configurable, default 7 days, then hidden (not deleted from SQLite).

5. **Staccato integration.** Should Legato call Staccato directly for PR comment extraction in v2, or should they share a library? Recommend: shared Go package extracted from Staccato when v2 begins.

6. **Server mode architecture.** When the web UI arrives, should `legato serve` be a separate command that runs the service layer as a daemon (TUI connects over IPC/socket), or should the TUI always embed the services in-process and the server mode be additive? Recommend: in-process for v0–v3 (simpler, single binary), then extract to daemon mode for v4+ when multiple clients need simultaneous access. The interface boundary is already clean enough that this refactor would be mechanical.

7. **Event bus implementation.** Go channels are fine for in-process pub/sub, but don't survive across a network boundary. When the web UI arrives, the event bus needs to fan out over WebSockets. Recommend: define the `EventBus` interface now, implement with channels for v0, swap to a WS-capable implementation later. The interface stays the same.
