# LEG-620 evidence

Screenshots from Playwright (Chromium, dark) against the web UI's Vite dev server. A scratch page
(`leg620-scratch-preview.tsx`, placed in `web/src/test/` while shooting and not committed) renders
`NativeAgentPanel` for a Claude Code agent with seven spawn calls. One is running. Six have
`subagent_finished` (reason `stop`). Its `backgroundJobs` prop is `subagentFleet()` from
`web/src/test/subagentFleet.ts`, which is empty for `?done`. `/api` is mocked in the page; no running
Legato was involved. `shoot.cjs <playwright-core dir> <out dir>` takes the four shots.
