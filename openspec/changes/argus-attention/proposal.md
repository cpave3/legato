## Why

Argus follows the person's CI. For each failing check on their PRs it opens a case, triages it (flaky or real), and sometimes proposes an action that waits for the person. Today the person has to open Argus to see any of this. They already work from Legato's Attention panel, and Legato knows which task owns each PR. This change puts Argus's cases in that panel.

The contract was agreed between the Argus agent and the Legato agent in November 2026. Argus builds its side (a "case follower" in its core and its Legato pack) against a fake Legato. This change is Legato's side. It builds on `argus-integration` (scoped tokens, owner lookup, the `integrations.argus` config).

## What Changes

- **Scope.** Integration tokens gain the scope `attention`.
- **Upsert.** `PUT /api/integrations/attention/{key}` adds or updates one Attention item per Argus case:
  - title, reason, Markdown detail, urgency, and the signal `argus_case` or `argus_needs_you`;
  - an optional target `{repo, pr, branch, sha}`, which Legato matches to the owning task;
  - a link back to the case in Argus.
- **Resolve.** `POST /api/integrations/attention/{key}/resolve` closes the item when the case closes. An upsert with the resolve's revision changes nothing, so a late retry cannot reopen a closed case.
- **Sync.** `POST /api/integrations/attention/sync {keys}` lists every case Argus still considers open. Legato closes the rest. Argus sends it at startup and hourly.
- **Attention.** A new producer `argus` with category `argus`, and two signals in the default allowlist. `argus_needs_you` shows in Needs intervention and `argus_case` in Merge blockers. A new navigation kind `external` opens the case in Argus. Item detail is shown as Markdown.
- **No duplicates.** While an open Argus item targets a PR, Legato's own `pull_requests` producer drops "CI failing" from that PR's item.

Not in this change: acting on a case from Legato. Accepting or rejecting a proposal stays in Argus's web app, because confirmations that raise Argus's autonomy belong there.

## Capabilities

### New Capabilities
- `integration-attention`: the `attention` scope, the upsert, resolve and sync routes, how pushed items become Attention items and attach to tasks, the `external` link, and the PR producer's deduplication.

### Modified Capabilities

None in `openspec/specs`. Attention has no spec there yet; its behaviour is in `docs/ai/attention.md`, which this change updates.

## Impact

- **Code:**
  - `internal/engine/store`: a migration for `integration_attention`; store methods.
  - `internal/service`:
    - `CaseAttentionService`: upsert, resolve, sync.
    - The `argus` Attention producer.
    - The two signals and the `attention` scope.
    - `PRAttentionAdapter` learns about Argus items.
  - `internal/server/integrations.go`: the three routes.
  - `cmd/legato`: wiring in both host runtimes.
  - `web/`: the `external` navigation kind, the two signals' sections, Markdown detail, and an "Open task" link.
  - `internal/tui`: opening an `external` item in the browser.
- **Config:** none new. The routes need `integrations.argus.url`, and the link must be on its origin.
- **Docs:** `docs/ai/integrations.md`, `docs/ai/attention.md`, `docs/ai/database.md`, `docs/ai/web-ui.md`.
