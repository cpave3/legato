## Requirements

### Requirement: Durable task code context

The system SHALL store repository identity independently from refreshable PR state. Code context SHALL include repository, optional branch/head SHA/working directory/base branch, provenance, and timestamps. PR metadata SHALL contain remote PR state, pin/match method, and synchronization outcome/error.

#### Scenario: Existing data migration
- **WHEN** a database with legacy `pr_meta` is opened
- **THEN** repository/branch/SHA identity SHALL be backfilled to code context, visible PR data retained, and existing numbered links pinned

#### Scenario: Missing context
- **WHEN** an active task has no repository context
- **THEN** GitHub discovery SHALL skip it without affecting other task behavior or guessing from titles/arbitrary repositories

### Requirement: Context capture and precedence

Context SHALL be captured from manual edits, task worktrees (including external worktree metadata), exact agent working directories, and unique configured Jira-project/workspace repository mappings. Authority SHALL be `manual > worktree > agent > repository mapping > inferred branch`; lower-authority evidence MUST NOT overwrite stronger context.

### Requirement: Conservative repository-batched discovery

Unresolved tasks SHALL be grouped by known repository and each repository listed once per cycle. Matching SHALL prefer exact head SHA, then exact repository+branch, then exactly one boundary-aware task/remote-ticket key branch. Default/detached branches, title-only matches, wrong fork identities, and ambiguous candidates MUST NOT auto-link.

#### Scenario: Unique match
- **WHEN** exactly one candidate satisfies the highest available confidence rule
- **THEN** its PR SHALL be linked and the match method recorded

#### Scenario: Ambiguous match
- **WHEN** multiple plausible candidates remain
- **THEN** no PR SHALL be linked and the task outcome SHALL be `ambiguous`

#### Scenario: API failure
- **WHEN** GitHub returns an authentication, access, rate-limit, network, or server failure
- **THEN** last-good PR state SHALL be retained and an actionable error outcome recorded

### Requirement: Pinned links and targeted repair

Manual PR selection and migrated numbered links SHALL be pinned. Background synchronization SHALL refresh pinned PRs by number without replacing them.

- Refresh SHALL refetch the current PR without reconsidering identity.
- Rediscover SHALL clear cached/pinned PR identity, retain code context, and rerun matching immediately.
- Unlink SHALL clear both PR state and code context so polling cannot relink automatically.
- Editing repository or branch identity SHALL clear stale derived PR state.

### Requirement: Polling and live updates

Active non-ephemeral tasks SHALL synchronize on configurable unresolved/resolved cadences. Archived and ephemeral tasks SHALL be excluded. Startup/global force sync SHALL process all eligible tasks. Meaningful resolved or unresolved state/outcome changes SHALL publish `EventPRStatusUpdated`; no-change cycles SHALL not publish.

### Requirement: User access

CLI, TUI, web/API, and task detail responses SHALL expose code context, sync outcomes, manual linking, targeted refresh, and targeted rediscovery without exposing credentials. Staccato hooks MAY continue using explicit link commands but SHALL NOT be required for discovery.
