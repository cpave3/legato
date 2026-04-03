## Context

The web server exposes REST endpoints and WebSocket connections that give full control over tmux agent sessions — including sending keystrokes, killing processes, and reading terminal output. Currently there is no authentication; anyone who can reach the port has full access. The planned multi-server feature (separate change) makes this more acute since users will explicitly be accessing servers across the network.

The server already uses self-signed TLS (auto-generated CA + server cert), so the transport is encrypted. What's missing is identity verification — proving the client is authorized.

## Goals / Non-Goals

**Goals:**
- Prevent unauthorized access to the web UI and API
- Zero-config for the server (token auto-generated on first run)
- One-time pairing per device per server (token stored client-side)
- QR code pairing for mobile devices (scan = add server + authenticate)
- Copy-paste fallback for desktop/SSH

**Non-Goals:**
- User accounts or multi-user access control
- Token rotation or expiry (static token, regenerate manually if compromised)
- OAuth, SSO, or external identity providers
- Encrypting the token at rest (it's in a plaintext file, same as TLS keys)
- Protecting the IPC socket (local only, separate threat model)

## Decisions

### 1. Token format and storage

**Decision**: 32 bytes from `crypto/rand`, hex-encoded (64 chars). Stored at `<dataDir>/auth-token` (alongside `certs/` and `legato.db`). Generated on first server start if absent.

**Why**: `crypto/rand` is the standard for security tokens in Go. 32 bytes = 256 bits of entropy — infeasible to brute force. Hex encoding is simple, no escaping issues in URLs/headers. File in the data dir follows the existing pattern for auto-generated artifacts (certs).

**Alternative considered**: Store in config.yaml. Rejected — config is user-edited and may be version-controlled. The token is a secret.

### 2. Auth middleware placement

**Decision**: HTTP middleware wrapping the mux handler in `server.go`. Checks `Authorization: Bearer <token>` header on every request. Exempt: `GET /health` (monitoring), `OPTIONS` (CORS preflight). For WebSocket upgrades, check `?token=<token>` query parameter (browsers can't set headers on WebSocket connections).

**Why**: Middleware is the standard pattern — one place, all endpoints covered. The exemptions are minimal and well-justified (health is read-only board state, OPTIONS has no body).

**Alternative considered**: Per-handler auth checks. Rejected — easy to forget on new endpoints.

### 3. Client-side token storage

**Decision**: localStorage key `legato:token:<serverUrl>` (or `legato:token:local` for origin). When a server responds with 401, the UI shows a token input modal. On success, token is stored and all subsequent requests include it.

**Why**: Per-server tokens support the multi-server feature naturally. localStorage persists across sessions. The 401 → prompt flow is standard and self-explanatory.

### 4. QR code pairing

**Decision**: `legato pair` CLI command (and a TUI keybinding) renders a QR code in the terminal using Unicode block characters. The QR payload is a URI: `legato://pair?url=<serverUrl>&token=<token>`. The PWA has a "Scan QR" button (in the add-server flow) that opens the device camera via `navigator.mediaDevices.getUserMedia` and decodes the QR with a JS library.

**Why**: QR eliminates typing a 64-char token on a phone. The URI format carries both the server URL and token, so scanning is a one-step add + authenticate. Terminal QR rendering is well-established (many Go libraries support it). The JS QR scanner runs in the PWA without native app capabilities.

**Go library**: `skip2/go-qrcode` — mature, MIT licensed, renders to `io.Writer`. For terminal output, render to string using Unicode half-blocks (▄▀█). Alternatively `mdp/qrterminal` which renders directly to terminal.

**JS library**: `html5-qrcode` — wraps `getUserMedia` + decoding, MIT licensed, works in PWA context.

### 5. Token display fallback

**Decision**: `legato auth token` prints the raw token to stdout. `legato auth regenerate` generates a new token (invalidating all paired devices). In the TUI, a keybinding shows the token briefly in the status bar or a modal.

**Why**: Not everyone has a camera (desktop, SSH). Copy-paste is the universal fallback.

## Risks / Trade-offs

- **Token in URL for WebSocket** — The `?token=` query param may appear in server logs and browser history. → Mitigation: The server is self-hosted, logs are local. The token doesn't appear in the address bar (WebSocket URL is programmatic). This is the same approach Jupyter, Grafana, and others use for WS auth.
- **No token rotation** — If the token is leaked, it's valid forever until manually regenerated. → Mitigation: The threat model is a home/office LAN with TLS. Physical access to the machine (to read the token file) implies full access anyway. `legato auth regenerate` is the escape hatch.
- **QR code readability** — Terminal QR codes depend on font size, terminal width, and contrast. Small terminals may produce unreadable QR codes. → Mitigation: Use the lowest QR error correction level for smaller codes. Fall back to copy-paste if scanning fails. Show the token as text below the QR.
- **Camera permissions** — PWA needs camera access for scanning. User may deny. → Mitigation: Fall back to manual paste input. The scan button clearly indicates camera use.
