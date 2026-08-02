## 1. Change and Persistence Foundation

- [x] 1.1 Retire the superseded `knowledge-memory` artifacts and update stale references; validate with `openspec validate legato-learn --strict` and repository grep.
- [x] 1.2 Add additive migrations for evidence, reflection requests, learnings, evidence relations, curation generations, incorporations, and prompt manifests; verify upgrade and fresh-database tests.
- [x] 1.3 Add store types and CRUD/query methods with source idempotency, lifecycle revision checks, FTS retrieval, and transactional callbacks; verify store tests through public methods.
- [x] 1.4 Implement stable project identity from `CodeContext`, checkout remote, or Git common directory; verify sibling worktrees converge and unrelated/unresolved repositories remain isolated.
- [x] 1.5 Add `LearningService` lifecycle operations and typed learning events; verify candidate, reinforce/counter, duplicate/supersede, accept/reject, incorporation, verification, and stale transitions.

## 2. Evidence and Reflection

- [x] 2.1 Implement bounded idempotent collectors for plan comments/responses, review Q&A/guidance, and explicit records; verify author/source provenance.
- [x] 2.2 Define and validate versioned reflection and learning-packet schemas with count/byte/depth limits and untrusted-data labeling.
- [x] 2.3 Add reflection request creation/submission and `legato learn reflection submit`; verify malformed, stale, and replayed callbacks through CLI/service entry points.
- [x] 2.4 Add borrowed-session semantics and the `agent.interview` workflow executor using Chimera HTTP or tmux delivery; verify completion/cancel/retry never cleans up the target task session.
- [x] 2.5 Implement typed unavailable-session behavior and prove a triggering task move remains committed when delivery fails.

## 3. Curator Workflow

- [x] 3.1 Add deterministic `learning.packet.prepare` executor and managed packet artifact/output; verify deterministic selection and recorded exclusions.
- [x] 3.2 Add strict curator operation validation and `learning.proposals.submit`; verify unknown IDs, forbidden acceptance/incorporation, partial failure, and replay safety.
- [x] 3.3 Ship the built-in interview → packet → curator `agent.run` → ingestion workflow with configurable adapter/tier and no embedded inference dependency.
- [x] 3.4 Add opt-in idempotent transition triggering, recursion exclusion, and startup validation; verify exactly one run per configured transition and unchanged behavior when disabled.
- [x] 3.5 Add manual curator and consolidator launch paths; verify repeated generations reinforce rather than duplicate proposals.

## 4. Human Curation Surfaces

- [x] 4.1 Add the complete `legato learn` CLI family for record/list/show/search/context and proposal lifecycle; verify JSON and actionable stderr behavior.
- [x] 4.2 Add authenticated learning APIs with revision-aware mutations and WebSocket events; verify auth, validation, and stale conflicts.
- [x] 4.3 Add a learning Attention producer for pending proposals, verification requests, and stale incorporations; verify event-driven reconciliation and actions.
- [x] 4.4 Add web learning list/detail/curation pages showing evidence, reasoning, scope, matches, counterevidence, and incorporation history; verify component/API tests.
- [x] 4.5 Add TUI Attention/navigation support or explicitly document web-first rich curation while preserving complete CLI parity; verify no dead-end Attention action.

## 5. Incorporation and Verification

- [x] 5.1 Add destination recommendation and explicit user-approved incorporation request creation linked to normal task/workflow/review lifecycle.
- [x] 5.2 Add incorporation-agent callback and verification-pending state; verify agent reports cannot directly mark a learning incorporated.
- [x] 5.3 Implement deterministic path/content/digest/named-resource verification plus explicit semantic confirmation; verify success/failure/retry history.
- [x] 5.4 Implement startup/manual/touched-reference drift checks; verify tracked changes mark records stale without deleting provenance.

## 6. Retrieval and Prompt Context

- [x] 6.1 Implement scoped FTS search and task context retrieval with deterministic status/project/user filters and limits.
- [x] 6.2 Add service-level learning prompt compilation with eligibility, deterministic ordering, and item/byte budgets; verify raw and non-prompt learnings are excluded.
- [x] 6.3 Integrate standing context without replacing assignments across Chimera, Claude Code, and Codex standalone/workflow/conductor/worker launches.
- [x] 6.4 Persist and expose launch prompt manifests with included versions, scope resolution, exclusions, and rendered digest; verify manifests match actual prompt content.

## 7. Configuration, Documentation, and Acceptance

- [x] 7.1 Add and validate opt-in learning configuration for transitions, curator adapter/tier, packet limits, reflection behavior, consolidation, and prompt budgets.
- [x] 7.2 Document architecture, privacy/retention, database, config, CLI, workflows, web UI, and package ownership in project docs and `CLAUDE.md` references.
- [x] 7.3 Add an end-to-end test covering transition, interview callback, curator callback, human decisions, non-prompt and prompt incorporation, verification, worktree scope, adapter launches, and staleness.
- [x] 7.4 Dogfood the real public transition → workflow → callbacks → Attention → verification → launch path and record any limits without relying only on disconnected tests.
- [x] 7.5 Run `task test`, targeted race tests for events/Attention, `task vet`, `task lint`, `task build`, and strict OpenSpec validation; fix all regressions. (`task lint` remains blocked only by the repository's 23 pre-existing unused legacy CLI functions; it reports no Legato Learn files.)
