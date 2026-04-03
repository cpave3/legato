## Why

The web UI currently has no authentication — anyone who can reach the server's port gets full access to terminal streaming, send-keys, and agent management. This is a remote shell. As multi-server support makes cross-machine access explicit, unauthenticated access becomes a real security concern on shared networks.

## What Changes

- Auto-generate a random auth token on first run, stored in the legato data directory
- Server rejects all API/WebSocket requests without a valid `Authorization: Bearer <token>` header (WebSocket uses `?token=` query param since browsers can't set WS headers)
- Web UI prompts for the token on first connect to any server; stores it in localStorage per server URL
- TUI/CLI `legato pair` command renders a QR code in the terminal encoding the server URL + token, scannable by the PWA's camera to add and authenticate in one step
- Manual token display via `legato auth token` for copy-paste fallback (desktop browsers, SSH)
- New Go dependency: QR code generation library for terminal rendering

## Capabilities

### New Capabilities
- `web-auth`: Token generation, server-side auth middleware, client-side token storage and prompt, QR code pairing flow

### Modified Capabilities
- `server-stub`: All HTTP endpoints and WebSocket upgrades require valid auth token. Health endpoint exempted for monitoring.

## Impact

- **Backend**: `internal/server/` (auth middleware), `internal/engine/auth/` (token generation/storage), `cmd/legato/` (pair/auth CLI subcommands)
- **Frontend**: `useWebSocket.ts` (token in WS URL), all `fetch()` calls (Authorization header), new token prompt component, QR scanner component
- **Dependencies**: Go QR library (e.g., `skip2/go-qrcode` or similar), JS QR scanner library (e.g., `html5-qrcode`)
- **Data**: New file `~/.local/share/legato/auth-token` (auto-generated, 32-char random)
- **No database changes**
- **Backwards compatible**: If token file doesn't exist (fresh install), it's generated automatically. Existing installs get a token on next startup.
