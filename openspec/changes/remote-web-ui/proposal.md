## Why

Legato's TUI is powerful but tethered to a terminal session. When agents are running long tasks, there's no way to monitor or interact with them from a phone or another device. A lightweight web UI served by Legato itself would let users check agent status, read output, and send input to tmux sessions from any browser on the local network — particularly useful over Tailscale for mobile access.

## What Changes

- New `legato serve` CLI subcommand that starts the HTTP/WebSocket server on a configurable port
- New keyboard shortcut in the TUI (`S`) to start/stop the web server in the background
- WebSocket-based real-time communication between Go server and React frontend, using the existing IPC socket protocol to stay in sync with the running TUI
- Embedded React SPA served by the Go binary (built at compile time via `embed.FS`)
- Agent view as the primary UI: list agents, stream terminal output, detect prompt state (approval vs free text), send input to tmux sessions
- Board view as a stub page (placeholder for future implementation)
- Regex-based prompt detection to classify Claude Code's waiting state and surface appropriate UI controls (approve/reject buttons vs text input)

## Capabilities

### New Capabilities
- `web-server`: HTTP/WebSocket server that serves the React SPA and exposes REST+WS endpoints for agents, tasks, and tmux interaction
- `web-ui-agents`: React-based agent monitoring page — agent list, streamed terminal output, prompt detection with contextual input controls (buttons for approvals, text field for free input)
- `web-ui-shell`: React app shell with routing, navigation, and board stub page
- `prompt-detection`: Regex-based classification of Claude Code terminal output to detect prompt type (tool approval, plan approval, free text input, agent working) and surface appropriate UI actions

### Modified Capabilities
- `server-stub`: Expand from health-check-only stub to full HTTP/WebSocket server with agent and task endpoints
- `legato-cli`: Add `serve` subcommand for standalone server mode
- `tui-app-shell`: Add shortcut to start/stop embedded web server from within the TUI

## Impact

- **New dependencies**: React, TypeScript, Tailwind CSS, shadcn/ui, Radix UI (frontend); `gorilla/websocket` or `nhooyr.io/websocket` (Go); build tooling (vite/esbuild for frontend compilation)
- **Build pipeline**: Frontend must be built before Go binary, output embedded via `embed.FS` in `internal/server/`
- **Existing code**: `internal/server/` expands significantly; `internal/engine/tmux/` gains `SendKeys` method; `cmd/legato/` gains `serve` subcommand; `internal/tui/app.go` gains server lifecycle management
- **IPC**: Web server connects as an IPC client to receive real-time updates from the TUI instance, same protocol as existing CLI commands
