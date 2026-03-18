## ADDED Requirements

### Requirement: SyncService interface definition

The system SHALL define a `SyncService` interface in `internal/service/interfaces.go` with the methods `Sync`, `Status`, and `Subscribe`. The interface MUST NOT import any presentation-layer packages or any Jira client packages.

#### Scenario: Interface is implementation-agnostic

- **WHEN** the `SyncService` interface is defined
- **THEN** it SHALL be possible to swap between a stub implementation and a real Jira-backed implementation without changing any consumer code

### Requirement: Stub SyncService seeds fake ticket data

The stub `SyncService` implementation SHALL seed the SQLite store with realistic fake Jira ticket data on first `Sync` call. The fake data MUST include at least 8 tickets distributed across multiple columns with varied priorities, issue types, labels, and descriptions.

#### Scenario: First sync seeds data

- **WHEN** `Sync` is called for the first time on the stub implementation
- **THEN** it SHALL insert fake tickets into the store across at least 3 different columns, with varied metadata, and return a `*SyncResult` indicating the number of tickets synced

#### Scenario: Subsequent syncs are idempotent

- **WHEN** `Sync` is called after the initial seed
- **THEN** it SHALL not duplicate tickets and SHALL return a `*SyncResult` with zero new tickets

#### Scenario: Fake data includes realistic descriptions

- **WHEN** fake tickets are seeded
- **THEN** at least some tickets SHALL have multi-paragraph markdown descriptions including headings, lists, and acceptance criteria sections

#### Scenario: Edge cases in fake data

- **WHEN** fake tickets are seeded
- **THEN** at least one ticket SHALL have an empty description, and at least one SHALL have a summary longer than 60 characters, to exercise edge cases in rendering

### Requirement: Stub Sync publishes events

The stub `SyncService.Sync` SHALL publish `EventSyncStarted` before processing, and either `EventSyncCompleted` and `EventCardsRefreshed` on success, or `EventSyncFailed` on failure, through the `EventBus`.

#### Scenario: Successful sync publishes events in order

- **WHEN** `Sync` is called and completes successfully
- **THEN** the following events SHALL be published in order: `EventSyncStarted`, `EventCardsRefreshed`, `EventSyncCompleted`

#### Scenario: Subscriber receives sync events

- **WHEN** a consumer has called `Subscribe` before `Sync` is called
- **THEN** the subscriber's channel SHALL receive `SyncEvent` values for each stage of the sync

### Requirement: Stub Status reports sync state

`SyncService.Status` SHALL return the current sync status, including whether a sync is in progress and the timestamp of the last successful sync.

#### Scenario: Before any sync

- **WHEN** `Status` is called before any `Sync` has been performed
- **THEN** it SHALL return a `SyncStatus` indicating no sync has occurred and the last sync time SHALL be zero-valued

#### Scenario: After successful sync

- **WHEN** `Status` is called after a successful `Sync`
- **THEN** it SHALL return a `SyncStatus` indicating the sync is not in progress and the last sync time SHALL be the time of the most recent sync

### Requirement: Stub Subscribe provides event channel

`SyncService.Subscribe` SHALL return a receive-only channel that delivers `SyncEvent` values whenever a sync operation begins, completes, or fails.

#### Scenario: Channel delivers events

- **WHEN** `Subscribe` is called and then `Sync` is invoked
- **THEN** the returned channel SHALL receive events corresponding to the sync lifecycle

#### Scenario: Multiple subscribers

- **WHEN** `Subscribe` is called multiple times
- **THEN** each returned channel SHALL independently receive all sync events
