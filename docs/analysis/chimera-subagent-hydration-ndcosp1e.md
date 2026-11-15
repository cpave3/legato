# Ocular session: discovered child omitted by snapshot hydration

15 November 2026. Task `ndcosp1e`; reported parent `np5epta4`, **Ocular Dark colour design discussion**.

## Finding and reproduction

The child was discovered, not lost by the mod. The first scoped live API snapshot contained:

- Parent conversation `01M7EV0P4YE7J9MM86FNT5ZJCA`.
- Foreground `spawn_agent` call `01M7EV9586DYBP3G5E6CCGYQRR`, started at `1794720961798`.
- Subject `01M7EV95878DWERN7KV7H9BPR5`, child conversation `01M7EV95QGXW94T4PSAYNHC0J7`, canonical status `running`, started at `1794720962740` (05:36:02.740 UTC).
- **Eight** snapshot tool records, but only **seven** committed message `tool-call` parts. The last tool record was the spawn; its assistant message was not committed yet.

`stateFromNativeSnapshot` consumed tool records only inside the committed-message tool-slot loop. It omitted the eighth record, then added **all eight** IDs to `state.callIds`. A reload/repair therefore hid the spawn and also suppressed a replayed `tool_call_start`. Canonical child state could not attach to an item that did not exist. This is an absent UI card/switch, not merely a stale “spawning” label.

The regression reproduces the shape using synthetic data (one committed tool, then uncommitted read/spawn tools). Before the fix both foreground/background rows fail: the uncommitted tools are absent. The fix renders remaining native tool records after committed history, before exact lifecycle joining. Later committed snapshots move these same stable calls into their message without duplication. No purpose/order-based child matching was added; child joins remain by `subagentId`/result ID or canonical `parentCallId`.

A later read-only sample showed canonical `finished`, reason `stop`, ended at `1794721297362`, and the tool result naming the same child conversation. Success is proven by that explicit result, not inferred from disappearance.

## Independent persisted evidence and timestamp correction

The operator supplied a narrow SQLite `mode=ro` + `query_only` read:

- `agent_sessions.id=1876`, `task_id=np5epta4`, `hook_source=mod`, running/working, started at `2026-11-15 05:31:23`, no end.
- `chimera_subagents` retained the exact subject and parent conversation above with status `finished`, reason `stop`, mod start/end `1794720962747` / `1794721297368`.
- Exactly one matching execution, `ex-8yk8mky6`, started at `2026-11-15 05:31:23.402986541`, running, no end.

The API listing presented a later/changing `started_at`. An initial hypothesis of a replaced/resumed execution was **not supported** by the database; it is withdrawn. No replacement or resumption is asserted. The presentation timestamp discrepancy is outside this fix.

## Lifecycle trace and scope

Prior context read in full: `/api/tasks/LEG-196/notes`, PR91 (merged `1b41bae9587ba7d071001c564133df889f11b0da`, implementation `7f07d16c`), and **`contracts/chimera-subagents.json`** (there is no `shared/contracts/` copy in this tree).

At inspected Chimera source `6220142a2abcc709221f17293e173e0fb4b45a75`:

1. `packages/cli/src/factory.ts:911-947` constructs `buildSpawnAgentTool`, emitting into the parent's `agent.pushEvent` and resolving the actual parent call ID.
2. `packages/subagents/src/spawn-tool.ts:193-362` distinguishes external/in-process launch, then uses the same parent `subagent_spawned` / `subagent_finished` path for foreground/background. Background returns `status: running`; foreground awaits the explicit outcome. External handshake failure returns an error result without a spawned event. In-process builder failure emits an error finish; a missing builder returns an error result without launch.
3. External `spawn-child.ts:58-100` sends `--parent <parent conversation>`; the child server's instance parent identity marks its mod contexts as children. `mod-runtime.ts:127-128` derives `isSubagent` from instance parentage. The Legato mod excludes child-context lifecycle callbacks before enqueueing or closing its single parent-owned link. The observed spawn did not request `in_process` or `background`.
4. **The current CLI factory supplies no `inProcess` builder.** Explicit `in_process: true` is unsupported in that runtime, returning `in_process subagents are not enabled in this runtime`; it must not be interpreted as an undiscovered successful child. Library embedders can supply a builder; after successful construction both modes use the common parent events. Their own child lifecycle ownership depends on the embedder; this investigation did not launch or verify a custom embedder. No demonstrated upstream ownership failure requires editing Chimera.
5. `core/src/agent.ts:208-246,862-878` retains canonical parent lifecycle and publishes idle events; `server/src/agent-registry.ts:274-307` subscribes idle events into the same bus/SSE path. **CHIM-93 is no longer an unfixed upstream premise**: commit `c9d6a3f9ef89e6d2d8956475ebf9cfb2bf335401` is in inspected HEAD; exact parent/child metadata and idle publication are present.
6. Legato `index.mjs` forwards ordered bounded records to `RunChimeraLink`, which owns exact binding/identity and sequence deduplication. `ParseChimeraHook` / `ApplyChimeraHook` persist lifecycle atomically with jobs/activity. Only background children enter active jobs, so no background job for this foreground child is expected. Existing mod persistence drops optional `parentCallId`/`childSessionId`; canonical native state supplies those identities on this host. That older fallback limitation was not the loss point here.
7. `chimeraSnapshotHandler` prefers canonical `subagentStates`, augmenting only absent native state. The two samples and independent DB evidence agree with that path. The SSE proxy remains the native stream, not the mod pipe. No raw live SSE history or browser event trace was captured, so the precise browser reconnect timing is unknown.
8. `NativeAgentPanel` hydrates/reconciles through `nativeAgentSync`, then uses the shared status selector for Inline and Tiles. The hydration omission is present in the served asset, independently of whether startup SSE was available. Agent-change notices repair completion once the tool item exists.

## Versions and deployed limits

- Isolated clone base: `bf669aeb` (PR99 merge); both PR91 and the reported deployed revision are ancestors. No live files were changed.
- Operator's read-only host build evidence: PID680564 executable `/var/home/cameron/Projects/legato/legato`, SHA256 `19a4523a7ce02f2a87db12797fee62a13703e7a2cd66f6373abc87424dd7814d`. PIDs683150/689071/689088/689317/689333 retain deleted executable SHA256 `d2b0c2fb660eca2150380a184d9da02fafe6241fff63a697e6568ca9e33faebe`. Roles were not established; these are not assumed to be multiple main processes.
- Both report Go1.26.0, module `v0.18.1-0.20261115050450-005ddd8bbf92+dirty`, VCS `005ddd8bbf925acde4f150abd8632d042dbb66e7`, time `2026-11-15T05:04:50Z`, modified=true, CGO disabled, linux/amd64. A dirty build cannot be equated to an exact clean source tree.
- Read-only static HTTP GET served `/assets/index-DVptZQcY.js`, 1,514,756 bytes, SHA256 `e6a43db0ef7961c8206c0fbb5dcec920671c845b1608a75f995658dcfcf3fc7b`. Its minified hydration function contains the committed-slot-only loop followed immediately by lifecycle join; the fix is **not deployed** there. PR91's lifecycle join/status repair is already present.
- Installed Chimera launcher resolves to the primary checkout's `packages/cli/dist/bin.js`; `chimera version --json` reports dev/nonrelease, mod API `[1]`. Read-only checkout HEAD `6220142a...`, clean. `dist/bin.js` SHA256 `6fa6da61d0eb7d9672c9ebf23779fb466b2100eadf99af4ba24cbd3a964cee9a`; actual CLI `dist/index.js` SHA256 `010443f56db5e739ba202764e156b740f2063660166801a634c8a5586c9f2ef8`, modified 04:38:59 UTC. The unversioned build does not prove every dependency's precise build provenance; live canonical metadata independently proves the relevant CHIM-93 behavior. No live design process executable/environment was inspected from this masked PID namespace.

## Verification

- RED: both new reducer rows omitted the uncommitted spawn on the original code.
- GREEN: reducer tests assert pending read/spawn ordering, exact child identity, replay deduplication, missed completion repair, and later committed-message reconciliation without duplicates (foreground/background).
- Caller-facing panel tests load a snapshot with no committed assistant tool message and no SSE spawn; assert running Inline, running Tile, completion after `agents_changed` without finish SSE, then finished Inline (both launch modes).
- Full web suite: **226 files / 1,672 tests passed**. TypeScript build and targeted ESLint passed.
- Existing Node mod harness: **13 passed**, including child lifecycle exclusion.
- Go lifecycle/broker and snapshot tests passed: `GOCACHE=/tmp/ocular-go-cache CGO_ENABLED=0 go test ./internal/cli -run 'TestChimera(LinkRetainsSubagentLifecycle|Hook_SubagentOutcomes)'` and `go test ./internal/server -run TestChimeraSnapshot` with the same environment. First attempt was blocked by the sandbox's read-only default Go cache; rerun uses a scratch cache.
- Test audit: two new parameterized declarations (four cases) retained for distinct reducer reconciliation and real panel wiring contracts; no test-only production seam. No existing tests removed. Removing only the production fix makes all four new cases fail; restoring it passes all 130 tests in the two touched files.

Diagnostics were scoped with local Python filtering, never huge raw snapshots in MCP. Private raw later snapshot/notes and served asset are under `/tmp/ocular-evidence` for this run only; initial snapshot's decisive fields/counts are preserved above and in tool output. No raw conversation or credentials are committed. No browser/E2E against the live app, terminal input, agent/child message, setting change, live build/install/restart, or process interruption was performed. End-to-end UI verification here is isolated jsdom panel behavior, not a claim that the operator's running browser was retested.
