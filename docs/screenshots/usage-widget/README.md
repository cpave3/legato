# Usage card screenshots (LEG-666)

Captured with headless Chromium at 2× from a throwaway harness page. The harness rendered the component in a simplified agents sidebar on a scratch Vite server. It intercepted `/api/usage` with synthetic accounts (Claude Work, Claude Default, Codex, OpenCode Go), percentages, reset times and policy. No live Legato, credentials or provider requests were used. The harness was not committed.

- `before-desktop-open.png`: main at 5f7a8470, the widget opened inline. It pushes the agent list down and shows the full timestamp and the raw credit balance.
- `after-desktop-collapsed.png`: the collapsed card, 800×900.
- `after-desktop-popout.png`: the Usage card open beside the sidebar.
- `after-desktop-pin.png`: the pin form open (`?pin=Claude Work`).
- `after-desktop-states.png`: a failed profile switch, a stale Codex reading and a signed-out Claude Default at once.
- `after-mobile-collapsed.png`, `after-mobile-sheet.png`: the phone layout, 390×844, and the bottom sheet.

These screenshots verify presentation only. The component tests in `web/src/components/__tests__/UsageStrip.test.tsx` cover the behaviour.
