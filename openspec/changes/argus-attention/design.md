## Context

Argus opens a case for each failing CI check it follows on the person's PRs. It triages the case (flaky or real, with a confidence), and may propose an action (a rerun, a delegation) that waits for the person. The person wants these cases in Legato's Attention panel, next to the task that owns the PR.

What Legato has that this builds on:
- **Attention** (`docs/ai/attention.md`):
  - items keyed by `(producer, source_key)`;
  - producers that Legato reconciles from snapshots, where a key missing from a snapshot is resolved;
  - revision-aware dismissal;
  - a central allowlist of signal kinds;
  - three sections on the web dashboard, chosen by signal kind.

  `UpsertAttentionItem` reopens a resolved item on any upsert, even at the same revision.
- **Integrations** (`argus-integration`):
  - scoped `lgi_` tokens on `/api/integrations/*`;
  - JSON errors `{error, message}`;
  - `OwnerLookupService`, which ranks the tasks that own a repository's PR, branch or commit;
  - the `integrations.argus` config block.
- **The PR producer** (`pull_requests`) raises `pr_merge_blocked` for a tracked, open, non-draft PR whose review decision or checks block it. "CI failing" is one of its reasons.

The contract below was agreed with the Argus agent. Argus's side:
- one call in flight per key, each reading the case when it runs;
- urgency `high` for needs-you, `normal` otherwise, never `low`;
- at startup and hourly, a re-upsert of every open case, then a sync;
- the link `<Argus server.publicUrl>/cases/<id>`;
- 5xx, network errors and 503 are retried, other 4xx are final, and a 403 `missing_scope` becomes a warning on Argus's dashboard.

## Goals / Non-Goals

**Goals:**
- Each open Argus case is one Attention item, attached to the task that owns its PR when there is one.
- A dismissed item stays dismissed until the case changes. A closed case stays closed whatever order retries arrive in.
- If Argus misses a close, or is down when a case closes, the item still goes away.
- A failing PR with an Argus case shows once.

**Non-Goals:**
- Actions on a case from Legato. The item links to Argus.
- Attention items from integrations other than Argus. The table is keyed by integration so a later one fits, but the routes and the producer are Argus's.
- Workspace-level items. An item with no owning task is unattached.

## Decisions

### D1. The `attention` scope

`attention` joins `owner:read` and `delegate` as a known scope. `legato integration token create argus --scope owner:read,delegate,attention` makes Argus's token. All three routes below need it; a token without it gets 403 `missing_scope`.

### D2. Storage: `integration_attention`

Pushed items live in their own table, not directly in `attention_items`:

| Column | Notes |
|---|---|
| `integration` | the token's integration name (`argus`); part of the primary key |
| `key` | `case-…`; part of the primary key |
| `revision` | Argus's revision of the case |
| `signal` | `argus_case` or `argus_needs_you` |
| `title`, `reason`, `detail` | cut to their limits (D4) |
| `urgency` | `low`, `normal` or `high` |
| `target` | `{repo, pr, branch, sha}`, or `{}` |
| `link` | the case in Argus |
| `state` | `open` or `resolved` |
| `resolved_revision`, `resolved_reason` | set by resolve and by sync |
| `created_at`, `updated_at` | |

The `argus` producer snapshots the open rows. **Why:**
- Attention stays reconciled like every other producer.
- A snapshot can match the owning task again each time. An item attaches when a task later takes on the PR.
- When `integrations.argus` is removed, the producer snapshots nothing and Attention resolves its items. The rows stay for when it comes back.

### D3. From row to Attention item

| Attention field | Value |
|---|---|
| producer, category | `argus`, `argus` |
| `source_key` | the key |
| `source_revision` | Argus's revision |
| signal, title, reason, detail, urgency | the row's |
| navigation | `{kind: "external", target: link}` (D7) |
| actions | none |

- **Task.** `TaskID` and `WorkspaceID` come from running `OwnerLookupService` with the target at snapshot time; the best match wins. The ranking is the same as owner lookup: PR, then SHA, then branch, then a live agent, then the most recent.
- **No match.** With an empty target, or no match, the item has no task and no workspace. It shows in the All view.
- **Rematching.** A changed match changes `task_id` but not the revision, so a dismissal holds.
- **When the producer runs.** It is registered whether or not Argus is configured, and reconciles:
  - after every upsert, resolve and sync;
  - on `EventCardsRefreshed`, so a newly linked PR attaches its item;
  - in `ReconcileAll` at startup.

### D4. Upsert: `PUT /api/integrations/attention/{key}`

Body `{revision, signal, title, reason, detail, urgency, target, link}`, at most 64 KiB.

**Validation.** These are refused with 400 `bad_request`:
- `key` not matching `^case-[A-Za-z0-9-]{1,96}$`;
- an empty `revision`, `title` or `reason`;
- an unknown `signal` or `urgency`;
- a target with `pr`, `branch` or `sha` but no `repo`.

A `link` that is not https (or loopback http), or whose origin differs from `integrations.argus.url`, gets 422 `invalid_link`.

**Length.**
- `title` is cut to 200 characters, `reason` to 300, and `detail` to 4096 bytes, each at a UTF-8 boundary and ending in `…`.
- `reason` is cut at its first line break.
- Nothing over a limit is refused, because 4xx is final for Argus and a long summary should not lose the item.

**Then:**
1. If the row is `resolved` and its `resolved_revision` equals `revision`, change nothing.
2. Otherwise, write the row as `open`. If it was `open` at the same revision, this changes nothing either.
3. Reconcile `argus` and `pull_requests`.
4. Answer 200 `{itemId, taskId?, status}`. `itemId` is the Attention item's ID. `status` is the item's: `open`, `dismissed` (the person dismissed this revision), or `resolved`. Before the item first appears, `itemId` is left out.

### D5. Resolve: `POST /api/integrations/attention/{key}/resolve`

- **Body.** `{revision, reason}`; `reason` is optional and kept for the record, not shown.
- **Unknown key.** 404 `not_found`. Argus treats it as already resolved.
- **Otherwise.** The row becomes `resolved` with `resolved_revision = revision`. Then Legato reconciles `argus` and `pull_requests`, and answers 200 `{status: "resolved"}`.
- **Repeats.** Resolving a resolved row again answers 200 and updates its revision.

**Why the stored revision.** Legato's Attention reopens a resolved item on any upsert. A retry of the case's last upsert that arrives after the resolve carries that same revision, and so changes nothing. Argus's one call per key and its read-at-send already prevent this; the rule covers the edge.

### D6. Sync: `POST /api/integrations/attention/sync`

- **Body.** `{keys: [...]}`: every key Argus considers open, at most 10,000.
- **Validation.** One invalid key refuses the whole call with 400 `bad_request`.
- **Effect.** Every open row of the integration not in `keys` becomes `resolved`, with `resolved_revision` set to its current revision and `resolved_reason` set to `sync`. Then Legato reconciles. It answers 200 `{resolved: n}`.
- **Unknown keys.** Keys in the list that Legato doesn't have are ignored. Argus re-upserts every open case before it syncs, so they are rare.

### D7. The `external` navigation kind

Attention navigation pointed only at Legato routes until now (`plan`, `review`, `workflow_run`, `task`, `approval`, `settings`). Kind `external` has an absolute URL as its target.

- **Web.** `legatoHref` doesn't apply. The card's Open is an `<a target="_blank" rel="noopener noreferrer">` labelled "Open in Argus" (the producer's name, title-cased). When the item has a task, the card also links "Open task" to `/board?task=<id>`.
- **TUI.** Enter on an `external` item opens the URL with the existing browser opener.
- **Other clients.** The CLI and Stream Deck list it like any other item.
- **Accepted links.** Legato never accepts an `external` target from a producer other than an integration's, and an integration's link must be on its configured origin (D4). So the token cannot put arbitrary links in front of the person.

### D8. Signals, sections, urgency

- **Allowlist.** `argus_case` and `argus_needs_you` join `knownAttentionSignals` and `defaultAttentionSignals`.
- **Sections.** On the web dashboard, `argus_case` goes to Merge blockers, beside `pr_merge_blocked`. `argus_needs_you` falls to Needs intervention, the default section.
- **Urgency.** It is Argus's: `high` for needs-you, `normal` otherwise.

### D9. Detail as Markdown

- **Web.** Detail is shown with the web UI's existing Markdown renderer, raw HTML not rendered, folded to its first three lines with "More". Other producers' details are short plain text and look the same as before.
- **TUI.** The TUI shows detail as plain text.

### D10. No double report of a failing PR

`PRAttentionAdapter` gets a source of open `integration_attention` rows (repository and PR number from `target`).
- **Matching.** A tracked PR matches a row when `pr_meta.repo` equals the target's repository (case-insensitive) and the PR numbers are equal.
- **What drops.** For a matching PR, the producer leaves out "CI failing". If no other reason is left, the PR has no item; otherwise the reason drops "CI failing", and the revision adds `:argus` so the change shows.
- **When it re-runs.** Upsert, resolve and sync reconcile `pull_requests`, so when the case closes and the checks still fail, Legato's own item returns.

### D11. Errors and where it runs

**Errors** are those of D13 in `argus-integration`:

| Code | Status |
|---|---|
| `bad_request` | 400 |
| `missing_scope`, `forbidden` | 403 |
| `not_found` | 404 |
| `invalid_link` | 422 |
| `unavailable` | 503, when `integrations.argus` is not set |

**Where.** The routes are served by both host runtimes, like delegations. The store is shared. Each process reconciles and publishes `EventAttentionChanged` on its own bus. A client of the other process sees the item at its next reconcile or refetch, as with other producers today.

## As built

- **Names.** The service is `service.CaseAttentionService` (`case_attention.go`); the store type is `IntegrationAttention`, and the target column is `target`.
- **Body limits.** The upsert and resolve take the integration routes' usual 128 KiB, not 64 KiB. The sync takes 512 KiB, which 10,000 keys need.
- **Only Argus pushes.** A token whose integration is not named `argus` gets 403 `forbidden`, since only the `argus` producer shows items.
- **Reconciling.** The producer reconciles on `EventPRStatusUpdated` and `EventCardsRefreshed`, after every push, and at startup. Every Attention service registers it; the routes use the first one's service, since all share the database and the bus.
- **TUI.** The Attention list opens the link itself (`clipboard.OpenURL`) on Enter, for an item with `external` navigation and no executable action.
- **Task links.** The web UI's "Open task" goes to `/board?highlight_task=<id>`, which the board reads. Owner lookup's `task.url` (from `argus-integration`) said `/board?task=<id>`, which the board ignores; it now uses `highlight_task` too.

## Risks / Trade-offs

- **[Stale items when Argus is gone for good]** Its items stay until the config is removed. → Removing `integrations.argus` resolves them (D2). `legato integration token revoke argus` alone does not.
- **[A wrong task match]** A branch name shared by two repositories cannot match, because `repo` is required. A branch reused within a repository can match an old task. → The same ranking as owner lookup, which prefers PR and SHA and a live agent.
- **[Hiding Legato's own CI signal]** If Argus stops following a PR without resolving its case, Legato's "CI failing" stays hidden. → The hourly sync resolves it.

## Open Questions

None. Agreed with the Argus agent: the routes, the semantics, the urgency mapping, and the deduplication.
