## Context

The PWA is currently served by and bound to a single legato instance. All API calls use relative URLs (`/api/agents`, `/ws`) which resolve to the serving origin. Users running legato on multiple machines want to control all instances from one PWA.

The current frontend has no concept of a configurable base URL. The Go server has no CORS headers since the PWA always talks to its own origin.

## Goals / Non-Goals

**Goals:**
- PWA can connect to any legato server, not just its origin
- Server list managed entirely in the browser (localStorage)
- Zero config on the server side — CORS is always enabled
- Current single-instance UX preserved as the default

**Non-Goals:**
- Server discovery / mDNS / Bonjour — users manually add server URLs
- Cross-server aggregation (seeing agents from multiple servers at once)
- Shared authentication between servers
- Syncing tasks/state between servers

## Decisions

### 1. Dynamic base URL via React context

**Decision**: Create a `ServerContext` that provides `baseUrl` (for REST) and `wsUrl` (for WebSocket) to the app. All `fetch()` calls and the WebSocket connection read from this context.

**Why**: Avoids threading a base URL prop through every component. The context is set once when the user picks a server, and everything downstream uses it.

**Alternative considered**: Global variable or module-level constant. Rejected because it doesn't trigger React re-renders when the server changes.

### 2. Server registry in localStorage

**Decision**: Store servers as a JSON array in `localStorage.getItem("legato:servers")`. Each entry has `{name, url}`. The active server URL is stored in `legato:active-server` (empty string = origin).

**Why**: Consistent with existing localStorage patterns (`legato:glitch-effect`, `legato:prompt-detection`, etc.). No backend storage needed — servers are a per-device preference.

### 3. Permissive CORS on the Go server

**Decision**: Add a middleware that sets `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods`, and `Access-Control-Allow-Headers` on all responses. For WebSocket upgrades, the existing `InsecureSkipVerify: true` already allows any origin.

**Why**: The server runs on a local network with self-signed TLS. There's no security benefit from restricting origins — the TLS CA trust is the access control. Wildcard CORS is simplest.

**Alternative considered**: Reflecting the request `Origin` header back. More "correct" but adds complexity for no security gain in this context.

### 4. Server switcher in sidebar footer

**Decision**: Show the active server name (or "Local" for origin) as a small clickable indicator at the bottom of the sidebar, above the settings gear. Clicking opens a popover with the server list + "Add server" option. Settings page has the full CRUD for servers.

**Why**: Quick switching should be accessible from the main view, not buried in settings. The sidebar footer is visible on desktop and hidden on mobile (where the settings page is the fallback).

### 5. WebSocket reconnect on server switch

**Decision**: When the active server changes, close the current WebSocket and open a new one to the new server. The `useWebSocketProvider` hook watches the active server URL and reconnects.

**Why**: The WebSocket is the primary data channel. A server switch means completely new agents, tasks, and state. A clean disconnect + reconnect is the simplest approach.

## Risks / Trade-offs

- **TLS trust per server** — Each legato instance has its own self-signed CA. The user must install each CA on their device. If they haven't, the browser silently fails cross-origin HTTPS requests with no useful error. → Mitigation: Show a connection error with a hint to check TLS trust when a server is unreachable.
- **CORS wildcard** — `Access-Control-Allow-Origin: *` means any website could make requests to the legato server if it knew the URL. → Mitigation: The server is on a local network behind a self-signed cert. The CA trust requirement is the real access control. Document this in the CORS header comment.
- **Stale server entries** — Users may add servers that later go offline permanently. → Mitigation: Show connection status per server in the list. No auto-cleanup needed.
