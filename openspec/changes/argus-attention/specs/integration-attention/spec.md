## ADDED Requirements

### Requirement: The attention scope
Integration tokens SHALL accept the scope `attention`. The routes `PUT /api/integrations/attention/{key}`, `POST /api/integrations/attention/{key}/resolve` and `POST /api/integrations/attention/sync` SHALL require it and answer 403 `missing_scope` to a token without it. They SHALL answer 503 `unavailable` when `integrations.argus` is not set.

#### Scenario: Token without the scope
- **WHEN** a token with only `owner:read` and `delegate` calls the upsert
- **THEN** Legato SHALL answer 403 with `{error: "missing_scope"}` and store nothing

### Requirement: Upserting a case's item
`PUT /api/integrations/attention/{key}` SHALL take `{revision, signal, title, reason, detail, urgency, target, link}` and store one item per key and integration. It SHALL answer 400 `bad_request` when:
- the key does not match `^case-[A-Za-z0-9-]{1,96}$`;
- `revision`, `title` or `reason` is empty;
- `signal` is not `argus_case` or `argus_needs_you`;
- `urgency` is not `low`, `normal` or `high`;
- the target names a PR, branch or SHA without a repository.

It SHALL answer 422 `invalid_link` when `link` is not on the origin of `integrations.argus.url`. It SHALL cut `title` to 200 characters, `reason` to its first line and 300 characters, and `detail` to 4096 bytes, rather than refuse them. It SHALL answer 200 `{itemId, taskId?, status}`, where `status` is the Attention item's `open`, `dismissed` or `resolved`.

#### Scenario: New case
- **WHEN** Argus upserts a new key with a target whose PR a task owns
- **THEN** an open Attention item from producer `argus` SHALL appear on that task, and the answer SHALL carry its ID and the task's ID

#### Scenario: Same revision
- **WHEN** Argus upserts a key again with the same revision and fields
- **THEN** nothing SHALL change and the answer SHALL be 200

#### Scenario: Long detail
- **WHEN** the detail is 10 KiB
- **THEN** Legato SHALL store the first 4096 bytes ending in `…` and answer 200

#### Scenario: Link elsewhere
- **WHEN** the link's origin differs from `integrations.argus.url`
- **THEN** Legato SHALL answer 422 `invalid_link` and store nothing

### Requirement: Dismissal follows the revision
An item the person dismissed SHALL stay dismissed while upserts carry the dismissed revision, even if the matched task changes. An upsert with a new revision SHALL reopen it.

#### Scenario: Case changes after a dismissal
- **WHEN** the person dismisses an item, and Argus then upserts it with a new revision
- **THEN** the item SHALL be open again

### Requirement: Resolving a case's item
`POST /api/integrations/attention/{key}/resolve` with `{revision, reason?}` SHALL resolve the item and record the revision. It SHALL answer 404 `not_found` for a key Legato does not have. An upsert carrying the recorded revision SHALL change nothing and answer `status: "resolved"`. An upsert with any other revision SHALL reopen the item.

#### Scenario: Late retry after a resolve
- **WHEN** Argus resolves a key at revision 7, and an upsert at revision 7 arrives afterwards
- **THEN** the item SHALL stay resolved

#### Scenario: Case reopens
- **WHEN** Argus upserts a resolved key with revision 8
- **THEN** the item SHALL be open

### Requirement: Syncing open cases
`POST /api/integrations/attention/sync` with `{keys}` SHALL resolve every open item of the integration whose key is not in `keys`, recording its current revision, and answer 200 `{resolved: n}`. Keys Legato does not have SHALL be ignored. An invalid key SHALL refuse the call with 400 and change nothing.

#### Scenario: A close Argus missed
- **WHEN** items `case-a` and `case-b` are open and Argus syncs `["case-a"]`
- **THEN** `case-b` SHALL be resolved, `case-a` SHALL stay open, and the answer SHALL be `{resolved: 1}`

### Requirement: Items attach to the owning task
The `argus` producer SHALL match each open item's target to a task with owner lookup's ranking at every reconcile. An item with no target, or no matching task, SHALL have no task and no workspace. An item SHALL attach when a task later comes to own its PR, branch or commit. When `integrations.argus` is not configured, the producer SHALL report no items, so Attention resolves them.

#### Scenario: Task links the PR later
- **WHEN** an item targets a PR no task owns, and a task is later linked to that PR
- **THEN** the item SHALL carry that task's ID after the next reconcile

### Requirement: Signals and sections
`argus_case` and `argus_needs_you` SHALL be in Attention's default allowlist, with category `argus`. The web dashboard SHALL show `argus_case` in Merge blockers and `argus_needs_you` in Needs intervention, with detail rendered as Markdown without raw HTML.

#### Scenario: Needs-you case
- **WHEN** Argus upserts an item with signal `argus_needs_you`
- **THEN** it SHALL count in `GET /api/attention` and show in Needs intervention

### Requirement: Opening the case in Argus
An Argus item's navigation SHALL be `{kind: "external", target: link}`. The web UI SHALL open it in a new tab as "Open in Argus", and SHALL link the task too when the item has one. The TUI SHALL open it in the browser.

#### Scenario: Open from the web
- **WHEN** the person opens an Argus item on the web dashboard
- **THEN** the case's page in Argus SHALL open in a new tab

### Requirement: One report per failing PR
While an open Argus item targets a tracked PR (same repository, case-insensitive, and PR number), the `pull_requests` producer SHALL leave "CI failing" out of that PR's item, and SHALL raise no item when it was the only reason. When the Argus item resolves and the checks still fail, its "CI failing" item SHALL return.

#### Scenario: CI is the only blocker
- **WHEN** a tracked PR's checks fail, nothing else blocks it, and an open Argus item targets it
- **THEN** Attention SHALL show the Argus item and no `pr_merge_blocked` item for that PR

#### Scenario: Review and CI
- **WHEN** a tracked PR has changes requested and failing checks, and an open Argus item targets it
- **THEN** its `pr_merge_blocked` item SHALL give only "Changes requested"
