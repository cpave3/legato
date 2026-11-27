# Named Claude accounts — synthetic UI evidence

These Playwright/Chromium component-fixture screenshots use no live Legato instance, login or account data.

- **Before:** actual source exported from merged baseline `bf94ed1d`.
- **After:** account-aware working tree based on `4ae89713`, captured before the final commit. Source hashes, full worktree status, fixture request evidence and capture details are embedded in each PNG's `LegatoFixtureEvidence` metadata.
- Desktop viewport: 1440 × 1000. Mobile viewport: 390 × 844.
- All API responses were synthetic. No external requests, page errors or horizontal overflow were observed. The temporary browser and fixture servers were removed.
- These images prove component rendering, not backend launch/account integration. Go tests separately exercise fake child processes, real SQLite, fixture HTTP, and real bwrap isolation.

The account labels are fictional. The default retains its existing tool label; named accounts use their configured labels. Agent info separates Account from Agent tool. Profiles select independent author/reviewer accounts, and the spawn dialog selects a named account on desktop and mobile.
