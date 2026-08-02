## ADDED Requirements

### Requirement: Bounded evidence packet
The system SHALL deterministically prepare a versioned, size-bounded packet containing task/project context, new evidence, and relevant existing learnings with strict IDs, authors, and untrusted-data delimiters.

#### Scenario: Packet exceeds configured budget
- **WHEN** available evidence exceeds packet count or byte limits
- **THEN** Legato SHALL select deterministically, record exclusions, and SHALL NOT silently truncate individual evidence semantics

### Requirement: Workflow-backed curator
The system SHALL run curation through an ordinary configurable `agent.run` workflow step and SHALL NOT require an embedded inference provider.

#### Scenario: Curator runs on configured adapter and tier
- **WHEN** a learning workflow reaches curation
- **THEN** Legato SHALL launch the configured adapter/tier with the prepared packet and normal workflow completion contract

### Requirement: Constrained curator operations
Curator output SHALL be versioned operations limited to creating a candidate, attaching support/counterevidence, proposing duplicate/supersession/scope/destination, or discarding unsupported/task-local material. Curator output SHALL NOT accept, incorporate, verify, mutate repositories, or delete history.

#### Scenario: Curator cites unknown evidence
- **WHEN** proposal ingestion encounters an evidence or learning ID absent from its packet
- **THEN** it SHALL reject the output atomically with an actionable error

#### Scenario: Curator attempts acceptance
- **WHEN** curator output requests an accepted or incorporated state
- **THEN** ingestion SHALL reject that operation and persist no partial mutation

### Requirement: Idempotent curation generations
The system SHALL track curation generations and processed evidence so retries do not duplicate proposals and later independent evidence can reinforce an existing learning.

#### Scenario: Completion callback is replayed
- **WHEN** the same curator output is submitted again for one attempt
- **THEN** the existing generation result SHALL be returned without duplicate candidates or relations

### Requirement: Consolidation proposals
The system SHALL offer a manual consolidator workflow that can propose merges, wording/scope changes, supersession, staleness, or a better incorporation destination but requires the same human decisions as initial curation.

#### Scenario: Consolidator finds duplicate accepted learnings
- **WHEN** consolidation proposes a canonical learning and duplicate relation
- **THEN** the proposal SHALL enter curation review and SHALL NOT rewrite accepted records automatically
