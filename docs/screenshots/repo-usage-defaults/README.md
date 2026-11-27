# Repository usage defaults screenshots (szmq9mjm)

Captured with headless Chromium at 2× from a throwaway harness page (not committed) on a scratch Vite server. Playwright intercepted `/api/usage` and `/api/config/lists` with synthetic profiles (Default, Claude Work, Codex), a synthetic `work` account, readings and policy. No live Legato, settings, accounts, credentials or provider requests were used.

- `after-usage-card.png`: the Usage card. The global profile is Default; **New work by repository** shows `autopilot-dev/autopilot`, whose default is Claude Work, switched to Codex by the rule `work almost full`.
- `after-usage-pin.png`: the pin form, which says a pin applies to all new work in every repository, over repository defaults and rules.
- `after-settings-lists.png`: Settings › Usage › Profiles and rules, with the new **Repository defaults** list.
- `after-settings-repository.png`: adding a repository default.
- `after-settings-rule.png`: a rule limited to work defaulting to Claude Work (**Only for work defaulting to**).

These verify presentation only. Behaviour is covered by `web/src/components/__tests__/UsageStrip.test.tsx` and `ListSettings.test.tsx`, and by the Go tests.
