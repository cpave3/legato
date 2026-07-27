## Why

The Legato web client currently shows a full-screen "Connection Lost" overlay when the WebSocket drops. The overlay blocks the underlying board/agents UI and pushes server-switching controls into a disconnected context. Users have reported that this looks bad and prevents them from reading state that may already be cached locally.

A persistent, dismissible dialog/popup keeps the offline state visible and actionable without obscuring the app. It matches the pattern already used for `PlanApprovalModal` and other modal chrome.

## What Changes

- Replace the full-screen/sticky `OfflineOverlay` banner with a centered, persistent offline dialog.
- The dialog remains open while `connected === false` and can be dismissed to inspect cached UI state.
- It still exposes: reconnect status, server switcher (Local / remote servers), "Add server", "Scan QR", "Go to Settings", "Retry", and TLS hint for remote servers.
- The dialog is rendered inside `Layout` so it is scoped to the authenticated app area and can overlay `Outlet` content.

## Capabilities

### New Capabilities
- `offline-dialog`: Persistent, dismissible offline-state dialog for the web client.

### Modified Capabilities
- `offline-overlay`: Replaced by `offline-dialog`; existing `OfflineOverlay` component removed.

## Impact

- `web/src/components/OfflineOverlay.tsx` — removed, replaced by `web/src/components/OfflineDialog.tsx`.
- `web/src/App.tsx` — stop rendering `OfflineOverlay` at the top level.
- `web/src/components/Layout.tsx` — render `<OfflineDialog />` alongside `PlanApprovalModal`.
- `web/src/components/__tests__/OfflineOverlay.test.tsx` — replaced by tests for `OfflineDialog`.
