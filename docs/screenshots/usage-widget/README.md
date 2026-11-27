# Usage card screenshots (LEG-666)

Captured with headless Chromium at 2× from a throwaway harness page. The harness rendered the component in a simplified agents sidebar on a scratch Vite server. It intercepted `/api/usage` with synthetic accounts (Claude Work, Claude Default, Codex, OpenCode Go), percentages, reset times and policy. No live Legato, credentials or provider requests were used. The harness was not committed.

- `before-desktop-open.png`: main at 5f7a8470, the widget opened inline. It pushes the agent list down and shows the full timestamp and the raw credit balance.
- `after-desktop-collapsed.png`: the collapsed card, 800×900.
- `after-desktop-popout.png`: the Usage card open beside the sidebar.
- `after-desktop-pin.png`: the pin form open (`?pin=Claude Work`).
- `after-desktop-states.png`: a failed profile switch, a stale Codex reading and a signed-out Claude Default at once.
- `after-mobile-collapsed.png`, `after-mobile-sheet.png`: the phone layout, 390×844, and the bottom sheet.

## The phone's usage button (32g8tm5p)

Captured the same way (headless Chromium at 2×, synthetic `/api/usage`, no live Legato), from the committed fixture `web/src/test/usage-preview.html` on a scratch Vite server (`?usage=open`, `?tone=over|failed`). On a phone the card above is replaced by a gauge button in the picker bar, so `after-mobile-collapsed.png` shows the old phone layout.

- `after-mobile-compact.png`: the phone layout, 390×844: the gauge beside Ensemble, no card.
- `after-mobile-compact-over.png`: a window at 93%, a red dot on the gauge.
- `after-mobile-compact-failed.png`: a failed profile switch, an error mark in place of the gauge.
- `after-mobile-compact-sheet.png`: the button opens the existing bottom sheet.
- `after-desktop-unchanged.png`: the desktop sidebar card, 800×900, as before.

These screenshots verify presentation only. The component tests in `web/src/components/__tests__/UsageStrip.test.tsx` and the page tests in `web/src/pages/__tests__/AgentsUsage.test.tsx` cover the behaviour.
