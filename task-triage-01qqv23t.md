# `01qqv23t` — Native agent view collapses to message width

| | |
| --- | --- |
| **Type** | Bug — UI layout regression |
| **Verdict** | Real and implementation-ready. A 2026-08-21 layout change made the native Chimera panel a content-sized flex item in the normal single-agent view. |
| **Cause / Gap** | The selected-agent host became a horizontal flex container, but `NativeAgentPanel` does not claim the available width; assistant-only history therefore leaves the whole panel at its intrinsic content width until wider user content forces it to grow. |
| **Recommendation** | Make `NativeAgentPanel` explicitly fill its host (`w-full` and/or `flex-1`) and add a regression test for its root layout contract. |
| **Severity / Value** | Medium — the conversation and composer become difficult to use, but no data is lost and sending a message temporarily works around it. |
| **Confidence** | Confirmed by the layout path and introducing diff; not browser-reproduced because browser/E2E testing was outside this triage assignment. |
| **Effort** | Small — one targeted class change plus a component regression test. |
| **Source** | Legato task `01qqv23t`; reporter, browser, viewport, and exact build unknown. |

## What was reported

The reporter observed that a newly created agent's Legato web view occupies only a narrow horizontal strip while its conversation contains only agent messages. After the reporter sends a message, the view expands across the remaining space. They reported that this started within the last few days and did not prescribe a cause or fix.

The missing environment details are the browser, viewport/device, served frontend build, and whether the agent is native Chimera or terminal-backed. The state-dependent symptom nevertheless matches the native Chimera conversation path: it renders separate assistant and user bubbles (`web/src/components/NativeAgentPanel.tsx:649-668`), whereas terminal-backed agents render xterm in an absolutely positioned full-host element (`web/src/components/TerminalPanel.tsx:173-179`).

## Current behaviour

The Agents page gives its main content region the remaining application width (`web/src/pages/Agents.tsx:473-477`). The selected-agent host at `web/src/pages/Agents.tsx:477` is now both `flex-1` and a horizontal `flex` container. A non-swarm native agent is inserted directly into that host at `web/src/pages/Agents.tsx:504-515`.

`NativeAgentPanel`'s root sets height, minimum width, and column layout, but neither width nor flex growth:

```tsx
// web/src/components/NativeAgentPanel.tsx:637-638
return (
  <div className="relative flex h-full min-w-0 flex-col overflow-hidden bg-zinc-950">
```

As a direct child of the horizontal flex host, that root therefore uses its intrinsic width rather than filling the host. Assistant rows align to the start and cap only their maximum width (`web/src/components/NativeAgentPanel.tsx:659-665`); they do not establish a full-width panel. User rows align to the end (`web/src/components/NativeAgentPanel.tsx:650-656`), and the native composer includes a growing textarea plus a fixed 6rem action column (`web/src/components/NativeAgentPanel.tsx:989-1008`). Content can consequently increase the panel's intrinsic width after the user sends a message, producing the reported apparent expansion without any explicit state transition or resize handler.

The terminal path does not expose the same defect. `TerminalPanel` uses `absolute inset-0` at its root (`web/src/components/TerminalPanel.tsx:173-179`), so it fills the positioned host independently of flex-item sizing.

## Reproduction

Static reproduction path (confirmed):

1. Open `/agents` and select a non-swarm agent with `has_chimera_endpoint === true`; `AgentsPage` chooses `NativeAgentPanel` (`web/src/pages/Agents.tsx:504-515`).
2. The panel becomes a direct child of the horizontal flex host (`web/src/pages/Agents.tsx:477`).
3. Hydrate a snapshot whose visible conversation contains only assistant messages. Snapshot hydration intentionally skips the initial user prompt at index zero (`web/src/lib/nativeAgentSync.ts:212-215`), so a newly launched task can display agent-only history even though Chimera stores the launch prompt.
4. The native panel root has no `w-full` or `flex-1` (`web/src/components/NativeAgentPanel.tsx:637-638`), so its used width is content-dependent.
5. Send a user message. The end-aligned user bubble and composer content can increase the flex item's intrinsic width (`web/src/components/NativeAgentPanel.tsx:650-656`, `web/src/components/NativeAgentPanel.tsx:989-1008`), making the panel appear to expand.

**Trigger conditions:** normal single-agent native Chimera view; the effect is most visible with short assistant-only content. It is not expected for terminal-backed agents. Native participants embedded in `SwarmCommandCenter` are less likely to show it because the command center places them inside already constrained grid/flex regions (`web/src/components/SwarmCommandCenter.tsx:107-122`, `web/src/components/SwarmCommandCenter.tsx:131-153`).

No browser/E2E reproduction was performed. The repository instructions prohibit browser/E2E testing unless explicitly requested, and this assignment requested repository triage only.

## Root cause

1. **Confirmed:** commit `62a54fd9e9e45770f635b3c3a183dde35270a4ad` (`feat(swarm): add web command center`, 2026-08-21) changed the selected-agent host from a normal block container to a flex container by adding `flex` at the line now located at `web/src/pages/Agents.tsx:477`.
2. **Confirmed:** the normal native panel remains a direct child of that host (`web/src/pages/Agents.tsx:504-515`).
3. **Confirmed:** the native panel root has no width/flex-growth utility (`web/src/components/NativeAgentPanel.tsx:637-638`). Under the new parent layout, it is therefore a content-sized flex item.
4. **Confirmed:** displayed snapshot history can omit the initial user prompt (`web/src/lib/nativeAgentSync.ts:212-215`), while assistant and user content have different alignment/content shapes (`web/src/components/NativeAgentPanel.tsx:649-668`). This explains why the defect is visible before the first displayed user message and appears to resolve after sending one.
5. **Result:** the entire native conversation/status/composer column can collapse to a narrow intrinsic width even though its host owns the full remaining page width.

**First broken in:** commit `62a54fd9e9e45770f635b3c3a183dde35270a4ad`, 2026-08-21. The previous version of the same host was not a flex container, so its block child stretched horizontally by default. This aligns with the report that the issue began “in the last few days.”

## Impact

- **Who is affected:** users opening normal single-agent native Chimera sessions, especially newly launched sessions whose visible history contains only short assistant output.
- **Blast radius:** `NativeAgentPanel` is also reused for swarm participants (`web/src/pages/Agents.tsx:491-496`), but those instances are wrapped by explicit command-center layout regions (`web/src/components/SwarmCommandCenter.tsx:107-122`, `web/src/components/SwarmCommandCenter.tsx:131-153`). The primary confirmed failure seam is the direct non-swarm child at `web/src/pages/Agents.tsx:504-515`.
- **Unaffected path:** terminal-backed agents fill the host through absolute positioning (`web/src/components/TerminalPanel.tsx:173-179`).
- **Data integrity:** none. This is CSS sizing only; messages and agent state are not mutated by the collapse.
- **Workaround:** send a message containing enough content to enlarge the panel, or use the terminal-backed view. The first workaround is temporary and content/viewport-dependent.

## Ruled out

- ~~The main application or Agents page lacks available width~~ — the application main is `flex-1` (`web/src/components/Layout.tsx:215-218`), and the Agents content column and selected-agent host are also `flex-1` (`web/src/pages/Agents.tsx:473-477`). The available space exists; the native child does not claim it.
- ~~Assistant bubble `max-w-[75%]` directly limits the whole UI to 75%~~ — that class is on the inner bubble (`web/src/components/NativeAgentPanel.tsx:661-665`), not the panel. It makes the collapse visible but cannot alone shrink a block-level panel; the parent becoming flex is the regression trigger.
- ~~The server or WebSocket resize protocol changes width after the user message~~ — native message submission sends `agent_message` (`web/src/components/NativeAgentPanel.tsx:492-500`) and the native panel has no terminal resize protocol. The width change follows flex intrinsic sizing, not server geometry.
- ~~The regression applies equally to xterm agents~~ — `TerminalPanel` is absolutely inset into its host (`web/src/components/TerminalPanel.tsx:173-179`) and does not participate as a content-sized flex item.
- ~~The 2026-08-21 composer visibility follow-up introduced the collapse~~ — commit `191b0a4` only made the native composer conditional for command-center embedding; the host became flex in the earlier `62a54fd` change. The follow-up does not alter the normal panel's root width (`web/src/components/NativeAgentPanel.tsx:954-1034`).

## Options

### Option 1 — Make the native panel own its width (recommended)

Add `w-full` and/or `flex-1` to the `NativeAgentPanel` root at `web/src/components/NativeAgentPanel.tsx:638`, retaining `min-w-0`. Add a component test that asserts the root's fill-width/flex contract for assistant-only history.

- **Effort:** small.
- **Risk:** low. The component should fill every host where it is used, including command-center participant regions.
- **Does not address:** future direct children of the shared host that also omit their own flex sizing.

### Option 2 — Constrain the normal selected-agent branch at the call site

Wrap the non-swarm `NativeAgentPanel` at `web/src/pages/Agents.tsx:504-515` in an explicit `min-w-0 flex-1` container, or apply sizing through a wrapper shared by both native and terminal branches.

- **Effort:** small.
- **Risk:** low to medium because normal and swarm render paths retain different layout contracts.
- **Does not address:** `NativeAgentPanel` remaining intrinsically sized if another flex host embeds it directly.

### Option 3 — Remove flex from the shared selected-agent host

Remove `flex` from `web/src/pages/Agents.tsx:477`, restoring the pre-`62a54fd` block behavior for non-swarm agents, and introduce a nested flex wrapper only around `SwarmCommandCenter` if needed.

- **Effort:** small to medium.
- **Risk:** medium. The flex host was deliberately added for the command center, whose root expects to grow as a flex item (`web/src/components/SwarmCommandCenter.tsx:107`, `web/src/components/SwarmCommandCenter.tsx:132`). This option couples the fix to recent swarm layout behavior and needs broader regression coverage.
- **Does not address:** the native panel's implicit dependency on being mounted under a block parent.

## Recommendation

Choose **Option 1**. The invariant belongs to `NativeAgentPanel`: it is a full agent surface, and all current callers place it in an area intended to be filled. Encoding that requirement on the component root is the smallest fix, preserves the command-center parent layout, and prevents recurrence at future flex call sites. Option 2 repairs only one caller; Option 3 risks regressing the feature that exposed the missing child contract.

**Files to touch**

- `web/src/components/NativeAgentPanel.tsx:638` — make the root fill/grow within its host while preserving `min-w-0` and overflow containment.
- `web/src/components/__tests__/NativeAgentPanel.test.tsx:24-44` — add a regression case using assistant-only snapshot history and assert the rendered panel root has the fill-width/flex class. The existing suite covers message behavior and optional composer rendering but not root geometry.

An optional higher-level `AgentsPage` regression test could assert that a native agent is mounted in a filling surface, but jsdom cannot compute flex layout. The durable unit assertion is the native panel's class contract; visual verification should cover actual geometry.

**Verification**

1. Add the failing class-contract test first, then apply the root sizing change.
2. Run `cd web && pnpm test -- src/components/__tests__/NativeAgentPanel.test.tsx`.
3. Run `cd web && pnpm test`, `pnpm lint`, and `pnpm build`.
4. In a real browser at desktop and narrow/mobile widths, open a newly created native Chimera agent with short assistant-only history and confirm the conversation, status bar, and composer fill the available content width before any user message.
5. Send short and long user messages and confirm the outer panel width does not change.
6. Check a terminal-backed agent and both conductor/worker native panels in the swarm command center to confirm their existing fill and split behavior remains intact.

**Rollout concerns:** none. This is a frontend-only CSS change with no API, migration, persistence, feature-flag, or ordering requirement. A normal web bundle rebuild/deploy is sufficient.

**Related decisions:** the regression was introduced by the web command-center commit `62a54fd9e9e45770f635b3c3a183dde35270a4ad`. The documented intent is that the Agents view owns the solo layout and that command-center interactive panels fill their assigned regions (`docs/ai/web-ui.md:61-67`, `docs/ai/web-ui.md:110-118`); the recommended fix supports that intent and conflicts with no located ADR.

## Risks and unknowns

- The report does not include a screenshot, browser, viewport, or build ID. If the reporter was viewing a terminal-backed rather than native Chimera agent, a second mechanism would need investigation; the “only agent messages / expands after I send” description strongly matches the native message feed.
- CSS geometry was established by code inspection and history, not by browser measurement, because browser/E2E testing was not authorized for this task.
- `w-full` alone and `flex-1` alone have slightly different flex-basis behavior. Implementation should choose the smallest class set that remains stable in both the single-agent host and command-center grid/flex wrappers; `min-w-0 flex-1 w-full` is the most explicit contract if tests show no overflow.

## Open questions for the reporter

These do not block the recommended fix, but they would confirm the affected path and verification matrix:

1. Did the narrow view show the native chat-style message bubbles and **Send / Queue** composer, or the terminal/xterm interface?
2. Which browser and approximate viewport/device showed the issue, and does it occur at both desktop and mobile widths?
3. Can you provide the served frontend build/version or a screenshot from before sending the first message?
