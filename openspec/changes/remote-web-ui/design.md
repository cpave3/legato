## Context

Legato currently has a minimal HTTP server stub (`internal/server/`) with only a `/health` endpoint, an IPC system (`internal/engine/ipc/`) for CLI-to-TUI communication via Unix domain sockets, and tmux management (`internal/engine/tmux/`) with spawn/kill/capture but no send-keys. The TUI runs in a terminal and there's no way to monitor or interact with agents remotely.

The web server will be a second presentation layer alongside the TUI, following the same layered architecture. It consumes the same service interfaces, connects to the same IPC bus for real-time updates, and talks to tmux for agent I/O.

## Goals / Non-Goals

**Goals:**
- Serve a React SPA from the Go binary via `embed.FS`
- Real-time agent monitoring: list agents, stream terminal output via WebSocket
- Send input to agent tmux sessions (both canned approval actions and free text)
- Detect Claude Code prompt types from terminal output and surface appropriate UI controls
- Start server via `legato serve` CLI or `S` shortcut in TUI
- Stay in sync with TUI state via IPC socket subscription

**Non-Goals:**
- Authentication (Tailscale provides network-level access control)
- Board/kanban management in the web UI (stub page only)
- Task creation, editing, or deletion from web UI
- Mobile-native app
- Multi-user / collaborative features
- Supporting agent CLIs other than Claude Code for prompt detection

## Decisions

### 1. WebSocket for real-time communication (not SSE or polling)

WebSocket provides bidirectional communication — we need upstream (send keys to tmux) and downstream (stream pane output). SSE is downstream-only. Polling capture-pane over HTTP would be laggy and wasteful.

**Library**: `nhooyr.io/websocket` — idiomatic Go, context-aware, good for the stdlib `net/http` mux we already use. Avoids the maintenance concerns of `gorilla/websocket`.

**Alternative considered**: Server-Sent Events for output + REST POST for input. Simpler protocol but two connections per agent, and SSE has browser connection limits (6 per domain in HTTP/1.1).

### 2. Embedded SPA via `embed.FS`

The React app is built at compile time and embedded into the Go binary. Single binary deployment, no separate process, no CORS.

**Build location**: `web/` directory at repo root for source, `internal/server/static/` for the built output that gets embedded.

**Alternative considered**: Separate frontend dev server. More complex deployment, CORS issues, two processes to manage.

### 3. Server connects to IPC as a client, not as a replacement

The web server connects to the TUI's existing Unix socket as a client (same as CLI commands). When the TUI publishes events (card updated, PR status changed), the web server receives them and fans out to connected WebSocket clients.

For standalone `legato serve` mode (no TUI running), the server creates its own IPC server socket and listens directly.

**Alternative considered**: Shared event bus. Would require refactoring the event bus out of the TUI layer — violates the current architecture where the bus lives in `internal/engine/events/` but is wired in `cmd/legato/`.

### 4. Prompt detection via regex, not LLM

Claude Code has stable, predictable prompt formats. A regex classifier on the last N lines of capture-pane output can reliably detect:
- **Tool approval**: patterns like `Allow <tool>`, `Do you want to`, `[Y/n]`
- **Plan approval**: `Accept plan?`, `Do you want to proceed`
- **Free text input**: the `❯` or `>` prompt with no pending question
- **Working**: output is actively changing (compare consecutive captures)
- **Compact approval**: `Yes / Yes, and don't ask again / No`

This lives in a new `internal/engine/prompt/` package — pure functions, no dependencies, easily testable. An LLM summarization layer can be added later as an enhancement.

**Alternative considered**: LLM classification. Overkill for ~6 known prompt patterns. Adds latency, cost, and a dependency. Better as a v2 enhancement for summarizing *what* the agent is asking approval for.

### 5. Frontend stack: React + Vite + Tailwind + shadcn/ui

- **React 19** with TypeScript — user preference, good ecosystem
- **Vite** — fast dev server, clean build output for embedding
- **Tailwind CSS v4** — utility-first, pairs with shadcn
- **shadcn/ui + Radix** — accessible, composable components, no heavy runtime
- **xterm.js** — terminal emulator widget for rendering agent output with ANSI support

The frontend is a standard Vite project in `web/`. Build output goes to `internal/server/static/dist/`.

### 6. Agent output streaming architecture

```
tmux capture-pane (200ms interval)
  → Go server goroutine per agent
    → diff against previous capture
      → WebSocket broadcast to subscribed clients
        → xterm.js renders in browser
```

The server maintains one capture goroutine per active agent session. Clients subscribe to specific agents via WebSocket messages. Capture interval matches the TUI's existing 200ms polling.

**Full pane content on connect**: when a client first subscribes to an agent, send the full current capture. Subsequent messages are diffs (new lines only) to minimize bandwidth.

### 7. Send-keys flow

```
Browser input → WebSocket message {type: "send_keys", agent_id: "...", keys: "..."}
  → Server validates agent exists and is alive
    → tmux.SendKeys(sessionName, keys)
```

For canned actions (approve/reject), the frontend sends the exact keystrokes Claude Code expects (e.g., `y\n` for yes, `n\n` for no). For free text, it sends the raw text followed by `Enter`.

New `SendKeys(name, keys string)` method on `tmux.Manager` — thin wrapper around `tmux send-keys -t <name> <keys>`.

### 8. API shape

**REST endpoints:**
- `GET /api/agents` — list active agent sessions with task context
- `GET /api/agents/:id` — single agent detail
- `GET /api/tasks` — list tasks (board data, for future board page)
- `GET /api/health` — server health (existing, extended)

**WebSocket endpoint:**
- `GET /ws` — multiplexed WebSocket connection

**WebSocket message protocol (JSON):**
```
// Client → Server
{type: "subscribe_agent", agent_id: "abc123"}
{type: "unsubscribe_agent", agent_id: "abc123"}
{type: "send_keys", agent_id: "abc123", keys: "y\n"}

// Server → Client
{type: "agent_output", agent_id: "abc123", content: "...", full: true}
{type: "agent_output", agent_id: "abc123", content: "...", full: false}
{type: "agent_list", agents: [...]}
{type: "prompt_state", agent_id: "abc123", prompt_type: "tool_approval", context: "Edit store.go"}
{type: "agents_changed"}  // trigger re-fetch
```

### 9. Package layout

```
web/                          # React source (Vite project)
  src/
    components/
    pages/
      agents.tsx              # Agent monitoring page
      board.tsx               # Stub/placeholder
    hooks/
      useWebSocket.ts         # WebSocket connection manager
    lib/
      prompt.ts               # Prompt type definitions
  package.json
  vite.config.ts

internal/
  engine/
    prompt/                   # NEW: prompt detection
      detect.go               # Regex-based prompt classifier
      detect_test.go
    tmux/
      tmux.go                 # Add SendKeys method
  server/
    server.go                 # Expand: REST + WebSocket + SPA serving
    agents.go                 # Agent REST handlers
    ws.go                     # WebSocket hub + agent streaming
    static/
      embed.go                # embed.FS for built frontend
      dist/                   # Vite build output (gitignored, embedded at compile)

cmd/legato/
  main.go                     # Add "serve" subcommand
```

## Risks / Trade-offs

**[Terminal rendering fidelity]** → xterm.js handles ANSI well but Claude Code uses advanced terminal features (progress bars, spinners, tree rendering). May need tuning. Mitigation: start with raw capture-pane output, iterate on rendering.

**[Capture-pane bandwidth]** → Full pane content at 200ms is ~80KB/s per agent over WebSocket. Mitigation: diff-based updates after initial sync; only send when content actually changes.

**[Build complexity]** → Adding a Node.js build step to a pure-Go project. Mitigation: `task build` runs `npm run build` in `web/` then `go build`. CI caches `node_modules`. The embedded `dist/` is gitignored — contributors without Node can still `go build` if `dist/` is pre-populated (e.g., from CI artifacts or a make target).

**[Prompt detection accuracy]** → Regex may miss edge cases or break on Claude Code updates. Mitigation: conservative matching — default to "free text input" when uncertain. Prompt patterns are versioned with Claude Code releases, which are infrequent.

**[Single binary size]** → Embedded SPA adds ~2-5MB to binary. Acceptable trade-off for zero-deployment simplicity.
