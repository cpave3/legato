## Context

Legato has a clean 3-layer architecture (engine → service → presentation) where all services are interface-based and presentation-agnostic. The TUI is the first presentation client, but the service layer was designed for reuse. A server stub (`internal/server/`) already exists with a single `/health` endpoint consuming `BoardService`. The event bus supports multiple subscribers with non-blocking publishes — ideal for real-time push to web clients. SQLite with WAL mode supports concurrent reads from multiple processes.

The goal is to add `legato serve` as a second presentation mode: an HTTP server + WebSocket endpoint + embedded web client, sharing the same database, event bus, sync scheduler, and IPC hooks as the TUI.

## Goals / Non-Goals

**Goals:**
- Full REST API covering all `BoardService` and `SyncService` operations
- Real-time updates via WebSocket, bridging the event bus to connected clients
- Embedded web client (SPA) served from the same binary — no separate build/deploy
- `legato serve` subcommand with independent lifecycle from TUI
- Same IPC integration: CLI hooks (agent state updates from Claude Code) propagate to web clients
- Bearer token authentication for the API

**Non-Goals:**
- Multi-user auth / RBAC — single-user tool, one token is sufficient for v1
- Agent spawning/killing from the web client — tmux requires a local terminal; web client shows agent status read-only
- SSR or server-rendered HTML — SPA with static assets only
- HTTPS termination — defer to reverse proxy (nginx, caddy) for TLS
- Mobile-optimized layout — desktop browser first

## Decisions

### 1. WebSocket over SSE for real-time updates

**Choice**: WebSocket via `nhooyr.io/websocket` (stdlib-compatible, maintained, no gorilla dependency)

**Alternatives considered**:
- **SSE (Server-Sent Events)**: Simpler, HTTP-native, but unidirectional. Future features (e.g., drag-drop reorder from web client triggering server-side actions) benefit from bidirectional communication. SSE also lacks clean binary framing and reconnection is client-side only.
- **Polling**: Simple but wastes bandwidth and has latency. The event bus is already push-based — polling throws away that advantage.

**Rationale**: WebSocket matches the event bus's push model. `nhooyr.io/websocket` uses `net/http` handlers (no framework), is context-aware, and handles ping/pong automatically.

### 2. Event bus bridge pattern

**Choice**: A `WebSocketHub` subscribes to all event types on the bus. For each connected client, it maintains a goroutine that reads from the bus subscription channel and writes JSON frames to the WebSocket. Client disconnect triggers `Unsubscribe()`.

**Key detail**: The bus already handles slow consumers (non-blocking publish with `select/default`). If a WebSocket client can't keep up, events are silently dropped — same behavior as a slow TUI. No additional buffering layer needed.

### 3. Embedded SPA via `embed.FS`

**Choice**: Web client assets live in `web/dist/` and are embedded into the binary via `//go:embed`. The server serves them at `/` with `http.FileServer`. API routes are prefixed `/api/v1/`.

**Alternatives considered**:
- **Separate frontend deploy**: More flexible but adds operational complexity for a developer tool. Users would need to run two things.
- **Template rendering**: Adds server-side complexity and couples Go code to HTML. SPA is simpler — server is just an API.

**Rationale**: Single binary distribution is a key UX property of Legato. `embed.FS` preserves this. The web client can be built with a lightweight tool (esbuild) and committed as a build artifact, or built via `task build:web`.

### 4. Web client technology: Preact + HTM (no build step required)

**Choice**: Preact (3KB) with HTM tagged templates — no JSX, no bundler required for development. Production build uses esbuild for minification.

**Alternatives considered**:
- **React**: Heavy (40KB+), requires build toolchain
- **Vanilla JS**: No component model, DOM manipulation becomes unwieldy for kanban boards
- **Svelte/Vue**: Good but add framework-specific build requirements

**Rationale**: Preact + HTM gives a component model and virtual DOM at 3KB. HTM templates work in plain `.js` files — no transpilation needed during development. `esbuild` handles production minification in milliseconds. Matches Legato's "lightweight developer tool" aesthetic.

### 5. API design: REST with JSON

**Choice**: Standard REST endpoints under `/api/v1/`. All responses are JSON. Errors use HTTP status codes + `{"error": "message"}` body.

**Endpoints**:
```
GET    /api/v1/board              → columns + cards (full board state)
GET    /api/v1/cards/:id          → card detail
POST   /api/v1/cards              → create task
DELETE /api/v1/cards/:id          → delete task
PUT    /api/v1/cards/:id/move     → move card to column
PUT    /api/v1/cards/:id/reorder  → reorder card within column
GET    /api/v1/cards/search?q=    → search cards
POST   /api/v1/sync              → trigger sync
GET    /api/v1/sync/status        → sync status
GET    /api/v1/sync/search?q=    → search remote
POST   /api/v1/sync/import/:id   → import remote task
GET    /api/v1/agents            → list agents (read-only)
GET    /health                    → health check (existing, kept at root)
GET    /ws                        → WebSocket upgrade
```

### 6. Authentication: Bearer token

**Choice**: Static bearer token configured in `config.yaml` under `server.auth_token`. Sent as `Authorization: Bearer <token>` header. Missing or wrong token → 401. The `/health` endpoint is unauthenticated (standard for health checks).

**Rationale**: Legato is a single-user tool. A static token is simple, sufficient, and avoids session management complexity. For local-only use, users can omit the token and the server runs unauthenticated.

### 7. Serve command wiring mirrors TUI wiring

**Choice**: `runServe()` in `main.go` follows the same pattern as `runTUI()` — creates store, event bus, board/sync/agent services, IPC server, then starts the HTTP server instead of bubbletea. Shares the same `config.Load()`, `store.New()`, `events.New()` setup.

```
legato serve [--addr :8080]
```

The server receives IPC messages (from CLI hooks) just like TUI instances do, publishing them to the event bus, which pushes them to WebSocket clients.

### 8. Server struct expansion

**Choice**: Expand the existing `internal/server/Server` struct to accept additional services (`SyncService`, `AgentService`, `EventBus`) via a new `ServerOptions` struct, rather than positional parameters. Keep `BoardService` required, others optional (nil-safe).

```go
type ServerOptions struct {
    Board    service.BoardService  // required
    Sync     service.SyncService   // optional
    Agents   service.AgentService  // optional
    Bus      *events.Bus           // optional, enables WebSocket
    AuthToken string               // optional, enables auth
}
```

## Risks / Trade-offs

**[SQLite concurrent writes]** → Both TUI and web server writing to the same DB could cause `SQLITE_BUSY`. Mitigation: WAL mode + 5s busy timeout already configured. Legato is low-write (task moves, sync updates). If this becomes an issue, introduce a write-through service or single-writer pattern.

**[WebSocket scaling]** → Event bus drops events for slow consumers. Mitigation: Web client handles missed events by re-fetching full board state on reconnect. Not a real scaling concern for a developer tool.

**[Embedded assets increase binary size]** → Preact SPA is small (~50-100KB gzipped). Acceptable for a CLI tool. Go's `embed.FS` compresses well with `upx` if needed.

**[No HTTPS]** → Users must run behind a reverse proxy for TLS. Mitigation: Document nginx/caddy config. For local-only use (localhost), this is fine. Could add TLS support later via `server.tls_cert`/`server.tls_key` config.

**[Agent operations limited]** → Web client can view agents but not spawn/kill. Mitigation: Clear UI indication that agent management requires the TUI or CLI. Could add a "run command" API later for controlled agent spawning.

## Open Questions

1. **Should the web client support mobile/touch?** — Deferred to non-goal for now, but kanban boards are naturally touch-friendly. Preact makes this easy to add later.
2. **Should we support running TUI and web server simultaneously from the same process?** — Currently planned as separate processes sharing the DB. A combined mode could reduce resource usage but adds complexity.
3. **File watching for web client development** — During development, should `legato serve` watch `web/dist/` for changes? Or just use `embed.FS` always? Could use build tag to switch between embedded and filesystem serving.
