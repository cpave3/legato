## Why

Legato users repeatedly give the same feedback across agent sessions, while each Chimera, Claude Code, or Codex agent starts without durable knowledge of user preferences or project discoveries. Legato already owns agent lifecycle, workflows, task/repository context, plans, reviews, and attention, so it can turn ephemeral session experience into evidence-backed, human-curated improvements without embedding an inference provider.

## What Changes

- Add a local learning ledger that separates immutable session/user evidence, atomic user- or project-scoped learnings, and verified incorporation records.
- Add a workflow executor that interviews an existing task agent through Legato's normal interaction transport and accepts a typed reflection through the Legato CLI/API.
- Add deterministic evidence-packet preparation and proposal ingestion around an ordinary `agent.run` curator, keeping all inference in configured agent workflows.
- Add human curation through CLI, API, Attention, and web surfaces: accept/edit/reject, reinforce, deduplicate, supersede, scope, and choose an incorporation destination.
- Track how accepted learnings are incorporated into documentation, agent instruction files, skills, workflows, checks, task notes, existing sources, or a bounded prompt profile; require verification before marking them incorporated.
- Add scoped retrieval and deterministic prompt compilation for only verified prompt-profile learnings, with a per-launch manifest.
- Add consolidation and drift checks that produce proposals rather than silently rewriting accepted memory.
- Retire the unimplemented `knowledge-memory` change. This change supersedes its original motivation but does not add a markdown wiki, wikilink graph, MCP server, or task-note migration.

## Capabilities

### New Capabilities
- `learning-ledger`: Evidence, atomic learnings, scope, provenance, lifecycle, deduplication, incorporation, verification, retrieval, and staleness.
- `session-reflection`: Non-blocking interviews of existing agents with durable typed callbacks and unavailable-session behavior.
- `learning-curation`: Workflow-backed evidence packets, curator proposals, consolidation, and strict deterministic ingestion.
- `learning-surfaces`: CLI, authenticated API, Attention producer, and web curation/retrieval surfaces.
- `learning-prompt-context`: Scoped, budgeted prompt-profile compilation and inspectable launch manifests.

### Modified Capabilities
- `agent-session`: Existing sessions can be borrowed for an interview without transferring ownership or cleanup responsibility.
- `legato-cli`: Adds the `learn` command family and workflow callbacks for reflection and incorporation.
- `ai-tool-adapter`: All supported adapter launch paths receive equivalent service-composed learning context without owning memory logic.
- `local-task-management`: Configured local task transitions can trigger a non-blocking built-in learning workflow.

## Impact

- New additive SQLite migrations and FTS indexes for evidence, reflections, learnings, relations, incorporations, curation generations, and prompt manifests.
- New store and service modules plus typed events; workflow schema/executors gain interview and learning operations.
- Agent prompt assembly moves to a service-level composition step shared by Chimera, Claude Code, and Codex.
- New CLI/API routes, Attention producer, and web pages; configuration controls opt-in automation, adapter/tier, transition columns, packet limits, and prompt budgets.
- No model SDK or provider dependency is added. Automatic inference is disabled by default and runs through existing configured agents.
