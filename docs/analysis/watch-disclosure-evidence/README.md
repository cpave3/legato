# Watched-agent disclosure evidence — qykhugdr

The isolated Vite fixture mounts the real `NativeAgentPanel`. It stubs all API responses and conversation operations. The browser script blocks API requests and external traffic and intercepts the fixture WebSocket. No running Legato or real agent was tested or restarted.

## Checks

- Focused Vitest files: `NativeAgentPanel.test.tsx`, `legatoMessage.test.ts`, `agentGlance.test.ts`: **148 passed**, two workers.
- Theme utility check, TypeScript project build, ESLint on changed TypeScript files, and Vite production build: **passed**.
- `verify-watch-disclosures.mjs`: **passed** at 1280, 390 and 320 pixels. See `results.json`.
- Browser: Chromium 1247 through installed Playwright. The preferred distrobox was unavailable. A temporary HOME allowed the browser to start without writing to the operator's home.
- Keyboard Tab reaches the disclosure with visible focus; Enter expands; Space collapses. Both single and batched rows start closed.
- Long titles wrap when expanded; no horizontal viewport overflow. Full literal URL/Markdown/angle-bracket text and attribution survive expansion. Details remain scrollable.
- Separate permission and question cards remain visible when notifications are closed. The panel test also submits a denial and an answer without opening the batch.
- Normal user prose containing the tag and assistant text beginning with the tag remain normal messages. Existing peer/change-set behavior passes.

## Screenshots

| State | Desktop | Mobile |
| --- | --- | --- |
| Collapsed, keyboard focus | [1280px](1280-collapsed.png) | [390px](390-collapsed.png) |
| Expanded, long content | [1280px](1280-expanded.png) | [390px](390-expanded.png) |

## Reproduce

Start only the fixture server in this job clone:

```sh
cd web
./node_modules/.bin/vite --host 127.0.0.1 --port 5198 --strictPort
```

In a second terminal at the repository root:

```sh
node scripts/verify-watch-disclosures.mjs http://127.0.0.1:5198 /tmp/watch-disclosures
```

Set `PLAYWRIGHT_MODULE` and `CHROMIUM_PATH` if Playwright and Chromium are not on the normal module/browser paths. Stop the fixture server after verification.

## Limits

No full suites ran locally; CI owns broad checks. Backend watch delivery, wake behavior, and rail logic are unchanged and were not exercised against live agents. URLs stay complete literal text, not new clickable anchors, as approved in the plan.

Test audit: four added declarations reviewed and kept (two panel scenarios, one summary table, one glance scenario). Each covers a separate boundary or format risk; no test-only production seams were added. The single-panel test failed before classification changed; summary cases failed before the titleless-task colon fix. User-visible new copy is limited to task/agent/event counts and the `Agent update` fallback.
