## Requirements

### Requirement: Pull Sync from Jira to SQLite

The sync service SHALL pull tickets from Jira and convert them into legato tasks with `provider='jira'` and `remote_id` set to the Jira issue key. Jira-specific fields (remote_status, issue_type, assignee, labels, epic, URL, stale_at, local_move_at, remote_transition) SHALL be stored in the `remote_meta` JSON field.

#### Scenario: New ticket from Jira

- **WHEN** a pull sync finds a Jira issue that does not exist as a task in SQLite
- **THEN** a new task is created with `provider='jira'`, `remote_id` set to the Jira key, title from summary, description from ADF conversion, status from column mapping, and Jira-specific fields packed into `remote_meta`

#### Scenario: Updated ticket from Jira

- **WHEN** a pull sync finds a Jira issue that exists as a task and the Jira `updated` timestamp is newer
- **THEN** the task's title, description, priority, and `remote_meta` fields are updated

#### Scenario: Jira status changed externally with no pending local move

- **WHEN** a pull sync detects that a task's Jira status maps to a different column and there is no pending local move
- **THEN** the task's status (column) is updated to match the Jira status mapping

#### Scenario: Jira status changed externally with pending local move within window

- **WHEN** a pull sync detects a Jira status change but the task has a pending local move within the last 5 minutes (tracked in `remote_meta`)
- **THEN** the local column is preserved (local wins)

#### Scenario: ADF description conversion on pull

- **WHEN** a task is synced from Jira with an ADF description
- **THEN** the ADF is converted to Markdown and stored in `description_md`

### Requirement: Stale Ticket Handling

The sync service SHALL detect tickets present in SQLite but absent from the Jira query results. Stale tickets MUST be handled according to configuration (default: hide after 7 days, retain in database).

#### Scenario: Ticket no longer in Jira results

- **WHEN** a pull sync completes and a ticket in SQLite was not returned by the JQL query
- **THEN** the ticket is marked as stale with the current timestamp

#### Scenario: Stale ticket exceeds retention period

- **WHEN** a stale ticket has been absent from Jira results for longer than the configured retention period (default 7 days)
- **THEN** the ticket is hidden from the board view but retained in the SQLite database

### Requirement: Push Sync from Local to Jira

The sync service SHALL push local card movements to Jira as transitions. The transition ID SHALL be read from `remote_meta` for the target column. When a card is moved in the UI, the local SQLite state MUST be updated immediately and the Jira transition MUST be queued for async execution.

#### Scenario: Successful push transition

- **WHEN** a synced task (provider='jira') is moved to a new column and the transition succeeds
- **THEN** the `remote_meta` remote_status field is updated and a success entry is written to `sync_log`

#### Scenario: Failed push transition

- **WHEN** a transition fails
- **THEN** the task remains in the local column, a warning is set, and a failure entry is written to `sync_log`

#### Scenario: Push skipped for local tasks

- **WHEN** a local task (provider is NULL) is moved
- **THEN** no remote transition SHALL be attempted

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
