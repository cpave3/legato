## 1. Token Generation (Engine Layer)

- [x] 1.1 Create `internal/engine/auth/` package with `EnsureToken(dataDir string) (string, error)` — reads `<dataDir>/auth-token` if it exists, otherwise generates 32 bytes from `crypto/rand`, hex-encodes, writes to file, and returns the token string.
- [x] 1.2 Add `RegenerateToken(dataDir string) (string, error)` — generates a new token and overwrites the existing file.
- [x] 1.3 Write tests for EnsureToken (first run creates, second run reads back same value) and RegenerateToken (returns different token, old one gone).

## 2. Server Auth Middleware

- [x] 2.1 Add `Server.SetAuthToken(token string)` method that stores the token on the server struct.
- [x] 2.2 Create auth middleware wrapper in `internal/server/server.go` that checks `Authorization: Bearer <token>` on all requests. Exempt `GET /health` and `OPTIONS` method. Return 401 with `{"error":"unauthorized"}` on failure.
- [x] 2.3 For the `/ws` endpoint, extract token from `?token=` query parameter before the WebSocket upgrade. Reject with 401 if missing/invalid.
- [x] 2.4 Write tests: 401 without token, 401 with wrong token, 200 with correct token, health endpoint works without token, OPTIONS works without token, WebSocket rejects without token query param.

## 3. Wire Token into Server Startup

- [x] 3.1 In `cmd/legato/main.go` `runServe()`: call `auth.EnsureToken(dataDir)`, pass result to `srv.SetAuthToken(token)`.
- [x] 3.2 In `cmd/legato/main.go` `runTUI()` (web auto-start path): same — call `auth.EnsureToken(dataDir)`, pass to `webSrv.SetAuthToken(token)`.

## 4. CLI Commands

- [x] 4.1 Add `legato auth token` subcommand: loads config, resolves data dir, reads `<dataDir>/auth-token`, prints to stdout.
- [x] 4.2 Add `legato auth regenerate` subcommand: calls `auth.RegenerateToken(dataDir)`, prints new token, warns that all paired devices must re-authenticate.
- [x] 4.3 Add `legato pair` subcommand: reads token, builds `legato://pair?url=<serverUrl>&token=<token>` URI, renders QR code to terminal using a Go QR library. Print raw token below QR as fallback. Accept `--port` flag (default from config).
- [x] 4.4 Add Go dependency for terminal QR rendering (e.g., `mdp/qrterminal` or `skip2/go-qrcode` with custom terminal renderer).

## 5. Frontend — Token Storage and Auth Header

- [x] 5.1 Create token storage helpers: `getToken(serverUrl): string | null`, `setToken(serverUrl, token)`, `clearToken(serverUrl)` using localStorage key `legato:token:<serverUrl>` (use `legato:token:local` for origin).
- [x] 5.2 Update all `fetch()` calls (in Agents.tsx, Settings.tsx, and any future `apiFetch` helper) to include `Authorization: Bearer <token>` header when a token is stored for the active server.
- [x] 5.3 Update `useWebSocket.ts` to append `?token=<token>` to the WebSocket URL when a token is stored.

## 6. Frontend — Token Prompt

- [x] 6.1 Create `TokenPrompt` component: modal with text input for pasting a token, submit button, error state for invalid tokens. Shown when any API call returns 401.
- [x] 6.2 On submit: store token via `setToken()`, retry the failed request. On 401 again: show "Invalid token" error.
- [x] 6.3 Handle stored-token-becomes-invalid: when a request with a stored token gets 401, clear the token and show the prompt.

## 7. Frontend — QR Scanner

- [x] 7.1 Add JS QR scanner dependency (e.g., `html5-qrcode`).
- [x] 7.2 Create `QRScanner` component: opens camera, decodes QR codes, parses `legato://pair?url=...&token=...` URIs. Returns parsed `{url, token}` on success.
- [x] 7.3 Integrate into the add-server flow (Settings page or server switcher): "Scan QR" button opens the scanner. On successful scan, add server to registry and store token. On camera denied, show fallback message with manual entry.
- [x] 7.4 Handle invalid QR codes (non-legato URIs): show error, keep scanning.

## 8. TUI Pairing Keybinding

- [ ] 8.1 Add a TUI keybinding (e.g., `P` from board view) that shows a full-screen QR code overlay with the pair URI and raw token text. Press any key to dismiss.
- [ ] 8.2 Reuse the same QR rendering logic from the CLI `pair` command.

## 9. Integration Verification

- [ ] 9.1 Manual test (pending): fresh install → start server → token auto-generated → web UI shows token prompt → paste token → authenticated → full access.
- [ ] 9.2 Manual test: `legato pair` → scan QR from phone PWA → server added + authenticated in one step.
- [ ] 9.3 Manual test: `legato auth regenerate` → existing web session gets 401 → re-prompted for new token.
- [ ] 9.4 Manual test: `/health` endpoint accessible without token (monitoring).
