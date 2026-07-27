## 1. Create OfflineDialog component

- [x] 1.1 Add `web/src/components/OfflineDialog.tsx` with a centered, fixed-position dialog. Include title, reconnecting message, TLS hint, and action buttons/links.
- [x] 1.2 Support `delayMs` prop (default 2000 ms) using the same visibility delay as `OfflineOverlay`.
- [x] 1.3 Read `connected`, `baseUrl`, `servers`, `activeServerName`, `isRemote`, and `setActiveServer` from `useWebSocket`/`useServer`.
- [x] 1.4 Add explicit close button; closing sets a local `isOpen` flag to false. The dialog reopens automatically when `connected` becomes false again after having been true, or on the initial disconnect after the delay.

## 2. Replace top-level OfflineOverlay with Layout-scoped dialog

- [x] 2.1 Remove `<OfflineOverlay />` from `web/src/App.tsx`.
- [x] 2.2 Add `<OfflineDialog />` to `web/src/components/Layout.tsx` alongside `<PlanApprovalModal />`.
- [x] 2.3 Delete `web/src/components/OfflineOverlay.tsx`.

## 3. Update tests

- [x] 3.1 Rename/replace `web/src/components/__tests__/OfflineOverlay.test.tsx` with `OfflineDialog.test.tsx`.
- [x] 3.2 Test that the dialog does not appear immediately and appears after the delay when disconnected.
- [x] 3.3 Test that the dialog does not appear when connected.
- [x] 3.4 Test server switching (Local + remotes) inside the dialog.
- [x] 3.5 Test active server styling.
- [x] 3.6 Test "Go to Settings" link when no remote servers exist.
- [x] 3.7 Test TLS hint for remote servers.
- [x] 3.8 Test that the dialog is dismissible and reappears when disconnected again.
- [x] 3.9 Test Retry button triggers `location.reload()`.

## 4. Specifications and documentation

- [x] 4.1 Add capability delta specs for the persistent offline dialog and modified multi-server error feedback.
- [x] 4.2 Update `docs/ai/web-ui.md` to describe the dialog lifecycle, actions, and responsive connection indicator.

## 5. Verification

- [x] 5.1 Run `cd web && npm run test`.
- [x] 5.2 Run `cd web && npm run build`.
- [ ] 5.3 Run `task check` (Go build/tests + web build if applicable). Blocked by pre-existing `unused` lint findings in `cmd/legato`; build, Go tests, and vet pass.
