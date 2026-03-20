## 1. Configuration & Dependencies

- [ ] 1.1 Add `server` section to config struct (`Addr`, `AuthToken`, `CORSOrigin`) with defaults (`:8080`, `""`, `*`), update YAML tags and `config_test.go`
- [ ] 1.2 Add `nhooyr.io/websocket` dependency (`go get nhooyr.io/websocket`)

## 2. Server Expansion (internal/server/)

- [ ] 2.1 Replace `New(svc, addr)` constructor with `New(opts ServerOptions)` accepting `BoardService` (required), `SyncService`, `AgentService`, `*events.Bus`, `AuthToken`, `CORSOrigin` — update existing server tests
- [ ] 2.2 Add CORS middleware that sets `Access-Control-Allow-Origin` from config and handles OPTIONS preflight
- [ ] 2.3 Add auth middleware that checks `Authorization: Bearer <token>` on `/api/v1/*` and `/ws` routes (skip when token is empty)
- [ ] 2.4 Implement `GET /api/v1/board` handler — returns all columns with nested cards (including agent states)
- [ ] 2.5 Implement `GET /api/v1/cards/:id` handler — returns full card detail
- [ ] 2.6 Implement `POST /api/v1/cards` handler — creates task (title required, column/priority optional)
- [ ] 2.7 Implement `DELETE /api/v1/cards/:id` handler
- [ ] 2.8 Implement `PUT /api/v1/cards/:id/move` handler — moves card to target column
- [ ] 2.9 Implement `PUT /api/v1/cards/:id/reorder` handler — sets card position
- [ ] 2.10 Implement `GET /api/v1/cards/search?q=` handler
- [ ] 2.11 Implement `POST /api/v1/sync`, `GET /api/v1/sync/status`, `GET /api/v1/sync/search?q=` handlers (nil-safe, return 404 when sync unavailable)
- [ ] 2.12 Implement `POST /api/v1/sync/import/:id` handler
- [ ] 2.13 Implement `GET /api/v1/agents` handler (nil-safe, return empty array when agent service unavailable)
- [ ] 2.14 Add JSON response types to `types.go` for all new endpoints (board, card detail, create/move/reorder requests, sync results, agent list)

## 3. WebSocket Hub (internal/server/)

- [ ] 3.1 Implement `WebSocketHub` struct — manages connected clients, subscribes to all event types on `*events.Bus`, routes events to client goroutines
- [ ] 3.2 Implement `/ws` upgrade handler with auth support (header or `?token=` query param)
- [ ] 3.3 Implement per-client write loop — reads from bus subscription channels, serializes events as JSON frames, handles client disconnect with `Bus.Unsubscribe()`
- [ ] 3.4 Add WebSocket hub tests — connect, receive event, disconnect cleanup, multiple clients, auth rejection

## 4. Static Asset Serving

- [ ] 4.1 Create `web/dist/` directory structure with placeholder `index.html`
- [ ] 4.2 Add `//go:embed web/dist` in server package with `http.FileServer` serving at `/`, SPA fallback for unknown paths to `index.html`
- [ ] 4.3 Add build tag `dev` that serves from filesystem instead of embed (for development hot-reload)

## 5. Serve Command (cmd/legato/)

- [ ] 5.1 Add `serve` case to `runCLI` dispatch and `runServe()` function — wires config, store, event bus, board/sync/agent services, IPC server (same pattern as `runTUI`)
- [ ] 5.2 Parse `--addr` flag (overrides `server.addr` from config)
- [ ] 5.3 Add signal handling (`SIGINT`/`SIGTERM`) with graceful shutdown — drain HTTP connections (5s timeout), stop sync scheduler, close IPC, close DB
- [ ] 5.4 Update usage text to include `serve` in command list

## 6. Web Client (web/)

- [ ] 6.1 Set up Preact + HTM project in `web/src/` with esbuild config, `task build:web` command
- [ ] 6.2 Implement API client module — fetch wrapper with auth token, error handling, all REST endpoints
- [ ] 6.3 Implement WebSocket client module — connect, reconnect with exponential backoff, event dispatch
- [ ] 6.4 Implement board component — columns layout, card rendering with priority/provider/agent indicators
- [ ] 6.5 Implement card detail component — markdown rendering, metadata display, close on Escape
- [ ] 6.6 Implement move dialog component — column selector, PUT move on confirm
- [ ] 6.7 Implement create task form — title input, column/priority selectors, POST on submit
- [ ] 6.8 Implement delete confirmation dialog — remote-tracking warning, DELETE on confirm
- [ ] 6.9 Implement search component — input with debounced query, results list, navigate to card
- [ ] 6.10 Implement agent status indicators on cards — working (spinner), waiting (idle icon), none
- [ ] 6.11 Wire real-time updates — on WebSocket event, re-fetch board state and re-render
- [ ] 6.12 Add CSS styling — kanban layout, card colors matching TUI theme, responsive columns

## 7. Integration & Testing

- [ ] 7.1 Add integration test: start server with real SQLite, create/move/delete tasks via API, verify board state
- [ ] 7.2 Add integration test: WebSocket client receives events after API mutations
- [ ] 7.3 Add integration test: IPC message from simulated CLI hook propagates to WebSocket client
- [ ] 7.4 Build `web/dist/` and verify `task build` embeds assets and produces working binary
- [ ] 7.5 Update Taskfile with `build:web`, `dev:web` (filesystem serving), and `serve` tasks
