## ADDED Requirements

### Requirement: Attributable learning evidence
The system SHALL persist bounded evidence independently from accepted learnings, with source identity, author, task/session/workflow context, project identity where known, content digest, and timestamp. Recollection of the same unchanged source SHALL be idempotent.

#### Scenario: Recollecting durable feedback
- **WHEN** the same submitted plan comment is collected twice without changing its source identity or content
- **THEN** exactly one evidence record SHALL exist and retain the original author and source reference

### Requirement: Stable user and project scope
The system SHALL support user-global and project-scoped learnings. Project identity SHALL be stable across worktrees by preferring normalized repository identity and otherwise Git common-directory identity; unresolved repositories SHALL NOT be silently assigned project scope.

#### Scenario: Sibling worktrees share learning
- **WHEN** two tasks run in separate worktrees of the same repository
- **THEN** both SHALL resolve to the same project key and retrieve the same project learning

#### Scenario: Unrelated repositories are isolated
- **WHEN** a project learning is recorded for one repository
- **THEN** context retrieval for an unrelated repository SHALL exclude it

### Requirement: Atomic learning lifecycle
The system SHALL store concise learning claims with kind, scope, provenance, lifecycle, confidence metadata, and supporting/counter evidence relations. Reinforcement SHALL attach evidence to an existing learning rather than require a duplicate claim.

#### Scenario: Curator reinforces an existing learning
- **WHEN** a validated curator operation cites new supporting evidence and an existing learning ID
- **THEN** the evidence SHALL be linked to that learning without creating another learning

#### Scenario: Inference is not acceptance
- **WHEN** an agent reflection or curator proposes a learning
- **THEN** it SHALL remain a candidate until an explicit user acceptance action succeeds

### Requirement: Verified incorporation
The system SHALL record an incorporation destination, target/reference, action, implementation links, and verification result separately from the learning. A learning SHALL become incorporated only when its active incorporation is verified.

#### Scenario: Agent reports a documentation edit
- **WHEN** an incorporation agent submits a destination and references
- **THEN** the incorporation SHALL become verification-pending and the learning SHALL NOT yet be incorporated

#### Scenario: Verification succeeds
- **WHEN** deterministic verification or explicit review/user confirmation succeeds for a verification-pending incorporation
- **THEN** the incorporation SHALL become verified and the learning SHALL become incorporated

### Requirement: Learning retrieval and audit
The system SHALL provide scoped full-text search, task context retrieval, and a complete audit view linking evidence, curator operations, user decisions, incorporations, verification, and supersession history.

#### Scenario: Technical fact incorporated in documentation
- **WHEN** a project fact has a verified documentation incorporation
- **THEN** search SHALL return the fact and its documentation reference while prompt context SHALL exclude it unless it separately has a verified prompt-profile incorporation

### Requirement: Incorporation drift
The system SHALL retain exact incorporation references or digests and SHALL allow verified incorporations to become stale without deleting their history.

#### Scenario: Tracked target changes
- **WHEN** a drift check finds that a tracked exact target or digest no longer matches
- **THEN** the incorporation and learning SHALL be marked stale and surfaced for curation
