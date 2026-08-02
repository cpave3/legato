## ADDED Requirements

### Requirement: Eligible learning prompt context
Only active learnings with verified `prompt_profile` incorporations matching user scope or the resolved project SHALL be eligible for standing launch context. Evidence, candidates, stale/rejected learnings, and non-prompt destinations SHALL be excluded.

#### Scenario: Project documentation fact and user preference
- **WHEN** a documentation-incorporated project fact and prompt-incorporated user preference are relevant
- **THEN** the launch prompt SHALL include only the user preference while scoped retrieval can return both

### Requirement: Deterministic bounded compilation
Prompt learning context SHALL be compiled without inference using deterministic ordering and configurable item/byte budgets.

#### Scenario: Eligible preferences exceed budget
- **WHEN** eligible entries exceed the launch budget
- **THEN** Legato SHALL include a deterministic subset and record excluded learning IDs in the launch manifest

### Requirement: Adapter-neutral prompt composition
Learning context SHALL be composed in the service layer and delivered equivalently to Chimera, Claude Code, and Codex across standalone, workflow, conductor, and worker launches without replacing task assignments.

#### Scenario: Self-kickoff adapter workflow assignment
- **WHEN** Chimera launches a workflow agent with scoped learning context
- **THEN** standing context and the workflow assignment SHALL both be delivered in their intended roles and the assignment SHALL NOT overwrite learning context

### Requirement: Prompt manifest
Every launch that resolves learning context SHALL persist an inspectable manifest containing project identity, included learning IDs/versions, budget exclusions, rendered digest, adapter, role, and session/task identity.

#### Scenario: User asks why an instruction was present
- **WHEN** the launch manifest is requested for an agent session
- **THEN** Legato SHALL return the exact learning versions included and the scope decision that selected them

### Requirement: On-demand context retrieval
Agents SHALL be able to query additional relevant learnings through a deterministic Legato context command rather than receiving all memory at launch.

#### Scenario: Agent enters a specialized code area
- **WHEN** it runs `legato learn context --task <id>`
- **THEN** the result SHALL contain scoped active learnings and canonical incorporation references subject to retrieval limits
