## Why

The current prompt action system sends arrow keys (`Down`, `Down Down Enter`) to navigate Claude Code's option list. This is fragile — if the user manually moves the cursor before clicking a button, the wrong option gets selected. Claude Code supports pressing number keys (1, 2, 3...) to select numbered options directly, which is deterministic regardless of cursor position.

## What Changes

- Update `prompt.Detect` to parse numbered option lines (e.g. `1. Yes`, `2. Yes, and don't ask again`) from the terminal output and build actions from them dynamically
- Change action `Keys` from arrow-based sequences (`"Down Enter"`) to number key presses (`"1"`, `"2"`, `"3"`)
- Remove hardcoded action lists for tool/plan approval in favor of dynamically detected options
- Preserve fallback behavior for prompts that don't use numbered options (plan approval uses Accept/Reject without numbers)

## Capabilities

### New Capabilities

_(none — this modifies existing prompt detection, no new capability boundary)_

### Modified Capabilities

- `server-stub`: The prompt detection subsystem (`prompt.Detect`) changes its parsing logic and action key format. Detection still returns the same `PromptState` struct, but actions are now dynamically extracted from numbered lines rather than hardcoded.

## Impact

- `internal/engine/prompt/detect.go` — parsing logic rewritten to extract numbered options
- `internal/engine/prompt/detect_test.go` — test cases updated for new detection and key format
- `internal/server/ws.go` — no changes needed (already handles single-key sends like `"1"`)
- `web/src/components/PromptBar.tsx` — no changes needed (already renders action labels and sends action keys)
- `web/src/hooks/useWebSocket.ts` — no changes needed (PromptState interface unchanged)
