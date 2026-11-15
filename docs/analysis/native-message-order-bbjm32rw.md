# Native completion-refresh ordering: bbjm32rw

## Finding

The reported `done?` exchange was not deleted. It survives in the affected
Chimera journal, Legato's conversation API, and the native snapshot captured at
2026-11-15 11:15:57 UTC. The native session identity is unchanged.

That snapshot contains 270 model messages and 377 tool records, but only 141
retained tool-call slots. The last two messages are the reported question and
completion reply. No compaction occurred during that exchange.

Earlier compaction had removed a prefix of model messages without deleting the
associated tool records. `stateFromNativeSnapshot` assigned the first 141 records
to retained slots, then appended the remaining 236 records after the final reply.
The PR100 uncommitted-tool fallback treated historical records as active work.
After `run_finished`, the panel's snapshot repair rebuilt the feed in this wrong
order. The exchange moved above the viewport and old tool cards occupied the end.
This explains the operator's native-only disappearance while tmux kept the text.
It is not evidence of acknowledged-but-unpersisted input or a session switch.

## Verification

- A private replay of the actual snapshot fails the final-exchange ordering
  assertion on the original source and passes with the fix. It renders 141
  retained tools rather than adding 236 historical records after the reply.
- Public panel tests stream the exchange, complete the turn, replace the
  snapshot, repair after a missed cursor, and remount. They assert one copy of
  each message and the reply as the last conversation row.
- Real Chromium, against an isolated Vite fixture only, reproduces visible
  exchange → displaced exchange on the original code. With the fix, the reply
  remains visible after completion and reload at desktop and phone widths.
  The script changes no production scroll behavior.
- Reducer tests cover reused provider IDs, failed unattached steps, mixed
  pre-upgrade records, completed-but-uncommitted siblings, replay deduplication,
  and exact active-child identity. Active children whose parent message was
  compacted remain before the retained timeline, not after the latest reply.
- The paired Chimera tests exercise public `Agent.run`, persistence/resume,
  actual client HTTP/SSE, errors and interruption. Legato's proxy test checks
  that correlation fields survive serialization unchanged.

Reproduce the browser check with an isolated Vite server and
`scripts/verify-native-message-order.mjs <loopback-origin> <output-folder>`.
`PLAYWRIGHT_MODULE` and `CHROMIUM_PATH` can select an installed local browser.
`--expect-broken` verifies the original-code symptom. The fixture uses synthetic
text and tools, never a live backend. Private snapshots and screenshots of live
transcripts must not be committed.

## Fix and compatibility

Idle older hosts now align retained model slots with the tool-record suffix.
This fixes the reported completion-refresh path without a Chimera update.

The paired Chimera change records raw `toolCallId`, explicit `messageCommitted`,
and active-step `pendingMessage` metadata. A provider can reuse its ID: Legato
joins the suffix of committed **occurrences** in message order, preserving each
canonical `callId`. Failed unattached invocations do not enter that occurrence
count. Pending calls remain visible even after finishing while a sibling runs.
New hosts mark pre-metadata records settled on resume, and clear crash-persisted
pending flags. A final checkpoint failure becomes an actionable run error.

Both updates are required for exact active-step correlation after historical
compaction. An old busy host still uses the earlier uncommitted-tool fallback;
there is no sound way to infer its active SDK boundary from counts alone.
Applying updates and resuming agents remains an operator-controlled rollout.
No live agent, repository, session, configuration or installed binary was changed.

This targeted fix does not separate durable conversation history from model
context. The separate proposed plan remains paused. It does not restore dialogue
that compaction actually removed, nor claim that every earlier report had the
same cause. No live SSE or browser trace was captured for this exchange.

## Source versions

Investigation bases: Legato `8e577f8f6051f03aa2a06857dd43a27c405973d9` and
Chimera `aa090b29962a049165327c1c6c9fe424f99c7bc6`. The installed binaries reported
`dev`; on-disk build hashes were recorded privately. The affected process's exact
build SHA was not proven. Work and verification used isolated clones only.
