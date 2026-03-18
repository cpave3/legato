## ADDED Requirements

### Requirement: Pull Sync from Jira to SQLite

The sync service SHALL pull tickets from Jira using the configured JQL query and upsert them into the local SQLite database. On pull, the service MUST map each ticket's Jira status to the appropriate local kanban column using the configured column mappings.

#### Scenario: New ticket from Jira

- **WHEN** a pull sync finds a Jira issue that does not exist in SQLite
- **THEN** the ticket is inserted into SQLite with the local column derived from the Jira status-to-column mapping

#### Scenario: Updated ticket from Jira

- **WHEN** a pull sync finds a Jira issue that exists in SQLite and the Jira `updated` timestamp is newer than the stored `jira_updated_at`
- **THEN** the ticket fields (summary, description, priority, labels, etc.) are updated in SQLite

#### Scenario: Jira status changed externally with no pending local move

- **WHEN** a pull sync detects that a ticket's Jira status maps to a different column than the local column and there is no pending local move for that ticket
- **THEN** the ticket's local column is updated to match the Jira status mapping

#### Scenario: Jira status changed externally with pending local move within window

- **WHEN** a pull sync detects a Jira status change but the ticket has a pending local move that occurred within the last 5 minutes
- **THEN** the local column is preserved (local wins) and the Jira status change is ignored

#### Scenario: ADF description conversion on pull

- **WHEN** a ticket is pulled from Jira with an ADF description
- **THEN** the ADF description is converted to Markdown and stored in the `description_md` field

### Requirement: Stale Ticket Handling

The sync service SHALL detect tickets present in SQLite but absent from the Jira query results. Stale tickets MUST be handled according to configuration (default: hide after 7 days, retain in database).

#### Scenario: Ticket no longer in Jira results

- **WHEN** a pull sync completes and a ticket in SQLite was not returned by the JQL query
- **THEN** the ticket is marked as stale with the current timestamp

#### Scenario: Stale ticket exceeds retention period

- **WHEN** a stale ticket has been absent from Jira results for longer than the configured retention period (default 7 days)
- **THEN** the ticket is hidden from the board view but retained in the SQLite database

### Requirement: Push Sync from Local to Jira

The sync service SHALL push local card movements to Jira as transitions. When a card is moved in the UI, the local SQLite state MUST be updated immediately and the Jira transition MUST be queued for async execution.

#### Scenario: Successful push transition

- **WHEN** a card is moved to a new column and the Jira transition succeeds
- **THEN** the `jira_status` field in SQLite is updated to reflect the new Jira status and a success entry is written to `sync_log`

#### Scenario: Failed push transition

- **WHEN** a card is moved to a new column and the Jira transition fails
- **THEN** the card remains in the local column (user intent preserved), a warning indicator is set on the card, the error is surfaced in the status bar, and a failure entry is written to `sync_log`

#### Scenario: Push is non-blocking

- **WHEN** a card move triggers a push sync
- **THEN** the UI remains responsive and does not block waiting for the Jira API response

#### Scenario: Retry failed push on manual sync

- **WHEN** a user triggers a manual sync (r key) and there are previously failed push operations
- **THEN** the failed transitions are retried

### Requirement: Conflict Resolution

The sync service SHALL resolve conflicts between local and remote state changes using a local-wins-within-window strategy. The conflict window MUST default to 5 minutes from the time of the local move.

#### Scenario: Local move within conflict window

- **WHEN** a ticket was moved locally within the last 5 minutes and a pull sync detects a different Jira status
- **THEN** the local column is preserved and the Jira status change is not applied

#### Scenario: Local move outside conflict window

- **WHEN** a ticket was moved locally more than 5 minutes ago and a pull sync detects a different Jira status
- **THEN** the Jira status takes priority and the local column is updated to match

#### Scenario: Conflict logging

- **WHEN** a conflict is detected between local and remote state
- **THEN** the conflict is logged to `sync_log` with details about both the local and remote states

### Requirement: Periodic Sync

The sync service SHALL execute pull syncs on a configurable interval. The default interval MUST be 60 seconds. The interval MUST be configurable via `sync_interval_seconds` in the config file.

#### Scenario: Automatic periodic sync

- **WHEN** the configured sync interval elapses
- **THEN** a pull sync is triggered automatically

#### Scenario: Sync status reporting

- **WHEN** a sync operation starts, completes, or fails
- **THEN** the sync service publishes appropriate events (SyncStarted, SyncCompleted, SyncFailed) on the event bus

### Requirement: Offline Resilience

The sync service SHALL handle network failures gracefully. When the Jira API is unreachable, the service MUST continue operating from local SQLite data.

#### Scenario: No network on startup

- **WHEN** Legato starts and cannot reach the Jira API
- **THEN** the board loads from SQLite data and the status bar shows "offline" status

#### Scenario: Network loss during operation

- **WHEN** a sync attempt fails due to network error
- **THEN** the board continues to display local data, the status bar shows the error, and sync retries on the next interval

#### Scenario: Network recovery

- **WHEN** the network becomes available after a period of offline operation
- **THEN** the next sync attempt succeeds and pulls any changes that occurred while offline
