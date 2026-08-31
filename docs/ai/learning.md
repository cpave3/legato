# Legato Learn

Legato Learn is a local, evidence-backed ledger for durable facts, preferences, process improvements, and pitfalls discovered while work is performed. It is deliberately not a transcript memory system. Evidence remains attributable, proposed learnings require lifecycle decisions, and changing a prompt or document is a separate, verifiable incorporation step.

## Architecture and ownership

The implementation follows Legato's normal layer boundaries:

- `internal/engine/store/migrations/049_learning.sql`, `internal/engine/store/learning*.go` — SQLite schema, FTS5 indexes, optimistic revisions, prompt manifests, and transactional persistence.
- `internal/service/learning*.go` — validation and lifecycle policy, reflection ingestion, bounded curation packets, prompt compilation, project identity, incorporation verification, and drift checking.
- `internal/service/workflow_learning_definition.go`, `workflow_interview.go`, and `workflow_learning.go` — built-in curation/consolidation definitions and learning-specific workflow executors.
- `internal/cli/learning.go` and `cmd/legato/learning_cmd.go` — the public `legato learn` command family and workflow callbacks.
- `internal/server/learning.go` — authenticated HTTP API.
- `web/src/pages/LearningsPage.tsx` and `LearningDetailPage.tsx` — browser list/search/detail and decision UI.

The `LearningService` owns policy. CLI, HTTP, workflows, and agent launch code call that service rather than mutating learning tables directly. Learning curation—including stale records—stays in the Learning ledger and does not increment Attention.

## Evidence, learning, and incorporation are different records

### Evidence

Evidence is an attributable observation in `learning_evidence`. It records source kind/reference, content and SHA-256 digest, author, and optional task, agent session, workflow run, and project identity. Evidence can support or counter a learning through `learning_evidence_links`.

Evidence is not automatically standing guidance. Agent reflections produce evidence first; curation may link it to an existing learning, create a candidate, or discard it from consideration. Evidence is intentionally allowed to remain session-specific: ticket IDs, filenames, exact reproduction values, and the incident that exposed a rule belong here. The full evidence record remains the provenance shown with a learning.

### Learning

A learning is a concise claim in the `learnings` ledger. It has `user` or `project` scope, kind, rationale, confidence, lifecycle status, and an optimistic `revision`. Typical lifecycle states are:

`candidate` → `validated` → `action_proposed` → `incorporated`

A proposal can instead become `rejected`; verified material can later become `stale`. Curator and critic agents may propose creates, evidence reinforcement/counters, duplicate/supersession/scope operations, reviewable wording revisions, destinations, or discards, but they do **not** accept or reject on the user's behalf. Current deterministic ingestion persists new candidates, evidence links, destination proposals, and pending semantic revisions; human decisions remain service operations.

A candidate claim should be less specific than its evidence while retaining the boundary needed to apply it. Curation applies six checks: novelty, a future trigger, transfer to another task of the same class, scope no broader than the evidence, actionability, and non-duplication. A repository-specific learning may name a subsystem or domain concept; it should not retain incident identifiers or exact reproduction details unless they are themselves a reusable invariant. Zero candidates is a valid and preferred outcome when nothing meets this bar.

Every mutating decision requires the current revision. Refresh with `legato learn show` after a conflict rather than retrying a stale decision.

### Searchable knowledge base

The learning ledger is also Legato's unified durable memory and knowledge base. Repository- or module-specific knowledge should remain project-scoped and carry a concise category plus normalized tags. It remains searchable without being injected into prompts; prompt activation is an explicit separate decision for high-leverage guidance.

`legato learn search <query>` is the agent-facing recall command. It always performs lexical FTS search. When `learning.embedding` is enabled, it also embeds the query, retrieves semantic candidates, and combines ranked lists with Reciprocal Rank Fusion. CLI search defaults to the current repository, validated/incorporated knowledge, a 0.55 minimum semantic cosine score, and 10 results. Use `--all-projects`, `--status all` (or a specific status), `--min-score`, and `--limit` to override those defaults. JSON items expose lexical/semantic ranks, semantic cosine score, and fused relevance score; the envelope reports effective `mode`, `semantic_status`, and fallback details. API and web search use the same underlying operation but supply their own filters.

```yaml
learning:
  embedding:
    enabled: true
    provider: openai-compatible
    model: text-embedding-nomic-embed-text-v1.5
    base_url: http://localhost:1234/v1
    api_key_env: OPENAI_API_KEY # optional for self-hosted endpoints
    timeout: 30s
    dimensions: 768             # 0 accepts provider dimensions
    batch_size: 32              # reserved for future batch backfill
```

Learning writes remain durable when embedding generation fails. Index state records the failure and later searches retry bounded stale/missing entries while lexical search remains available. Vectors live in additive ordinary SQLite tables and are ranked in Go behind `SemanticIndex`; PostgreSQL can replace the lexical adapter with `tsvector`/`tsquery` and the semantic adapter with pgvector without changing callers.

### Incorporation

An incorporation is a proposed use of an accepted learning, not another name for acceptance. It identifies a destination, optional target, and action. For example, a learning may be incorporated into documentation or into the special `prompt_profile` destination.

The lifecycle is:

1. Propose from a `validated` learning (`proposed`; the learning becomes `action_proposed`).
2. An implementation process performs the change and submits exact references plus a verification kind/result (`verification_pending`).
3. Deterministic target verification, or an explicit trusted user action through the Learning API/UI, passes or fails verification (`verified` or `failed`). A pass marks the learning `incorporated`.

Only a **verified** `prompt_profile` incorporation makes a validated/incorporated learning eligible for automatic launch-prompt context. A verified documentation incorporation does not.

## Collection and curation workflow

`legato learn curate --task <task-id>` launches the built-in `legato-learning` workflow:

1. `agent.interview` borrows the task's existing live agent and asks for a structured reflection. It does not launch a replacement. If no active task agent exists, the workflow fails at the interview step and no packet, curator, critic, or submission step runs.
2. The agent submits the reflection through the CLI callback described below. Valid observations become attributable evidence. An empty observation list is a successful result and creates no evidence.
3. `learning.packet.prepare` creates and persists canonical, bounded JSON containing unlinked relevant evidence and existing user/matching-project learnings. Whole records are included; records are never silently truncated. Item/byte exclusions are recorded, and the packet has a digest.
4. A configured ordinary `agent.run` curator receives the packet explicitly labelled as untrusted data and returns draft schema-versioned operations. An empty operations array is valid.
5. A second ordinary `agent.run` acts as an independent precision-first critic. It receives the same bounded packet and the untrusted curator draft, applies the six-part quality contract, and may preserve, rewrite, replace, or remove operations. It uses the curator adapter/tier and has no fallback to uncriticized output.
6. `learning.proposals.submit` consumes only critic output, strictly validates schema, packet digest, IDs, bounds, and operations, then applies deterministic mutations transactionally. Exact retries are replay-safe; changed retries conflict.

`legato learn consolidate` uses the same packet, curator, critic, and deterministic ingestion path over existing user or project learnings, without the interview step. Consolidation is currently manual. It may propose a `revise` operation for over-specific but evidence-backed wording. Accepting that proposal updates the learning under optimistic revision control and preserves its evidence; rejection leaves it unchanged. Existing records are never rewritten automatically.

### Workflow agent communication uses CLI callbacks

Legato does not infer success from terminal output or tmux exit. The borrowed task agent receives a request ID and must submit one JSON document:

```bash
cat >reflection.json <<'JSON'
{"schema_version":1,"observations":[
  {"kind":"pitfall","content":"When retrying callbacks, bind completion to the active attempt rather than the workflow run alone.","rationale":"A stale callback reproduced advancement of a newer attempt."}
]}
JSON
legato learn reflection submit --request <request-id> --file reflection.json
```

The version 1 document has two top-level fields: required integer `schema_version` (currently `1`) and required array `observations`. Each observation requires string fields `kind` and `content`. The string `rationale` and object `metadata` fields are optional. Recommended `kind` values are `invariant`, `convention`, `pitfall`, `preference`, `process_improvement`, `module_rule`, and `technical_fact`; other non-empty values are accepted.

The request can also come from `LEGATO_LEARNING_REQUEST_ID`; omitting `--file` or using `--file -` reads stdin. Submission validates and persists the reflection **before** completing the exact workflow run/step/attempt associated with the request. An exact replay is idempotent; a changed or stale replay fails. Submit `{"schema_version":1,"observations":[]}` when nothing durable was learned; this completes normally and creates no evidence. Limits currently include zero to 32 observations, 64 KiB per document, and 8 KiB per observation content/rationale.

After reflection is persisted and the bounded packet is prepared, the borrowed task agent remains online and associated with its original task. Curator and critic agents run under the separate generated workflow task, so their lifecycle does not require or affect the interviewed session.

The curator is a normal workflow agent. It completes its `agent.run` contract with the declared `operations` output, normally via:

```bash
legato workflow step complete --output operations='<schema-versioned JSON>' --json
```

The critic completes the same output contract after reviewing that draft. Only the critic output is followed by the deterministic `learning.proposals.submit` executor. See [workflows](../workflows.md) for the general callback contract.

## Configuration and opt-in behavior

Learning automation is opt-in:

```yaml
learning:
  # Automatic reflection/curation on configured task transitions.
  enabled: true
  # Global kill switch for composing enabled learnings into new agent prompts.
  # Defaults false; existing sessions are never modified.
  prompt_enabled: false
  transition_columns: [Review]
  # Optional overrides. Without curator_adapter, swarm.default_agent is used.
  curator_adapter: chimera
  curator_tier: careful
  # Usually omit this: task curation uses that task's checkout/worktree.
  # curator_working_dir: /absolute/path/to/checkout
  packet_max_items: 100
  packet_max_bytes: 262144
  reflection_timeout: 5m
  reflection_retries: 3
  prompt_max_items: 20
  prompt_max_bytes: 16384
  consolidation_automatic: false
  consolidation_threshold: 50
```

`learning.enabled` defaults to `false` and gates injection of built-in task-column triggers. When enabled, every named transition column must exist. `curator_adapter` overrides `swarm.default_agent`; one of them must name a configured adapter, and `curator_tier`, when set, must exist for that adapter. Task curation derives the curator working directory from the task's durable checkout/worktree. `curator_working_dir` is an optional global override and must be absolute when set. Triggered definitions ignore tasks marked as learning-workflow execution tasks, preventing recursion.

`enabled` controls only automatic transition-triggered reflection/curation. `prompt_enabled` independently gates launch-time prompt composition. The local ledger, CLI/API, web pages, and manual `learn curate`/`consolidate` remain available regardless. Accepted learnings start prompt-disabled; enablement is an explicit, reversible decision in Learning. Manual curation still needs a valid curator adapter in practice. `reflection_timeout`, `reflection_retries`, `consolidation_automatic`, and `consolidation_threshold` are reserved settings: they are parsed and validated but currently have no operational effect. Consolidation remains manual.

See [configuration](config.md) for defaults and validation.

## Privacy and retention

Legato Learn does **not** read Chimera JSONL, scrape tmux panes, or retain complete terminal/session transcripts. The interview asks an already-running agent to send a bounded typed reflection. Legato stores only the submitted observations and their attribution, explicit learning records, curation packet/operation audit documents, incorporation records, and prompt manifests in the local SQLite database.

Treat reflections as durable local data: do not submit secrets. Agent-provided content and metadata are marked untrusted, packet data is labelled untrusted in curator prompts, and external JSON decoders reject unknown fields. `learning.enabled: false` prevents automatic transition-triggered collection but does not erase existing data or disable the manual/API surfaces. There is currently no learning-specific export, bulk-delete, retention, or redaction command; database backup/deletion is an operator-level action and affects more than Learn.

## Project identity and scope isolation

User-scoped learnings can apply across projects. Project-scoped learnings require an exact stable project key.

Identity resolution prefers durable task `CodeContext.repo`, then repository identity inspected from the checkout, then a SHA-256 key derived from Git's common directory. Repository remotes normalize to lowercase keys such as `repo:github.com/acme/widget`; branch names and worktree paths are not identity. The Git-common-directory fallback avoids storing the local path itself in the key.

Task context retrieval uses the task's durable repository identity and does not guess a project from the caller's current directory. If no task repository identity is available, only user context is returned. Project consolidation resolves the current checkout when `--project` is omitted and otherwise fails rather than mixing projects. Manual recording requires `--project` for project scope; callers should use the stable key shown by existing records or resolved project workflows.

## Prompt eligibility and manifests

At each adapter-backed agent launch, `AgentService` compiles a deterministic `## Learned context` section, appends it to the role/general prompt, and then binds its manifest to the created agent session.

Candidates are considered only from user scope and the exact resolved project scope. A learning is included only when:

- its status is `validated` or `incorporated`; and
- it has a `prompt_profile` incorporation whose status is `verified`.

Rejected, stale, raw candidate, merely accepted-but-unincorporated, and non-prompt incorporations are excluded. Selection is deterministic and bounded by `prompt_max_items` and UTF-8 byte count. The persisted `learning_prompt_manifests` row records task, adapter, role, project key, exact included learning IDs/revisions, exclusions and reasons (`inactive`, `unverified_prompt_profile`, `non_prompt_incorporation`, `item_limit`, or `byte_limit`), and a SHA-256 digest of rendered text. It does not store another transcript.

The authenticated manifest API can inspect a known manifest ID. There is no CLI list command or web manifest browser yet.

## CLI reference

Successful learning commands currently emit raw JSON (the `--json` flag primarily selects structured error rendering).

```text
legato learn record [claim] --kind <kind>
  [--scope user|project] [--project <project-key>] [--project-label <label>]
  [--rationale <text>] [--task <id>] [--source-kind <kind>]
  [--source-ref <ref>] [--author <author>] [--json]

legato learn show <learning-id> [--json]
legato learn list [--scope user|project] [--status <status>]
  [--project <project-key>] [--limit <count>] [--json]
legato learn search <query> [--scope user|project]
  [--status <status>|all] [--project <project-key> | --all-projects]
  [--category <category>] [--tags <tag,...>] [--min-score <0..1>]
  [--limit <count>] [--json]
legato learn context --task <task-id> [--limit <count>] [--json]
legato learn curate --task <task-id> [--json]
legato learn consolidate --scope user|project [--project <project-key>] [--json]

legato learn accept <learning-id> --revision <n>
  [--claim <replacement>] [--scope user|project] [--project <key>] [--json]
legato learn edit <learning-id> --revision <n>
  (--claim <replacement> | --scope <scope> | --project <key>) [--json]
legato learn reject <learning-id> --revision <n> [--json]
legato learn enable <learning-id> --revision <n> [--json]
legato learn disable <learning-id> --revision <n> [--json]

legato learn incorporate <learning-id> --revision <n>
  --destination <destination> [--target <reference>] [--action <action>] [--json]
legato learn incorporation submit <incorporation-id> --revision <n>
  [--file <path|->] [--json]
legato learn verify <incorporation-id> --revision <n>
  --repo-root <path> [--confirm] [--json]
legato learn drift --repo-root <path> [--json]

legato learn reflection submit --request <request-id>
  [--file <path|->] [--json]
```

`context`, `curate`, and reflection submission can use `LEGATO_TASK_ID` or `LEGATO_LEARNING_REQUEST_ID` as documented above. `incorporation submit` accepts `references`, `verification_kind`, and `result`; `verification` may alternatively be a string or `{kind,result}` object.

## HTTP API

All routes are under the normal bearer-token middleware. Request JSON is strict (unknown fields and multiple JSON values are rejected).

| Method and path | Purpose |
|---|---|
| `GET /api/learnings` | List; filters: `status`, `scope`, `project_key`, `limit`. |
| `POST /api/learnings` | Record a `LearningRecordInput`; returns 201. |
| `GET /api/learnings/search?q=...` | FTS5 claim/rationale search with list filters (`query` is an alias for `q`). |
| `GET /api/learnings/context?task_id=...` | Context for a task; alternatively use exact `project_key`; supports `limit`. |
| `GET /api/learnings/prompt-preview?task_id=...` | Preview the exact enabled global/repository learning section for a new agent on that task. |
| `GET /api/learnings/{id}` | Learning, linked evidence, and incorporation history. |
| `POST /api/learnings/{id}/accept` | Accept with `{revision, claim?, scope?, project_key?}`. |
| `PATCH /api/learnings/{id}/edit` or `POST` | Edit and accept; at least one editable field is required. |
| `POST /api/learnings/{id}/reject` | Reject with `{revision}`. |
| `POST /api/learnings/{id}/prompt` | Reversibly enable/disable an accepted learning with `{revision,enabled}`. |
| `POST /api/learnings/{id}/incorporations` | Propose with `{revision,destination,target?,action?}`; returns 201. |
| `POST /api/learning-incorporations/{id}/submit` | Submit `{revision,references,verification_kind,result?}`. |
| `POST /api/learning-incorporations/{id}/verify` | Verify manually with `{revision,passed,result?}`. |
| `POST /api/learning-incorporations/{id}/verify-target` | Verify deterministically with `{revision,repository_root,confirmed?}`; `confirmed` is used for test/lint results. |
| `POST /api/learning-incorporations/drift` | Check verified targets for drift with `{repository_root}`. |
| `POST /api/learning-reflections/{id}/submit` | Persist a bounded reflection callback. Unlike the CLI callback, this endpoint does not itself advance the waiting workflow step. |
| `GET /api/learning-prompt-manifests/{id}` | Inspect one prompt audit manifest. |

Revision conflicts return 409, missing records 404, and validation errors 400. List/search limits default to 50 and cap at 200.

## Web UI and dedicated curation queue

The web app has `/learning` for filtering, FTS search, activation badges, and task-scoped effective prompt preview. `/learning/:learningId` provides candidate decisions, evidence provenance, incorporation history, and a reversible prompt Enable/Disable switch for accepted learnings. File/skill/check incorporation remains an advanced CLI/API workflow.

Learning candidates, proposals, activation, verification, and stale records stay in the Learning ledger. They are not projected into Attention and do not increment its badge. This keeps routine curation discoverable without weakening Attention's interrupt-only contract.

## Verification and drift

Incorporation submission proves only that an implementation reports references and a verification method; it remains `verification_pending`. Implementation agents can only invoke deterministic CLI verification against `--repo-root` (with `--confirm` only for test/lint targets). Explicit pass/fail authority remains limited to trusted Attention and authenticated server user actions. Passing verification changes the incorporation to `verified` and the learning to `incorporated`. Failing leaves the incorporation `failed` and does not make it prompt-eligible.

Deterministic verification is available with `legato learn verify <id> --revision <n> --repo-root <path>` or `POST /api/learning-incorporations/{id}/verify-target`. It checks supported destinations against local repository state. Safe file targets can be checked for existence, exact submitted content, or a submitted SHA-256 result; named skills/workflows are constrained to their supported repository roots. Prompt-profile wording must exactly match the approved learning. `--confirm` (API: `confirmed`) explicitly confirms `test_or_lint`; unsupported semantic destinations require a trusted Attention/server user action. Unsafe paths are rejected.

`legato learn drift --repo-root <path>` and `POST /api/learning-incorporations/drift` manually inspect verified incorporations whose targets are safe repository-relative regular files. Absolute, URL-like, fragment-bearing, traversal, and symlink-escape targets are reported `unverifiable` and left unchanged. Missing files or a stored SHA-256 mismatch mark both incorporation and learning stale. Historical references and verification data are retained. When no target digest was stored, an existing target can only be checked for existence/type, not content changes. Stale items are surfaced through Attention for review.

## Current limitation

Verification, drift checking, and consolidation have no automatic scheduling. Invoke the available CLI/API surfaces explicitly.
