## Context

Legato persistently coordinates tasks, agent sessions, plans, reviews, workflows, repository identity, and human attention, but it does not retain what agents or users learn across sessions. Existing workflows already provide the desired inference boundary: deterministic Legato code launches a configured agent, gives it typed inputs, and validates typed outputs. Chimera sessions can be messaged through HTTP; other sessions use the existing tmux interaction path.

The system must support user-global preferences and project-specific facts across worktrees, avoid prompt bloat, preserve provenance, and never let an inferred claim silently become accepted truth or mutate a repository. The old `knowledge-memory` proposal was document/wiki-oriented and is superseded.

## Goals / Non-Goals

**Goals:**
- Close an auditable loop from session/user evidence through reflection, curation, human decision, incorporation, verification, retrieval, and future-agent context.
- Keep Legato deterministic and provider-neutral; use ordinary workflow agents for inference.
- Interview an existing task agent without taking ownership of its session.
- Share project learning across worktrees with stable repository identity and user learning across repositories.
- Route most learnings to canonical documentation, skills, workflows, or checks; inject only verified residual preferences.
- Keep all agent submissions typed, durable, idempotent, and inspectable.

**Non-Goals:**
- Embedded model-provider calls, embeddings, or an external memory service.
- Full JSONL/terminal transcript ingestion in the MVP.
- Automatic repository/skill/instruction mutation from curator output.
- Markdown wiki files, wikilinks, graph visualization, MCP, or task-note migration.
- Multi-user synchronization or autonomous deletion of history.

## Decisions

### 1. Evidence, learning, and incorporation are separate records

Evidence is append-only attributable input; a learning is a curated claim with scope/lifecycle; incorporation records where and how an accepted learning was actioned and verified. This prevents agent self-report from becoming truth and allows many evidence items to reinforce or contradict one learning.

**Alternative:** store one mutable memory record. Rejected because it loses provenance, independent reinforcement, counterevidence, and action history.

### 2. All inference runs through workflows

An existing-session `agent.interview` executor sends one structured reflection request. A separate `agent.run` curator receives a bounded packet and submits operations through workflow outputs. Deterministic executors prepare packets and ingest proposals.

**Alternative:** add an AI SDK to the service. Rejected because it duplicates agent configuration, retries, observability, cost controls, and adapter support already provided by workflows.

### 3. Agents communicate only through Legato

The worker submits reflection with `legato learn reflection submit`; the curator consumes a persisted packet and submits a workflow output. They do not chat directly. Callbacks validate request/attempt identity and schemas before advancing durable state.

This survives process exits, supports replay, and keeps adapter behavior equivalent.

### 4. Interviews borrow sessions without owning them

Workflow attempts distinguish owned sessions (`agent.run`) from borrowed sessions (`agent.interview`). Cancellation/finish cleans up only owned sessions. An unavailable task session yields a typed unavailable result and does not block task movement or downstream curation of existing feedback.

### 5. Project identity is repository-based

Resolution prefers normalized `CodeContext.Repo`, then checkout remote identity, then a hash of Git's common directory for no-remote repositories. Branch/worktree paths are labels, not identity. Unresolved evidence remains task-scoped until explicitly scoped.

### 6. SQLite is the local source of truth

Add normalized tables and FTS5 indexes for bounded evidence/learning text. Generated one-line markdown files are avoided. Exact source identities/digests make collection idempotent; revision/state predicates protect callbacks and UI actions.

### 7. Curators emit constrained operations

A versioned packet contains strict evidence and learning IDs. Curator operations may create a candidate, reinforce/counter an existing learning, propose duplicate/supersession/scope/destination, or discard. Ingestion rejects unknown IDs and cannot accept or incorporate anything.

### 8. Human curation uses Attention and normal product surfaces

Pending proposals and verification requests are Attention items. CLI and authenticated APIs expose all lifecycle operations; the web UI supplies evidence-rich review. User actions are revision-aware and stale actions conflict.

### 9. Incorporation is recommendation-first and verification-gated

A user may launch normal implementation work from an accepted recommendation. Agent callbacks submit exact references and verification evidence into `verification_pending`; deterministic checks or explicit review/user confirmation move it to verified. Only then is the learning incorporated.

### 10. Prompt context is a residual, deterministic destination

Only active learnings with verified `prompt_profile` incorporations matching user/project scope are rendered. The service composes this section above adapter delivery under item/byte budgets and stores a manifest of included versions and rendered digest. No inference occurs at launch.

### 11. Automatic learning is opt-in

`learning.enabled` defaults false. Configured transition columns trigger the built-in workflow idempotently, excluding learning workflow tasks to prevent recursion. Manual record/curate paths permit dogfooding before automation.

### 12. JSONL extraction is an extension point

The MVP defines a session evidence source contract but does not read `~/.chimera/sessions`. If interviews prove insufficient, a later Chimera implementation can provide bounded, redacted excerpts without changing evidence or curator schemas.

## Risks / Trade-offs

- **Agent hallucination** → Treat output as evidence/proposals, require citations and human acceptance.
- **Prompt injection from evidence** → Bound and author-label packets, delimit them as untrusted data, and validate constrained operations.
- **Prompt bloat** → Only verified prompt-profile incorporations enter a deterministic budget; retrieval handles the rest.
- **Unexpected model cost** → Opt-in automation and configurable adapter/tier; every inference is a visible workflow run.
- **Live-session disruption** → One bounded prompt, borrowed-session ownership, idempotent callback, and non-blocking unavailable behavior.
- **Worktree leakage** → Stable repository/common-dir identity and tests across sibling/unrelated worktrees.
- **Stale canonical facts** → Store target references/digests and run startup/manual/touched-file drift checks.
- **UI scope** → CLI/API remain complete; rich curation is web-first while TUI can rely on Attention navigation.

## Migration Plan

1. Add forward-only tables/indexes; existing data and behavior remain unchanged.
2. Ship explicit record/search and manual curation with automation disabled.
3. Add interview/curator workflow and test against fake adapters before enabling transition triggers.
4. Add human curation/incorporation and prompt context; no-learning launches retain byte-for-byte prior behavior where practical.
5. Dogfood on Legato, then enable configured Review transitions.
6. Remove the unimplemented `knowledge-memory` change and stale references once the new artifacts are valid.
7. Rollback disables learning/prompt composition; additive tables may remain without affecting existing features.

## Open Questions

None blocking. Full Chimera JSONL extraction, periodic schedules, export/import, and MCP are explicit follow-ups driven by dogfooding evidence.
