# React Frontend Summary (`web/`)

The frontend is a React 19 SPA built with **Vite** and **TypeScript**. Styling uses **TailwindCSS 4** (+ `tailwind-merge`/`class-variance-authority` for component variants). Key libraries include **React Router DOM** for routing, **xterm.js** (`@xterm/xterm`) for terminal emulation, **html5-qrcode** for QR pairing, **lucide-react** for icons, and **Radix UI** primitives. **vite-plugin-pwa** turns the SPA into an installable PWA with Workbox precaching and offline support. Tests run via **Vitest**.

## Source Layout (`web/src/`)

- **pages/** — Top-level route components: `Agents.tsx` (main terminal + agent list), `Board.tsx` (keyboard-driven kanban board), and `Settings.tsx` (appearance, pairing, CA download).
- **components/** — Reusable UI pieces: `TerminalPanel` (xterm.js with resize/touch scroll), `PromptBar` (action buttons, macro dropdown, prompt actions), `AgentSidebar` (agent list with sparklines), `TokenPrompt` (auth modal), `OfflineOverlay` (disconnect overlay), `QRScanner` (camera pairing), `Layout` (shell with nav), plus `PlanApprovalModal`, `StartSwarmModal`, etc. The `board/` subdir holds kanban-specific modals/overlays (`BoardCard`, `CreateTaskModal`, `ImportRemoteModal`, etc.).
- **hooks/** — Custom React hooks: `useWebSocket.ts` (WS lifecycle + reconnect), `useServer.ts` (server URL management), `useSwarmEvents.ts` (swarm inbox polling), `useBoard.ts` (board data fetching), `useToast.tsx`.
- **lib/** — Utility modules: `api.ts` (fetch wrapper adding base URL + auth), `auth.ts` (localStorage token helpers), `utils.ts`, `swarm.ts`, `board.ts`, `board-types.ts`.
- **assets/** — Static images/icons.
- **App.tsx / main.tsx / index.css** — Entry points and global styles.

## Backend Integration Points

Communication with the Go backend happens over two channels:

- **REST API** — All API calls go through `apiFetch()` in `lib/api.ts`, which prepends the active server base URL and injects an `Authorization: Bearer <token>` header from `lib/auth.ts`. Endpoints include `/api/settings`, `/api/agents`, `/api/board`, `/api/macros`, and swarm management routes.
- **WebSocket** — `useWebSocket.ts` opens a connection to `/ws`, appending `?token=<token>` for auth. It provides a context with exponential-backoff reconnect, a pub/sub message bus, and a `connected` flag consumed by `OfflineOverlay`. Messages types include `agent_output`, `agents_changed`, `prompt_state`, `plan_proposed`, `plan_verdict`, `send_keys`, `resize`, and `cards_changed`.
- **Auth** — A per-server Bearer token is stored in `localStorage` (`legato:token:<url>`). On app load, `App.tsx` health-checks `/api/settings`; a 401 triggers the `TokenPrompt` modal. Token invalidation forces a page reload and re-prompt.

The built assets are compiled to `internal/server/static/dist/` and embedded into the Go binary via `embed.FS`.
