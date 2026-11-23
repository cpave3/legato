# Compacting an agent

Legato compacts a native agent with its tool's own compaction (LEG-535). It never sends `/compact` to an agent as a message: a message `/compact` reached Claude Code during a turn as plain text, and Codex and Chimera always as plain text.

User docs: [Compacting an agent](../../site/src/content/docs/using/agents.md#compacting-an-agent) and each page under `agent-tools/`.

## One route for every tool

Every native endpoint serves `POST /v1/sessions/{id}/compact` with an optional `{"instructions": "…"}`: 202 when the tool took the request, 409 while a turn (or another compaction) runs. Chimera's server had this route already. Legato's two bridges serve the same shape:

| Tool | Endpoint | How it compacts | Focus | While busy |
| --- | --- | --- | --- | --- |
| Claude Code | `claudebridge.handleCompact` | the mod command `{"type":"compact","instructions"}`, run by `compact` in `claudemod/legato/hooks/register.ts` as `$.session.compact({instructions})`: the event `session.compact` with trigger `plugin`, the call `/compact` makes | yes | 409 from the bridge (`session.busy` or `session.compacting`); the engine also rejects during a turn |
| Codex | `codexbridge.handleCompact` | `thread/compact/start {threadId}` on the terminal's thread | no: 400 from the bridge | 409 from the bridge; Codex itself would interrupt the turn (`turn/completed` `interrupted`, then a compact turn) |
| Chimera | Chimera's `packages/server/src/app.ts` | `registry.compact` → `agent.compactSession()` | no (ignored by Chimera; Legato refuses first) | 409 `run already in progress` |

Every endpoint reports the compaction in its event stream with Chimera's event names:

- `compaction_started {reason}`: `manual` for one Legato asked for; Codex's bridge says `threshold` for one Codex started itself.
- `compaction_finished {tokensBefore?, tokensAfter?}`.
- `compaction_failed {error}`: Claude Code's `{ skip }` (a hook vetoed it) or a rejection.

### Claude Code's sizes

`SessionCompacted.tokensAfter` is the engine's count of the compacted conversation alone. Measured on Claude Code 2.1.330: 49,656 → 15,046, then the next request read 49,135. It leaves out the system prompt, the tools and the files Claude Code reads back in after compacting ("Referenced file notes.md"). After a compaction `$.session.usage()` has no `tokens` until the next request. So the mod measures:

- before: the last request's tokens (`contextOf`), else `result.tokensBefore`;
- after: before − (estimate before − estimate after), where the estimate is `$.session.usage({ breakdown: 'summary' })`'s `context.breakdown.totalTokens`, counted locally without a request. Its fixed parts cancel out. Measured: 66,815 → 48,945, and the next request read 49,180.

Without both estimates, `tokensAfter` and `context` are left out and the meter keeps its reading until the next request. The bridge (`session.apply`, `compaction_finished`) sends `usage_updated` from the event's `context`.

### Codex's sizes

`tokensBefore` is the context reading when the `contextCompaction` item started; `tokensAfter` is the last `thread/tokenUsage/updated` before the item completed, which Codex sends as the compacted thread's size (only `last.totalTokens` set). The meter follows those updates already. This was seen against a fake Responses API only (the sandbox hides Codex's login); a real model's figures are not verified. Compaction items in a joined thread's history are skipped (`session.load`): they are no news.

## Legato's side

- `service.AgentCompactor.CompactAgent(ctx, taskID, instructions)` (`internal/service/agent_compact.go`) refuses with errors that read as the reason and match with `errors.Is`: `ErrCompactUnsupported` (a tool not in `compactTools`, `native_off`, no endpoint yet), `ErrCompactFocusUnsupported` (focus for Codex or Chimera), `ErrAgentWorking` (the endpoint's 409). Otherwise it calls `chimera.Client.Compact`. It is not on the `AgentService` interface; the server type-asserts it. `CanCompact(kind)` says which tools Legato can compact.
- `POST /api/agents/{task_id}/compact` (`internal/server/agent_compact.go`, class `change`): 202, 409, 422 for both refusals, 502 for anything else, with the reason as `error`.
- `GET /api/agents/{task_id}/chimera/skills` adds `compact` (source `legato`) for an agent whose tool `CanCompact`, unless its list has one (Claude Code lists its own built-in `compact`).
- The context watcher (`internal/server/agent_context.go`, `readingFrom`) also reads `compaction_finished`'s `tokensAfter`, in the window of the agent's last reading, so the meter follows a Chimera compaction, which sends no `usage_updated` of its own.
- The panel (`NativeAgentPanel`, `web/src/lib/compaction.ts`): `compactCommand` reads `/compact [focus]` in the composer, which calls the route instead of sending a message; a refusal shows in the send-error line. `nextCompaction` follows the events, and `compactionNotice` is the status line: "Compacting the conversation…", "Compacted: 66.8k → 48.9k tokens", or "Compaction failed: …". The next turn's `session_started` clears a finished one.

## Automatic compaction

`agents.compact.auto_percent` (int 0–100, default 0, live; Settings › Agents › Compaction). `service.AutoCompactor.Check` runs every `AutoCompactCheckEvery` (15 s) in the process that serves the broker (`cmd/legato/autocompact_runtime.go`, started in `startApprovals`). It compacts each running native agent that is idle (`activity` empty) with `used × 100 ≥ percent × window` (read through `ConversationTail`, only for idle agents). It remembers the size it tried at, and tries again only after the context grows past it. One that drops under the threshold is forgotten, so the next fill is compacted too. Refusals and failures are logged, not raised.

## Not done

- A workflow step that compacts an agent: LEG-536.
- The TUI has no compact key.
