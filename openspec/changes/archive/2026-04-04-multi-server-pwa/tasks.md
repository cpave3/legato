## 1. Server-Side CORS

- [x] 1.1 Add CORS middleware to Go HTTP server (`internal/server/server.go`): wrap the mux handler to set `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods: GET, POST, OPTIONS`, `Access-Control-Allow-Headers: Content-Type` on all responses. Handle OPTIONS preflight with 204.
- [x] 1.2 Add test for CORS headers: OPTIONS preflight returns 204 with correct headers, GET response includes CORS headers. Verify WebSocket upgrade path is unaffected.

## 2. Server Context and Dynamic Base URL

- [x] 2.1 Create `web/src/hooks/useServer.ts`: `ServerContext` providing `baseUrl` (string, empty for origin) and `wsUrl` (string). `useServer()` hook. `ServerProvider` reads `legato:active-server` from localStorage, derives `baseUrl` and `wsUrl`.
- [x] 2.2 Create `web/src/lib/api.ts` helper: `apiFetch(path, init?)` that prepends `baseUrl` from context. Export a hook `useApiFetch()` or a standalone function that reads the context.
- [x] 2.3 Update `useWebSocket.ts`: accept `wsUrl` from server context instead of deriving from `window.location`. Reconnect when `wsUrl` changes.
- [x] 2.4 Update all `fetch()` calls in `Agents.tsx` and `Settings.tsx` to use the dynamic base URL (via `apiFetch` or prepending `baseUrl`).
- [x] 2.5 Wire `ServerProvider` into `App.tsx` (or `main.tsx`) wrapping the app, above `WebSocketProvider`.

## 3. Server Registry (localStorage CRUD)

- [x] 3.1 Add server registry helpers in `useServer.ts`: `addServer(name, url)`, `removeServer(url)`, `listServers()`, `setActiveServer(url)`. All read/write `legato:servers` and `legato:active-server` in localStorage.
- [x] 3.2 When removing the active server, auto-revert to origin (empty string).

## 4. Settings Page — Server Management

- [x] 4.1 Add "Servers" section to `Settings.tsx` with: list of configured servers (name, URL, delete button), "Add server" form (name + URL inputs, add button), visual indicator for the active server.
- [x] 4.2 Clicking a server in the list sets it as active. "Local" entry always present at the top, representing the origin.

## 5. Sidebar Server Switcher

- [x] 5.1 Add server indicator to sidebar footer in `Layout.tsx` (or the sidebar area of `AgentSidebar.tsx`): show active server name, clickable.
- [x] 5.2 On click, show a popover listing all servers + "Local". Selecting one calls `setActiveServer()` which triggers WebSocket reconnect and agent list refresh.
- [x] 5.3 Hide sidebar switcher when no remote servers are configured (only origin — no need to show a switcher).

## 6. Connection Error Feedback

- [x] 6.1 Update `OfflineOverlay.tsx`: when the active server is not the origin and connection fails, show additional hint text ("Check that the server is running and its TLS certificate is trusted on this device").
- [x] 6.2 Pass active server info (is remote, server name) to `OfflineOverlay` via context or props.

## 7. Integration Verification

- [ ] 7.1 Manual test: add a remote server URL in Settings, switch to it, verify agent list loads from remote, terminal streaming works cross-origin.
- [ ] 7.2 Manual test: switch back to Local, verify origin behavior unchanged.
- [ ] 7.3 Manual test: kill origin legato, verify PWA loads from SW cache, can switch to remote server and operate.
