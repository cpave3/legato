# OpenCode Go usage screenshots

- `before-settings.png`: main at 7d7de930, isolated scratch database, subscription readers disabled.
- `after-fixture-settings.png`: branch Settings › Usage, all runtime readers disabled, no key configured.
- `after-fixture-usage.png`: branch Usage card, `/api/usage` intercepted in Playwright with synthetic account percentages and reset times. The catalog comes from the real scratch API. The visible banner labels the synthetic data.

No live subscription credentials or provider requests were used for these captures. Before and after use different viewport heights (900 and 1000 px). These screenshots verify presentation, not live subscription readings; HTTP fixtures and focused runtime/API tests cover the reader path separately.
