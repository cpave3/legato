## MODIFIED Requirements

### Requirement: Pull Sync from Jira to SQLite

The sync service SHALL pull tickets from Jira and convert them into legato tasks with `provider='jira'` and `remote_id` set to the Jira issue key. Jira-specific fields (remote_status, issue_type, assignee, labels, epic, URL, stale_at, local_move_at, remote_transition) SHALL be stored in the `remote_meta` JSON field. When matching incoming remote tickets to local tasks, the sync service SHALL check both `id` and `remote_id` fields to support bound tasks where `id != remote_id`.

#### Scenario: New ticket from Jira

- **WHEN** a pull sync finds a Jira issue that does not exist as a task in SQLite (neither by `id` nor by `remote_id`)
- **THEN** the ticket is silently skipped (pull never auto-imports; new tickets must be imported or bound manually)

#### Scenario: Updated ticket matching by remote_id

- **WHEN** a pull sync finds a Jira issue whose key matches a task's `remote_id` (but not its `id`, i.e., a bound task)
- **THEN** the task SHALL be updated with the latest title, description, priority, and `remote_meta` fields, same as a normally-imported task

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

### Requirement: Push Sync from Local to Jira

The sync service SHALL push local card movements to Jira as transitions. The transition ID SHALL be read from `remote_meta` for the target column. When a card is moved in the UI, the local SQLite state MUST be updated immediately and the Jira transition MUST be queued for async execution. Push sync SHALL use `remote_id` (not `id`) when communicating with the Jira API for bound tasks.

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
