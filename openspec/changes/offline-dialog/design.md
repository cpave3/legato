## Context

The current `OfflineOverlay` is a sticky top banner. The user-provided screenshot, however, shows a full-screen "Connection Lost" overlay that blocks the plan queue. This proposal treats the screenshot as the current undesired state and replaces it with a centered modal dialog that does not block the underlying page.

## Goals / Non-Goals

**Goals:**
- Persistent offline indication that remains visible while disconnected.
- Dismissible so the user can still see cached UI state.
- Retains all existing offline actions: retry, switch server, add/scan server, open settings, TLS hint.
- Keeps the current 2-second delay before showing to avoid flashes on slow initial connections.

**Non-Goals:**
- Offline data editing or optimistic mutations.
- Changing the WebSocket reconnection strategy.
- Mobile app changes (the mobile app embeds the web view; the dialog will appear there automatically once the web client is updated).

## Decisions

### 1. Centered fixed dialog with backdrop

Use a fixed-position overlay with a dark, partially-transparent backdrop. The dialog itself is centered, has a clear title, a reconnecting message, and an action footer. Unlike the full-screen overlay, it does not replace the page content — the board/agents UI remains visible behind it.

### 2. Dismissible but reappears if still offline

A close button (×) hides the dialog. It reappears automatically on the next disconnect event. This lets users inspect cached state without losing awareness.

### 3. Actions grouped in dialog footer

Existing action links/buttons move into the dialog footer:
- Retry
- Add server
- Scan QR
- Go to Settings
- Server switcher (Local / configured remotes)

### 4. Keep server switcher behavior

Clicking a server calls `setActiveServer` exactly as today; the underlying provider handles navigation/reload.

### 5. Keep the 2-second delay

Preserve `delayMs` to avoid a dialog flash on initial page load when the WebSocket is still opening.

## Risks / Trade-offs

**[Dismissed dialog hides offline state]** → The dialog reappears on the next disconnect; the sidebar/server dot also continues to show red when disconnected.

**[Backdrop click could accidentally dismiss]** → Only the explicit close button dismisses; clicking the backdrop does not close the dialog, so users don't lose the persistent indicator.

**[Z-index conflicts with other overlays]** → Use `z-50` for the dialog container, same as existing modals and dropdowns. The dialog sits above page content but below tooltips if any are added later.
