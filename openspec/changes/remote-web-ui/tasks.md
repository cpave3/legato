## 1. Engine Layer — Tmux SendKeys + Prompt Detection

- [x] 1.1 Add `SendKeys(name, keys string) error` method to `tmux.Manager` — wraps `tmux send-keys -t <name> -- <keys>`. Add corresponding method to `TmuxManager` interface in service layer. Test with mock exec.
- [x] 1.2 Create `internal/engine/prompt/` package with `Detect(output string) PromptState` function. Define `PromptType` enum (`ToolApproval`, `PlanApproval`, `FreeText`, `Working`) and `PromptState` struct (type, context string, actions). Strip ANSI before matching.
- [x] 1.3 Write comprehensive tests for prompt detection — real Claude Code output samples for each prompt type, ANSI-laden input, edge cases (empty output, partial prompts).

## 2. Server Layer — REST API + WebSocket

- [x] 2.1 Expand `server.New()` constructor to accept `AgentService` and `TmuxManager` (nil-safe) alongside existing `BoardService`. Update existing health handler and tests.
- [x] 2.2 Add `GET /api/agents` REST endpoint — calls `AgentService.ListAgents()`, returns JSON array. Add `GET /api/tasks` endpoint — calls `BoardService` to return tasks grouped by column.
- [x] 2.3 Add WebSocket endpoint at `GET /ws` using `nhooyr.io/websocket`. Implement hub pattern: track connected clients, fan out messages. Send initial `agent_list` on connect.
- [x] 2.4 Implement agent output subscription — on `subscribe_agent` message, start a goroutine polling `tmux.Capture()` at 200ms for that client+agent. Send full pane on first capture, then only on change. Stop on `unsubscribe_agent` or client disconnect.
- [x] 2.5 Implement `send_keys` message handler — validate agent exists and is alive via `AgentService`, then call `tmux.SendKeys()`. Return error message over WebSocket on failure.
- [x] 2.6 Integrate prompt detection — after each capture, run `prompt.Detect()` on the output. Send `prompt_state` WebSocket message when prompt type changes.
- [x] 2.7 Add IPC client mode — when web server is running alongside TUI, connect to TUI's IPC socket and translate IPC messages to WebSocket broadcasts (`agents_changed`). In standalone mode, create own IPC server socket.
- [x] 2.8 Add SPA serving — embed `internal/server/static/dist/` via `embed.FS`. Serve static files, fall back to `index.html` for unmatched paths (client-side routing support). API and WebSocket routes take precedence.

## 3. CLI — Serve Subcommand

- [x] 3.1 Add `legato serve [--port <port>]` subcommand to `cmd/legato/main.go`. Wire up store, board service, agent service, tmux manager. Default port 3000. Print URL on start. Handle SIGINT/SIGTERM for graceful shutdown.

## 4. Frontend — Project Setup

- [x] 4.1 Initialize Vite + React + TypeScript project in `web/`. Configure Tailwind CSS v4. Add shadcn/ui with Radix. Add xterm.js + xterm-addon-fit. Configure `vite.config.ts` to output to `../internal/server/static/dist/`.
- [x] 4.2 Set up `web/src/hooks/useWebSocket.ts` — WebSocket connection manager with auto-reconnect (exponential backoff: 1s, 2s, 4s, max 30s), connection state tracking, JSON message send/receive helpers.

## 5. Frontend — App Shell

- [x] 5.1 Create app shell with React Router — sidebar nav with "Agents" and "Board" links, active state highlighting, connection status indicator (green/red dot from WebSocket hook). Root `/` redirects to `/agents`.
- [x] 5.2 Create board stub page at `/board` — placeholder card with "Board — Coming Soon" message.

## 6. Frontend — Agents Page

- [x] 6.1 Build agent list sidebar — fetches from `GET /api/agents`, subscribes to `agents_changed` WebSocket messages for live updates. Each entry shows task ID, title, activity badge (working=green, waiting=yellow, idle=gray), and command. Click to select.
- [x] 6.2 Build terminal output panel using xterm.js — on agent select, send `subscribe_agent` via WebSocket, write `agent_output` messages to xterm instance. Handle `full: true` (reset + write) vs `full: false` (append). Unsubscribe on agent switch.
- [x] 6.3 Build prompt action bar — reads `prompt_state` WebSocket messages for selected agent. Renders: buttons for `tool_approval`/`plan_approval`, text input + send for `free_text`, "Working..." indicator for `working`. Button clicks and text submit send `send_keys` via WebSocket.
- [x] 6.4 Implement responsive layout — sidebar + terminal side-by-side above 768px, sidebar collapses to dropdown selector below 768px.

## 7. TUI Integration

- [x] 7.1 Add `W` keybinding to TUI app model — starts/stops the web server in a background goroutine. Pass existing services to server constructor. Show "Web: :3000" in status bar when running. Server stops on TUI quit.

## 8. Build Pipeline

- [x] 8.1 Add `task web:build` command — runs `npm run build` in `web/`. Add `task web:dev` for frontend dev mode. Update `task build` to run `web:build` before `go build`. Create `internal/server/static/embed.go` with `//go:embed dist/*` directive.
- [x] 8.2 Add `.gitignore` entry for `internal/server/static/dist/`. Add fallback `embed.go` that provides an empty FS when dist doesn't exist (so `go build` works without Node for dev).
