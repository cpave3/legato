# Web request traffic — w155emy9

Measured 2026-08-04 with the production data-owner tree and the real provider/Layout/page hooks in `web/src/__tests__/requestTraffic.test.tsx`. The test mocks only HTTP and websocket browser boundaries, bypassing service-worker/browser caches. It mounts the same query owner and route consumers used by production.

## Board reload

| API GET | Before | After |
| --- | ---: | ---: |
| `/api/settings` | 3 | 1 |
| `/api/review/queue` | 1 | 0 |
| `/api/plans/queue` | 1 | 0 |
| `/api/workflow-runs` | 1 | 0 |
| `/api/attention` | 1 | 0 |
| `/api/agents` | 1 | 0 |
| `/api/board?workspace=all` | 1 | 1 |
| `/api/workflows` | 1 | 1 |
| `/api/navigation-summary` | 0 | 1 |
| **Total** | **10** | **4** |

The before count is derived from the pre-change production mount graph at the implementation base commit. The after count is asserted by the integration test and repeated identically across test runs.

## Active-agent event burst

The representative deterministic session is a synchronous burst of 20 `agents_changed` plus 20 `cards_changed` websocket notices while `/board` is active. This models repeated activity/card notices without depending on agent runtime duration.

| Refresh GET after initial snapshots | Before behavior | After measured |
| --- | ---: | ---: |
| `/api/board?workspace=all` | 20 | 1 |
| `/api/workflows` | 20 | 0 |
| `/api/workflow-runs` | 20 | 0 |
| `/api/agents` | 20 | 0 |
| `/api/navigation-summary` | 0 | 1 |
| **Total** | **80** | **2** |

Before counts are code-derived from the independent subscriptions mounted on Board/Layout before this change. After counts are asserted after the 75 ms coalescing window. A workflow-backed agent transition additionally emits targeted `workflow_changed`; that refreshes the observed run list/matching detail and summary once, never definitions.

## Reproduce

```bash
cd web
pnpm exec vitest run src/__tests__/requestTraffic.test.tsx
pnpm build
```

No running Legato process was stopped, restarted, or replaced for this capture.
