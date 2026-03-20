## Why

Legato's 3-layer architecture (engine/service/presentation) was designed with presentation-agnostic services. The TUI is the first client, but all service interfaces accept `context.Context`, the event bus supports multiple subscribers, and a server stub already exists. Adding a web server mode lets users run Legato as an HTTP/WebSocket service with a browser-based client — useful for teams, remote access, and users who prefer a GUI over a terminal. The foundation is already there; this change builds on it.

## What Changes

- New `legato serve` CLI subcommand that starts the HTTP server without the TUI
- Expand `internal/server/` from a single `/health` endpoint to a full REST API (boards, cards, columns, tasks CRUD, sync operations)
- Add WebSocket endpoint that bridges `events.Bus` to connected clients for real-time board updates
- Add static file serving for an embedded web client (SPA)
- Build a web client (likely lightweight — vanilla JS or Preact) that mirrors the TUI's kanban board, detail view, and overlays
- Add server configuration to `config.yaml` (bind address, port, auth token)
- Wire services identically to TUI mode: same store, same event bus, same sync scheduler, same IPC reception
- **Agent operations exposed as read-only** in web mode (list agents, view output) — spawning/killing agents still requires the TUI/CLI due to tmux dependency

## Capabilities

### New Capabilities
- `rest-api`: Full HTTP REST API for board/card/task/sync operations, expanding the existing server stub
- `websocket-events`: WebSocket endpoint bridging the event bus to web clients for real-time updates
- `web-client`: Browser-based kanban board SPA served as embedded static assets
- `serve-command`: `legato serve` CLI subcommand that starts the server without TUI

### Modified Capabilities
- `server-stub`: Requirements expand from a single health endpoint to a full API server with middleware, auth, and CORS
- `legato-cli`: Add `serve` subcommand to the CLI dispatch

## Impact

- **Code**: `internal/server/` expands significantly (routes, handlers, middleware, WebSocket). New `web/` directory for client assets. `cmd/legato/main.go` gains `serve` subcommand wiring.
- **Dependencies**: WebSocket library (e.g., `nhooyr.io/websocket` or `gorilla/websocket`), possibly a lightweight JS build step for the web client
- **Config**: New `server` section in `config.yaml` (addr, port, auth)
- **Database**: No schema changes — web server uses the same SQLite store. WAL mode already supports concurrent readers.
- **IPC**: Web server instance listens on IPC just like TUI instances, so CLI hooks (agent state updates) propagate to web clients too
