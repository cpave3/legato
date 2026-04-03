## Why

The PWA is served by a single legato TUI instance, binding it to one machine's agents and tasks. Users who run legato on multiple machines (desktop, laptop) want to monitor and control all instances from a single PWA without running a separate standalone server.

## What Changes

- Make the PWA's API/WebSocket base URL dynamic instead of hardcoded to `window.location`
- Add a server registry in the PWA (stored in localStorage) where users can name and save multiple legato server URLs
- Add a server switcher UI (sidebar indicator + Settings section) to select the active server
- Add CORS headers to the Go HTTP server so the PWA served from one origin can talk to another legato instance
- Default to the origin server (preserving current single-instance behavior)
- When the origin server is offline, the PWA loads from SW cache and can connect to an alternate server

## Capabilities

### New Capabilities
- `multi-server`: Server registry, active server selection, dynamic base URL routing, and CORS support for cross-origin PWA connections

### Modified Capabilities
- `server-stub`: Add CORS middleware to allow cross-origin API/WebSocket requests from PWA instances served by other legato servers

## Impact

- **Frontend**: `useWebSocket.ts` (dynamic WS URL), `fetch` calls throughout (dynamic base URL), new components for server switcher, Settings page additions
- **Backend**: `internal/server/server.go` (CORS middleware on HTTP handler)
- **No database changes** — servers are a purely client-side concept stored in localStorage
- **No protocol changes** — same REST + WebSocket API, just accessed cross-origin
- **TLS**: Each server has its own CA; user must trust each CA on their device independently
