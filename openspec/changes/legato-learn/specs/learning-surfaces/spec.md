## ADDED Requirements

### Requirement: Complete learning CLI and API
The system SHALL expose CLI and authenticated API operations to record, list, show, search, retrieve context, submit reflections, curate, accept/edit/reject, relate, incorporate, verify, consolidate, and inspect prompt manifests. User-fixable errors SHALL be returned rather than discarded.

#### Scenario: User explicitly records a preference
- **WHEN** `legato learn record` receives a valid user-scoped working preference
- **THEN** it SHALL create attributable evidence and a candidate learning visible through list/show APIs

### Requirement: Revision-aware human curation
Users SHALL be able to inspect evidence and curator reasoning, edit wording/scope/kind, accept, reject, mark duplicate/superseded, and choose or reject incorporation recommendations. Mutations SHALL require the current revision.

#### Scenario: Stale browser action
- **WHEN** a curation action submits an outdated revision
- **THEN** the API SHALL return a conflict and preserve current state

### Requirement: Learning Attention producer
Pending learning proposals, verification requests, and stale incorporations SHALL reconcile into the unified Attention registry with declared navigation and execution actions.

#### Scenario: Proposal accepted
- **WHEN** a user accepts a learning proposal through Attention
- **THEN** the action SHALL call the learning service, reconcile the producer, and resolve or revise the Attention item

### Requirement: Evidence-rich web curation
The web UI SHALL provide learning list/detail surfaces showing scope, status, claim, source quotations/references, curator reasoning, existing matches, counterevidence, incorporation history, and available user actions.

#### Scenario: Review a pending proposal
- **WHEN** a user opens a pending learning
- **THEN** they SHALL be able to trace it to its task/session/artifact evidence and edit its wording/scope before accepting

### Requirement: Opt-in automation and privacy
Automatic transition-triggered learning SHALL be disabled by default and configurable by transition columns, curator adapter/tier, limits, and prompt budgets. The UI/docs SHALL disclose captured data and SHALL NOT expose arbitrary session-log reads.

#### Scenario: Learning is disabled
- **WHEN** a task enters Review with `learning.enabled` false
- **THEN** no learning workflow or model-costing agent run SHALL start and existing task behavior SHALL remain unchanged
