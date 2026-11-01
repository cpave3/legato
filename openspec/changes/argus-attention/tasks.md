Every task is test first (the tdd skill). Keep `docs/ai/integrations.md` and `docs/ai/attention.md` current as each part lands.

## 1. Storage and scope

- [x] 1.1 Migration: `integration_attention`; store methods to upsert, get, resolve, list open, and resolve every open row not in a list. Validation: store tests, including the resolved-revision no-op (`docs/ai/database.md`)
- [x] 1.2 The `attention` scope. Validation: token create accepts it, and the routes refuse a token without it with 403 `missing_scope`

## 2. Service and producer

- [x] 2.1 `CaseAttentionService.Upsert`: validation, cutting to limits, the link's origin, the resolved-revision rule, the answer. Validation: service tests for each refusal and each cut
- [x] 2.2 `Resolve` and `Sync`. Validation: unknown key, repeat resolve, a sync that resolves only the keys left out
- [x] 2.3 The `argus` producer: rows to items, the task matched at snapshot time through `OwnerLookupService`, unattached with no match, nothing when Argus isn't configured; the two signals in the allowlist. Validation: a dismissal holds through a task rematch, a new revision reopens, removing the config resolves
- [x] 2.4 `PRAttentionAdapter` leaves out "CI failing" for a PR with an open Argus item, and brings it back when the item resolves. Validation: a CI-only PR, a PR with review and CI reasons

## 3. Routes and wiring

- [x] 3.1 `PUT …/attention/{key}`, `POST …/{key}/resolve`, `POST …/attention/sync` with D11's errors. Validation: handler tests for each status
- [x] 3.2 Wiring in both host runtimes; reconcile on `EventCardsRefreshed`

## 4. Clients

- [x] 4.1 Web: the `external` navigation kind (new tab, "Open in Argus"), "Open task" for an attached item, `argus_case` in Merge blockers, detail as folded Markdown without raw HTML. Validation: component tests; `pnpm exec tsc -b` (`docs/ai/web-ui.md`)
- [x] 4.2 TUI: Enter on an `external` item opens the browser. Validation: a model test

## 5. Together

- [x] 5.1 A scratch Legato and curl as Argus: upsert, see it on the task, dismiss, upsert the same revision (still dismissed), upsert a new one (open), resolve, retry the old upsert (still resolved), sync. Stop the scratch instance and remove its folders afterwards
- [x] 5.2 Tell the Argus agent the final paths and any change from this design
